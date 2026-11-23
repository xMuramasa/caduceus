// Package ext is the one seam for everything that leaves the process (gh, git, docker, systemd, date, HTTP), so
// tests replace it with exttest.Fake.
package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Ext runs external commands and HTTP JSON GETs.
type Ext interface {
	// Run returns the command's stdout; a non-zero exit is an *Error that still carries stdout.
	Run(args []string) (string, error)
	// GetJSON decodes the JSON body of a GET into v.
	GetJSON(url string, v any) error
}

// Error is a command that failed to start or exited non-zero.
type Error struct {
	Args []string
	Code int // -1 when the command didn't run or timed out
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Real is the Ext that does the work.
type Real struct {
	Timeout     time.Duration // per command; 0 means 5 minutes
	HTTPTimeout time.Duration // per GET; 0 means 15 seconds
}

func (r Real) Run(args []string) (string, error) {
	if len(args) == 0 {
		return "", &Error{Code: -1, Msg: "ext: empty command"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), or(r.Timeout, 5*time.Minute))
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), nil
	case ctx.Err() != nil:
		return stdout.String(), &Error{args, -1, fmt.Sprintf("%s: %v", args[0], ctx.Err())}
	case errors.As(err, &exit):
		out := strings.TrimSpace(stderr.String())
		if out == "" {
			out = strings.TrimSpace(stdout.String())
		}
		if len(out) > 300 {
			out = out[:300]
		}
		head := args[:min(3, len(args))]
		msg := fmt.Sprintf("%s: exit %d: %s", strings.Join(head, " "), exit.ExitCode(), out)
		return stdout.String(), &Error{args, exit.ExitCode(), msg}
	default:
		return stdout.String(), &Error{args, -1, fmt.Sprintf("%s: %v", args[0], err)}
	}
}

func (r Real) GetJSON(url string, v any) error {
	c := http.Client{Timeout: or(r.HTTPTimeout, 15*time.Second)}
	resp, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}

func or(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}
