// Package settings reads a component's .caduceus/project.toml (SPEC section 6) and turns it into a run's
// allow/deny tool lists.
package settings

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
)

// File is where a component keeps its settings, relative to its repo root.
const File = ".caduceus/project.toml"

// Network is the rules' network line when the settings don't set one.
const Network = "No network: no WebFetch, WebSearch, curl, wget or package downloads. Never add a dependency."

var (
	Kinds    = []string{"macos-app", "android-app", "web-app", "service", "homelab", "cli", "library", "config"}
	Meanings = []string{"test", "build", "lint", "format", "coverage", "complexity", "mutation", "e2e"}
	envKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	name     = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
)

// Access is the settings' [access] table.
type Access struct {
	Allow     []string // tools beyond the base set
	Deny      []string
	Protected []string // worktree-relative globs runs must not edit
	ReadOnly  []string // other components by name: --add-dir, not writable
	NoTouch   []string // other components by name: not writable
	Secret    []string // globs runs must never read
}

// Settings is one component's project.toml with defaults applied and its paths resolved.
type Settings struct {
	Repo       string
	Kind       string // empty when unset
	Language   string
	Model      string
	Effort     string
	BaseBranch string
	Hosts      []string
	Locks      []string
	Night      bool
	Env        map[string]string
	Gates      map[string][]string // by meaning (Meanings)
	Review     []string            // [review] gates: only Hermes runs them
	Access     Access
	Rules      []string
	Network    string

	Checkout     string   // the main checkout: never writable for a run
	ReadOnlyDirs []string // Access.ReadOnly resolved to <projects>/<name>
	NoTouchDirs  []string
	Others       []string // every other component's checkout and worktree root
}

// Load reads the settings from the default branch of the component's main checkout, never from a worktree.
func Load(x ext.Ext, cfg *config.Config, repo string) (*Settings, error) {
	if !name.MatchString(repo) {
		return nil, fmt.Errorf("bad component name %q", repo)
	}
	co := cfg.Checkout(repo)
	out, err := x.Run([]string{"git", "-C", co, "show", "origin/HEAD:" + File})
	if err != nil {
		return nil, fmt.Errorf("%s: no %s on origin/HEAD: %w", repo, File, err)
	}
	return parse([]byte(out), co+": origin/HEAD:"+File, repo, cfg)
}

