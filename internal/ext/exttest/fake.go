// Package exttest has the test fake for ext.Ext: it answers only the commands and URLs a test sets up and fails
// the test on any other call.
package exttest

import (
	"encoding/json"
	"fmt"

	"github.com/xMuramasa/caduceus/internal/ext"
)

// TB is the part of testing.TB the fake needs.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// Fake records every call. Commands answers by program name (args[0]) in the shapes of real answers; JSON answers
// GETs by exact URL. With Git set, git runs for real: only point it at the test's temporary repos.
type Fake struct {
	T        TB
	Commands map[string]func(args []string) (string, error)
	JSON     map[string]any
	Git      bool
	Calls    [][]string
}

func (f *Fake) Run(args []string) (string, error) {
	f.T.Helper()
	f.Calls = append(f.Calls, append([]string(nil), args...))
	if len(args) > 0 && args[0] == "git" && f.Git {
		return ext.Real{}.Run(args)
	}
	if len(args) > 0 {
		if h, ok := f.Commands[args[0]]; ok {
			return h(args[1:])
		}
	}
	f.T.Errorf("unexpected command %q", args)
	return "", &ext.Error{Args: args, Code: -1, Msg: fmt.Sprintf("unexpected command %q", args)}
}

func (f *Fake) GetJSON(url string, v any) error {
	f.T.Helper()
	f.Calls = append(f.Calls, []string{"GET", url})
	body, ok := f.JSON[url]
	if !ok {
		f.T.Errorf("unexpected GET %s", url)
		return fmt.Errorf("unexpected GET %s", url)
	}
	// A round trip through JSON, so the caller decodes exactly what a real body would give.
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
