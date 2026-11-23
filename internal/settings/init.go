package settings

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/xMuramasa/caduceus/internal/config"
)

// profile is a hermes-dispatch profile (profiles/<repo>.toml): every key its DEFAULTS allow.
type profile struct {
	Model        string            `toml:"model"`
	Effort       string            `toml:"effort"`
	Checkout     string            `toml:"checkout"`
	WorktreeRoot string            `toml:"worktree_root"`
	BaseBranch   string            `toml:"base_branch"`
	Env          map[string]string `toml:"env"`
	Allow        []string          `toml:"allow"`
	Deny         []string          `toml:"deny"`
	Protected    []string          `toml:"protected"`
	ReadOnly     []string          `toml:"read_only"`
	NoTouch      []string          `toml:"no_touch"`
	Secret       []string          `toml:"secret"`
	Gates        []string          `toml:"gates"`
	HermesGates  []string          `toml:"hermes_gates"`
	Rules        []string          `toml:"rules"`
	Network      string            `toml:"network"`
}

var kindLine = fmt.Sprintf("# kind = \"\"  # set it: %s\n", strings.Join(Kinds, " | "))

// FromProfile turns a hermes-dispatch profile into project.toml text. Paths under the projects dir become component
// names; a no_touch path outside it becomes a protected glob (Tools denies edits there just the same).
func FromProfile(path, repo string, cfg *config.Config) (string, error) {
	var p profile
	md, err := toml.DecodeFile(path, &p)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if bad := md.Undecoded(); len(bad) > 0 {
		return "", fmt.Errorf("%s: unknown keys %v", path, bad)
	}
	if p.Checkout != "" || p.WorktreeRoot != "" {
		return "", fmt.Errorf("%s: checkout and worktree_root now come from the machine config (%s)", path, cfg.Path)
	}
	for k := range p.Env {
		if !envKey.MatchString(k) {
			return "", fmt.Errorf("%s: env key %q is not a shell name", path, k)
		}
	}
	var readOnly, noTouch []string
	for _, d := range p.ReadOnly {
		n, ok := component(d, repo, cfg)
		if !ok {
			return "", fmt.Errorf("%s: read_only %s is not a component in %s", path, d, cfg.Projects)
		}
		readOnly = append(readOnly, n)
	}
	protected := p.Protected
	for _, d := range p.NoTouch {
		if n, ok := component(d, repo, cfg); ok {
			noTouch = append(noTouch, n)
		} else {
			protected = append(protected, strings.TrimSuffix(d, "/")+"/**")
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s for %s, from the hermes-dispatch profile %s.\n", File, repo, filepath.Base(path))
	b.WriteString(kindLine)
	str := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, quote(v))
		}
	}
	list := func(k string, vs []string) {
		if len(vs) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s = [\n", k)
		for _, v := range vs {
			fmt.Fprintf(&b, "  %s,\n", quote(v))
		}
		b.WriteString("]\n")
	}
	str("model", p.Model)
	str("effort", p.Effort)
	str("base_branch", p.BaseBranch)
	list("rules", p.Rules)
	str("network", p.Network)
	if len(p.Env) > 0 {
		b.WriteString("\n[env]\n")
		for _, k := range slices.Sorted(maps.Keys(p.Env)) {
			str(k, p.Env[k])
		}
	}
	if len(p.Gates) > 0 {
		b.WriteString("\n[gates]\n# The profile's gates, all under test for now: split them by meaning (" +
			strings.Join(Meanings, ", ") + ").\n")
		list("test", p.Gates)
	}
	if len(p.HermesGates) > 0 {
		b.WriteString("\n[review]\n")
		list("gates", p.HermesGates)
	}
	if len(p.Allow)+len(p.Deny)+len(protected)+len(readOnly)+len(noTouch)+len(p.Secret) > 0 {
		b.WriteString("\n[access]\n")
		list("allow", p.Allow)
		list("deny", p.Deny)
		list("protected", protected)
		list("read_only", readOnly)
		list("no_touch", noTouch)
		list("secret", p.Secret)
	}
	return b.String(), nil
}

// component gives the component name for a path directly under the projects dir.
func component(path, repo string, cfg *config.Config) (string, bool) {
	abs, err := config.Expand(strings.ReplaceAll(path, "{repo}", repo))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(cfg.Projects, abs)
	return rel, err == nil && filepath.Dir(rel) == "." && name.MatchString(rel)
}

// quote writes a TOML basic string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Scaffold is project.toml text for a component without a profile: the defaults, every key, comments saying what
// goes where.
func Scaffold(repo string) string {
	return fmt.Sprintf(`# %s for %s (Caduceus SPEC section 6). Caduceus reads it from the default branch of the
# main checkout, never from a run's worktree, and runs can't edit it.
%slanguage = ""                  # informational; the gates below are what count
model = "sonnet"                # default model for solo runs; the issue's agent: label wins
effort = "high"
base_branch = "develop"         # task branches start here; "main" only for repos outside gitflow
hosts = []                      # hosts a run needs, e.g. a build machine
locks = []                      # shared resources a run holds
night = false                   # may the night run take this component's issues
rules = []                      # extra rule lines for the run's rules file
# network = "..."               # the rules' network line; the default allows no network and no new dependencies

[env]                           # set on the run's command line

[gates]                         # by meaning; each a list of commands the run itself executes
test = []
build = []
lint = []
format = []
coverage = []
complexity = []
mutation = []
e2e = []                        # the public surface QA drives (squads)

[review]                        # gates only Hermes runs (containers, other hosts)
gates = []

[access]
allow = []                      # tools beyond the base set, e.g. "Bash(go test*)"
deny = []
protected = []                  # worktree-relative globs runs must not edit
read_only = []                  # other components by name: readable, not writable
no_touch = []                   # other components by name: not writable
secret = []                     # globs runs must never read
`, File, repo, kindLine)
}
