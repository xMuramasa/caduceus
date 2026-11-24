// Package config reads the machine config: where things live on this host and the ids Caduceus needs.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Limits are quota limits in percent: the 5-hour window and the weekly limit.
type Limits struct {
	Window int `toml:"window"`
	Weekly int `toml:"weekly"`
}

// Board is the GitHub project board work items live on.
type Board struct {
	ID          string            `toml:"id"`
	StatusField string            `toml:"status_field"`
	Status      map[string]string `toml:"status"` // option name (Todo, Ready, ...) -> option id
}

// Config is the machine config. Paths are absolute once loaded.
type Config struct {
	Path         string `toml:"-"`
	Projects     string `toml:"projects"`
	WorktreeRoot string `toml:"worktree_root"` // pattern with {projects} and {repo}
	Runs         string `toml:"runs"`
	Owner        string `toml:"owner"`
	Board        Board  `toml:"board"`
	HorusURL     string `toml:"horus_url"`
	Slots        int    `toml:"slots"`
	Quota        struct {
		Day   Limits `toml:"day"`
		Night Limits `toml:"night"`
	} `toml:"quota"`
}

// DefaultPath is $CADUCEUS_CONFIG, else ~/.config/caduceus/config.toml.
func DefaultPath() (string, error) {
	if p := os.Getenv("CADUCEUS_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(home, ".config", "caduceus", "config.toml"), nil
}

// Load reads the config at DefaultPath; a missing file gives the defaults.
func Load() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

// LoadFile reads the config at path; a missing file gives the defaults. Unknown keys are errors.
func LoadFile(path string) (*Config, error) {
	c := &Config{
		Path:         path,
		Projects:     "~/Projects",
		WorktreeRoot: "{projects}/{repo}-wt",
		Runs:         "~/.local/state/caduceus/runs",
		Slots:        3,
	}
	c.Quota.Day = Limits{Window: 70, Weekly: 80}
	c.Quota.Night = Limits{Window: 30, Weekly: 80}
	md, err := toml.DecodeFile(path, c)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if bad := md.Undecoded(); len(bad) > 0 {
		keys := make([]string, len(bad))
		for i, k := range bad {
			keys[i] = k.String()
		}
		sort.Strings(keys)
		return nil, fmt.Errorf("%s: unknown keys %s", path, strings.Join(keys, ", "))
	}
	if c.Projects, err = Expand(c.Projects); err != nil {
		return nil, err
	}
	if c.Runs, err = Expand(c.Runs); err != nil {
		return nil, err
	}
	if !strings.Contains(c.WorktreeRoot, "{repo}") {
		return nil, fmt.Errorf("%s: worktree_root must contain {repo}", path)
	}
	if c.Slots < 1 {
		return nil, fmt.Errorf("%s: slots must be at least 1", path)
	}
	for name, l := range map[string]Limits{"quota.day": c.Quota.Day, "quota.night": c.Quota.Night} {
		if l.Window < 0 || l.Window > 100 || l.Weekly < 0 || l.Weekly > 100 {
			return nil, fmt.Errorf("%s: %s limits must be percentages (0-100)", path, name)
		}
	}
	return c, nil
}

// Checkout is a component's main checkout: <projects>/<repo>.
func (c *Config) Checkout(repo string) string { return filepath.Join(c.Projects, repo) }

// Worktrees is the folder a component's task worktrees live in.
func (c *Config) Worktrees(repo string) string {
	s := strings.NewReplacer("{projects}", c.Projects, "{repo}", repo).Replace(c.WorktreeRoot)
	p, _ := Expand(s) // shortcut: only a bad $HOME fails here, and Load already expanded the projects dir with it
	return p
}

// Require fails with "set <key> in <path>" for the first key the config leaves empty. Keys: owner, horus_url,
// board.id, board.status_field.
func (c *Config) Require(keys ...string) error {
	for _, k := range keys {
		var v string
		switch k {
		case "owner":
			v = c.Owner
		case "horus_url":
			v = c.HorusURL
		case "board.id":
			v = c.Board.ID
		case "board.status_field":
			v = c.Board.StatusField
		default:
			return fmt.Errorf("config: no key %s", k)
		}
		if v == "" {
			return fmt.Errorf("set %s in %s", k, c.Path)
		}
	}
	return nil
}

// Expand resolves a leading ~ and makes the path absolute.
func Expand(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("config: %w", err)
		}
		p = filepath.Join(home, p[1:])
	}
	return filepath.Abs(p)
}