// LoadFile reads the settings from a file (`--file`).
func LoadFile(cfg *config.Config, repo, path string) (*Settings, error) {
	if !name.MatchString(repo) {
		return nil, fmt.Errorf("bad component name %q", repo)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(data, path, repo, cfg)
}

// parse decodes strictly and reports every error at once, each naming its key.
func parse(data []byte, src, repo string, cfg *config.Config) (*Settings, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	s := &Settings{Repo: repo, Model: "sonnet", Effort: "high", BaseBranch: "develop", Network: Network,
		Env: map[string]string{}, Gates: map[string][]string{}, Checkout: cfg.Checkout(repo)}
	d := &decoder{}
	access := map[string]*[]string{"allow": &s.Access.Allow, "deny": &s.Access.Deny,
		"protected": &s.Access.Protected, "read_only": &s.Access.ReadOnly, "no_touch": &s.Access.NoTouch,
		"secret": &s.Access.Secret}
	strs := map[string]*string{"kind": &s.Kind, "language": &s.Language, "model": &s.Model, "effort": &s.Effort,
		"base_branch": &s.BaseBranch, "network": &s.Network}
	lists := map[string]*[]string{"hosts": &s.Hosts, "locks": &s.Locks, "rules": &s.Rules}
	for _, k := range slices.Sorted(maps.Keys(raw)) {
		v := raw[k]
		switch k {
		case "kind", "language", "model", "effort", "base_branch", "network":
			d.str(k, v, strs[k])
		case "hosts", "locks", "rules":
			*lists[k] = d.strs(k, v)
		case "night":
			if b, ok := v.(bool); ok {
				s.Night = b
			} else {
				d.fail("night must be a boolean")
			}
		case "env":
			for ek, ev := range d.table(k, v) {
				if !envKey.MatchString(ek) {
					d.fail("env key %q is not a shell name", ek)
				}
				var val string
				d.str("env."+ek, ev, &val)
				s.Env[ek] = val
			}
		case "gates":
			for gk, gv := range d.table(k, v) {
				if !slices.Contains(Meanings, gk) {
					d.fail("unknown key gates.%s (gates by meaning: %s)", gk, strings.Join(Meanings, ", "))
					continue
				}
				s.Gates[gk] = d.strs("gates."+gk, gv)
			}
		case "review":
			for rk, rv := range d.table(k, v) {
				if rk != "gates" {
					d.fail("unknown key review.%s", rk)
					continue
				}
				s.Review = d.strs("review.gates", rv)
			}
		case "access":
			for ak, av := range d.table(k, v) {
				if p, ok := access[ak]; ok {
					*p = d.strs("access."+ak, av)
				} else {
					d.fail("unknown key access.%s", ak)
				}
			}
		default:
			d.fail("unknown key %s", k)
		}
	}
	if s.Kind != "" && !slices.Contains(Kinds, s.Kind) {
		d.fail("kind %q is not one of %s", s.Kind, strings.Join(Kinds, ", "))
	}
	for k, v := range map[string]string{"model": s.Model, "effort": s.Effort, "base_branch": s.BaseBranch} {
		if v == "" {
			d.fail("%s must not be empty", k)
		}
	}
	s.ReadOnlyDirs = d.components("access.read_only", s.Access.ReadOnly, cfg)
	s.NoTouchDirs = d.components("access.no_touch", s.Access.NoTouch, cfg)
	if len(d.errs) > 0 {
		slices.Sort(d.errs) // map order above is random; keep the report stable
		errs := make([]error, len(d.errs))
		for i, e := range d.errs {
			errs[i] = fmt.Errorf("%s: %s", src, e)
		}
		return nil, errors.Join(errs...)
	}
	others, err := others(cfg, repo)
	if err != nil {
		return nil, err
	}
	s.Others = others
	return s, nil
}

// others lists every other component's checkout and worktree root. A component is any git checkout in the projects
// dir: settings files arrive repo by repo, and a checkout without one must not become writable meanwhile.
func others(cfg *config.Config, repo string) ([]string, error) {
	entries, err := os.ReadDir(cfg.Projects)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("projects dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == repo {
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.Projects, e.Name(), ".git")); err == nil {
			out = append(out, cfg.Checkout(e.Name()), cfg.Worktrees(e.Name()))
		}
	}
	return out, nil
}

type decoder struct{ errs []string }

func (d *decoder) fail(format string, args ...any) {
	d.errs = append(d.errs, fmt.Sprintf(format, args...))
}

// str sets *dst only when v is a string, so a wrong type keeps the default and gives one error.
func (d *decoder) str(key string, v any, dst *string) {
	if s, ok := v.(string); ok {
		*dst = s
	} else {
		d.fail("%s must be a string", key)
	}
}

func (d *decoder) strs(key string, v any) []string {
	list, ok := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		s, isStr := x.(string)
		ok = ok && isStr
		out = append(out, s)
	}
	if !ok {
		d.fail("%s must be a list of strings", key)
	}
	return out
}

func (d *decoder) table(key string, v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		d.fail("%s must be a table", key)
	}
	return m
}

// components resolves component names to <projects>/<name>; each must exist.
func (d *decoder) components(key string, names []string, cfg *config.Config) []string {
	var dirs []string
	for _, n := range names {
		if !name.MatchString(n) {
			d.fail("%s: %q is not a component name", key, n)
			continue
		}
		dir := cfg.Checkout(n)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			d.fail("%s: %s doesn't exist", key, dir)
			continue
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

// Summary is check's one line: kind, gates per meaning, allow/deny counts.
func (s *Settings) Summary() string {
	kind := s.Kind
	if kind == "" {
		kind = "unset"
	}
	var gates []string
	for _, m := range Meanings {
		if n := len(s.Gates[m]); n > 0 {
			gates = append(gates, fmt.Sprintf("%s %d", m, n))
		}
	}
	if len(gates) == 0 {
		gates = []string{"none"}
	}
	return fmt.Sprintf("%s: kind %s; gates %s; review %d; allow %d, deny %d", s.Repo, kind, strings.Join(gates, ", "),
		len(s.Review), len(s.Access.Allow), len(s.Access.Deny))
}
