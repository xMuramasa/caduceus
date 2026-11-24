package settings

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

// Shipped are the hermes-dispatch profiles copied into testdata/profiles.
var Shipped = []string{"caduceus", "hermes-dispatch", "hermes-relay", "homelab", "mage-sys", "news-reader",
	"tablet-kiosk", "trismegistus-mac"}

// setup gives a temporary home whose ~/Projects holds a git checkout of every shipped component plus the plain
// folders profiles refer to, and the machine config defaults for it.
func setup(t *testing.T) (string, *config.Config) {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "CADUCEUS_CONFIG": "", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.invalid",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.invalid"} {
		t.Setenv(k, v)
	}
	for _, r := range Shipped {
		mkdir(t, filepath.Join(home, "Projects", r, ".git"))
	}
	mkdir(t, filepath.Join(home, "Projects", "endfield-dotfiles"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return home, cfg
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) string {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func parseText(t *testing.T, cfg *config.Config, repo, text string) (*Settings, error) {
	t.Helper()
	return LoadFile(cfg, repo, writeFile(t, filepath.Join(t.TempDir(), "project.toml"), text))
}

func TestDefaults(t *testing.T) {
	_, cfg := setup(t)
	s, err := parseText(t, cfg, "relay", "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != "" || s.Model != "sonnet" || s.Effort != "high" || s.BaseBranch != "develop" || s.Night ||
		s.Network != Network || len(s.Env) != 0 || len(s.Gates) != 0 || s.Checkout != cfg.Checkout("relay") {
		t.Errorf("defaults: %+v", s)
	}
	if got := s.Summary(); got != "relay: kind unset; gates none; review 0; allow 0, deny 0" {
		t.Errorf("summary %q", got)
	}
}

func TestSpecExample(t *testing.T) {
	_, cfg := setup(t)
	s, err := parseText(t, cfg, "relay", `
kind = "service"
language = "go"
model = "opus"
effort = "high"
base_branch = "develop"
hosts = ["optiplex"]
locks = []
night = true

[env]
GOPROXY = "off"
GOFLAGS = "-mod=readonly"

[gates]
test = ["go -C relay test ./..."]
build = ["go -C relay build ./..."]
lint = ["go -C relay vet ./..."]
format = ["gofmt -l relay"]
coverage = []
complexity = []
mutation = []
e2e = []

[review]
gates = ["docker run ... golang:1.27.3-bookworm go test -race ./..."]

[access]
allow = ["Bash(go -C relay test*)"]
deny = ["Bash(go get*)", "Bash(go mod*)"]
protected = ["scripts/*.sh", "compose*.yml"]
read_only = ["hermes-relay"]
no_touch = []
secret = []

rules = ["Never log tokens, pairing codes or message text."]
network = "No network. Standard library only unless the issue says otherwise."
`)
	if err == nil {
		t.Fatal("rules after [access] belong to the access table and must be rejected")
	}
	if !strings.Contains(err.Error(), "unknown key access.rules") || !strings.Contains(err.Error(), "access.network") {
		t.Errorf("err %v", err)
	}
	// The same file with the top-level keys before the tables, as TOML requires.
	s, err = parseText(t, cfg, "relay", `
kind = "service"
model = "opus"
hosts = ["optiplex"]
night = true
rules = ["Never log tokens, pairing codes or message text."]
network = "No network."
[env]
GOPROXY = "off"
[gates]
test = ["go -C relay test ./..."]
lint = ["go -C relay vet ./..."]
format = ["gofmt -l relay"]
[review]
gates = ["go test -race ./..."]
[access]
allow = ["Bash(go -C relay test*)"]
deny = ["Bash(go get*)", "Bash(go mod*)"]
read_only = ["hermes-relay"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Night || s.Env["GOPROXY"] != "off" || !reflect.DeepEqual(s.ReadOnlyDirs, []string{cfg.Checkout("hermes-relay")}) ||
		s.Network != "No network." || len(s.Rules) != 1 || !reflect.DeepEqual(s.Hosts, []string{"optiplex"}) {
		t.Errorf("got %+v", s)
	}
	want := "relay: kind service; gates test 1, lint 1, format 1; review 1; allow 1, deny 2"
	if got := s.Summary(); got != want {
		t.Errorf("summary %q, want %q", got, want)
	}
}

func TestBadSettings(t *testing.T) {
	home, cfg := setup(t)
	tests := []struct {
		text string
		want []string
	}{
		{`modle = "opus"`, []string{"unknown key modle"}},
		{"[access]\nallow = \"Bash(ls*)\"", []string{"access.allow must be a list of strings"}},
		{"[access]\nallow = [1]", []string{"access.allow must be a list of strings"}},
		{"[env]\n\"A B\" = \"x\"", []string{`env key "A B" is not a shell name`}},
		{"[env]\nX = 1", []string{"env.X must be a string"}},
		{`env = "x"`, []string{"env must be a table"}},
		{`night = "yes"`, []string{"night must be a boolean"}},
		{`model = 4`, []string{"model must be a string"}},
		{`base_branch = ""`, []string{"base_branch must not be empty"}},
		{`kind = "toaster"`, []string{`kind "toaster" is not one of macos-app`}},
		{"[gates]\nunit = []", []string{"unknown key gates.unit"}},
		{"[review]\ngate = []", []string{"unknown key review.gate"}},
		{"[access]\nread_only = [\"nope\"]", []string{"access.read_only: " + filepath.Join(home, "Projects/nope") + " doesn't exist"}},
		{"[access]\nno_touch = [\"../etc\"]", []string{`access.no_touch: "../etc" is not a component name`}},
		{"x =", []string{"project.toml"}},
		{"modle = 1\nnight = 1\n[gates]\ntest = \"go test\"", []string{"unknown key modle", "night must be a boolean",
			"gates.test must be a list of strings"}},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			_, err := parseText(t, cfg, "relay", tc.text)
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err %q, want it to contain %q", err, w)
				}
			}
			if n := len(strings.Split(err.Error(), "\n")); n != len(tc.want) {
				t.Errorf("%d error lines, want %d: %v", n, len(tc.want), err)
			}
		})
	}
	if _, err := LoadFile(cfg, "../relay", "/dev/null"); err == nil {
		t.Error("a path as a component name was accepted")
	}
}

func gitRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLoadFromOriginHEAD(t *testing.T) {
	home, cfg := setup(t)
	origin, seed, co := filepath.Join(home, "relay.git"), filepath.Join(home, "seed"), cfg.Checkout("relay")
	gitRun(t, "init", "-q", "--bare", "-b", "develop", origin)
	gitRun(t, "init", "-q", "-b", "develop", seed)
	writeFile(t, filepath.Join(seed, File), "model = \"opus\"\n")
	gitRun(t, "-C", seed, "add", ".")
	gitRun(t, "-C", seed, "commit", "-qm", "settings")
	gitRun(t, "-C", seed, "push", "-q", origin, "HEAD:develop")
	gitRun(t, "clone", "-q", origin, co)
	// Local changes, committed or not, are not the default branch: Load must not see them.
	writeFile(t, filepath.Join(co, File), "model = \"haiku\"\n")
	gitRun(t, "-C", co, "commit", "-qam", "local only")
	writeFile(t, filepath.Join(co, File), "model = \"fable\"\n")

	fake := &exttest.Fake{T: t, Git: true}
	s, err := Load(fake, cfg, "relay")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "opus" {
		t.Errorf("model %q, want origin/HEAD's opus", s.Model)
	}
	want := [][]string{{"git", "-C", co, "show", "origin/HEAD:.caduceus/project.toml"}}
	if !reflect.DeepEqual(fake.Calls, want) {
		t.Errorf("calls %q", fake.Calls)
	}

	gitRun(t, "clone", "-q", origin, cfg.Checkout("bare"))
	gitRun(t, "-C", seed, "rm", "-q", File)
	gitRun(t, "-C", seed, "commit", "-qm", "no settings")
	gitRun(t, "-C", seed, "push", "-q", origin, "HEAD:develop")
	gitRun(t, "-C", cfg.Checkout("bare"), "fetch", "-q")
	if _, err := Load(fake, cfg, "bare"); err == nil || !strings.Contains(err.Error(), "bare: no .caduceus/project.toml on origin/HEAD") {
		t.Errorf("err %v", err)
	}
	if _, err := Load(fake, cfg, "../relay"); err == nil || len(fake.Calls) != 2 {
		t.Errorf("a path as a component name reached git: %v %q", err, fake.Calls)
	}
}

// fromProfile runs the shipped profile through FromProfile and back through the strict loader.
func fromProfile(t *testing.T, cfg *config.Config, repo string) *Settings {
	t.Helper()
	text, err := FromProfile(filepath.Join("..", "..", "testdata", "profiles", repo+".toml"), repo, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseText(t, cfg, repo, text)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	return s
}

func TestEveryProfileRoundTrips(t *testing.T) {
	home, cfg := setup(t)
	for _, repo := range Shipped {
		t.Run(repo, func(t *testing.T) {
			s := fromProfile(t, cfg, repo)
			wt := filepath.Join(cfg.Worktrees(repo), "issue-7")
			allow, deny := s.Tools(wt)
			for _, d := range slices.Concat(BaseDeny, AgentFiles) {
				if !slices.Contains(deny, d) {
					t.Errorf("deny lacks %s", d)
				}
			}
			if !slices.Contains(deny, "Edit(/"+home+"/Projects/"+repo+"/**)") {
				t.Errorf("own checkout writable")
			}
			if slices.Contains(deny, "Edit(/"+home+"/Projects/"+repo+"-wt/**)") {
				t.Errorf("own worktree root denied")
			}
			if !slices.Contains(deny, "Write(/"+wt+"/.caduceus/**)") {
				t.Errorf(".caduceus/** not protected")
			}
			for _, a := range allow {
				if strings.HasPrefix(a, "Bash(git commit") || strings.HasPrefix(a, "Bash(docker") || strings.HasPrefix(a, "Bash(curl") {
					t.Errorf("allowed %s", a)
				}
			}
			for _, a := range s.Access.Allow {
				if slices.Contains(s.Access.Deny, a) {
					t.Errorf("%s both allowed and denied", a)
				}
			}
			if len(s.Gates["test"]) == 0 || s.Model == "" {
				t.Errorf("gates or model lost: %+v", s)
			}
		})
	}
}

func TestFromProfileKeepsValues(t *testing.T) {
	_, cfg := setup(t)
	s := fromProfile(t, cfg, "caduceus")
	if s.Model != "opus" || s.Env["GOFLAGS"] != "-mod=readonly" || len(s.Rules) != 5 ||
		!strings.HasPrefix(s.Network, "No network: no WebFetch, WebSearch, curl, wget, `go get`") ||
		!reflect.DeepEqual(s.Gates["test"], []string{"go test ./...", "go vet ./...", "gofmt -l ."}) ||
		!reflect.DeepEqual(s.Review, []string{"go test -race ./..."}) ||
		!reflect.DeepEqual(s.Access.ReadOnly, []string{"hermes-dispatch"}) || len(s.Access.Protected) != 7 {
		t.Errorf("got %+v", s)
	}
	h := fromProfile(t, cfg, "homelab")
	if want := "uv run -q --no-project --with pyyaml python -c \"import yaml,sys; [yaml.safe_load(open(f)) for f in sys.argv[1:]]\" apps/homepage/*.yaml"; h.Gates["test"][1] != want {
		t.Errorf("quoting: %q", h.Gates["test"][1])
	}
	if s.Network == Network || fromProfile(t, cfg, "trismegistus-mac").Network != Network {
		t.Error("network default")
	}
	m := fromProfile(t, cfg, "trismegistus-mac")
	if !reflect.DeepEqual(m.Access.ReadOnly, []string{"hermes-relay"}) || !reflect.DeepEqual(m.Access.NoTouch, []string{"endfield-dotfiles"}) {
		t.Errorf("components: %+v", m.Access)
	}
	if ms := fromProfile(t, cfg, "mage-sys"); !reflect.DeepEqual(ms.Access.Protected, []string{"~/.local/share/mage-sys/**"}) {
		t.Errorf("no_touch outside the projects dir: %+v", ms.Access)
	}
}

func TestFromProfileErrors(t *testing.T) {
	_, cfg := setup(t)
	dir := t.TempDir()
	for text, want := range map[string]string{
		`modle = "opus"`:                    "unknown keys [modle]",
		`read_only = ["/srv/elsewhere"]`:    "read_only /srv/elsewhere is not a component",
		`checkout = "~/src/{repo}"`:         "checkout and worktree_root now come from the machine config",
		`env = { "A B" = "x" }`:             `env key "A B" is not a shell name`,
		`allow = "Bash(ls*)"`:               "allow",
		"x =":                               "bad.toml",
		`read_only = ["~/Projects/a/b"]`:    "is not a component",
		`no_touch = ["~/Projects/missing"]`: "",
	} {
		t.Run(text, func(t *testing.T) {
			_, err := FromProfile(writeFile(t, filepath.Join(dir, "bad.toml"), text), "relay", cfg)
			if want == "" {
				if err != nil {
					t.Fatal(err) // init writes the name; check reports that it doesn't exist
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("err %v, want %q", err, want)
			}
		})
	}
}

func TestScaffoldParses(t *testing.T) {
	_, cfg := setup(t)
	s, err := parseText(t, cfg, "relay", Scaffold("relay"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "sonnet" || s.BaseBranch != "develop" || s.Night || s.Network != Network {
		t.Errorf("got %+v", s)
	}
}

// renamed maps the hand-built examples' paths across the trismegistus -> hermes-relay rename (2026-11-11), and
// their home to the test's.
func renamed(launch, home string) string {
	launch = strings.ReplaceAll(launch, "/home/muramasa/", home+"/")
	launch = strings.ReplaceAll(launch, "/Projects/trismegistus-wt/", "/Projects/hermes-relay-wt/")
	return regexp.MustCompile(`/Projects/trismegistus([/)])`).ReplaceAllString(launch, "/Projects/hermes-relay$1")
}

func toolLists(t *testing.T, launch string) (allow, deny []string) {
	t.Helper()
	a := regexp.MustCompile(`--allowedTools '([^']*)'`).FindStringSubmatch(launch)
	d := regexp.MustCompile(`--disallowedTools '([^']*)'`).FindStringSubmatch(launch)
	if a == nil || d == nil {
		t.Fatal("no tool lists")
	}
	return strings.Split(a[1], ","), strings.Split(d[1], ",")
}

func missing(want, got []string) []string {
	var out []string
	for _, w := range want {
		if !slices.Contains(got, w) {
			out = append(out, w)
		}
	}
	return out
}

func TestToolsCoverTheHandBuiltLaunchScripts(t *testing.T) {
	home, cfg := setup(t)
	// Entries that were specific to one issue, not to the repo.
	issueOnly := []string{"Edit(/" + home + "/Projects/hermes-relay-wt/issue-25/relay/internal/github/**)",
		"Write(/" + home + "/Projects/hermes-relay-wt/issue-25/relay/internal/github/**)"}
	for _, tc := range []struct {
		script, repo, issue string
	}{
		{"trismegistus-mac-issue-52-launch.sh", "trismegistus-mac", "issue-52"},
		{"trismegistus-relay-25-launch.sh", "hermes-relay", "issue-25"},
		{"news-reader-issue-11-launch.sh", "news-reader", "issue-11"},
		{"tablet-kiosk-issue-2-launch.sh", "tablet-kiosk", "issue-2"},
	} {
		t.Run(tc.script, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "launch-examples", tc.script))
			if err != nil {
				t.Fatal(err)
			}
			wantAllow, wantDeny := toolLists(t, renamed(string(b), home))
			allow, deny := fromProfile(t, cfg, tc.repo).Tools(filepath.Join(cfg.Worktrees(tc.repo), tc.issue))
			if m := missing(wantAllow, allow); len(m) > 0 {
				t.Errorf("allow lacks %q", m)
			}
			if m := missing(wantDeny, append(deny, issueOnly...)); len(m) > 0 {
				t.Errorf("deny lacks %q", m)
			}
		})
	}
}

func TestRepoSpecificEntries(t *testing.T) {
	home, cfg := setup(t)
	tools := func(repo, issue string) ([]string, []string) {
		return fromProfile(t, cfg, repo).Tools(filepath.Join(cfg.Worktrees(repo), issue))
	}
	_, deny := tools("trismegistus-mac", "issue-52")
	for _, d := range []string{"Bash(scripts/mac-build.sh --install*)",
		"Write(/" + home + "/Projects/trismegistus-mac-wt/issue-52/scripts/*.sh)"} {
		if !slices.Contains(deny, d) {
			t.Errorf("mac deny lacks %s", d)
		}
	}
	allow, deny := tools("homelab", "issue-3")
	// Edits files only: the Homepage build and a YAML parse are its own gates (homelab#1), the rest are Hermes'.
	want := append(slices.Clone(BaseAllow), "Bash(python3 apps/homepage/theme/build.py*)",
		"Bash(uv run -q --no-project --with pyyaml python -c*)")
	if !reflect.DeepEqual(allow, want) {
		t.Errorf("homelab allow %q", allow)
	}
	for _, d := range []string{"Bash(docker*)", "Read(/" + home + "/Projects/homelab-wt/issue-3/**/.env)"} {
		if !slices.Contains(deny, d) {
			t.Errorf("homelab deny lacks %s", d)
		}
	}
	if _, deny = tools("tablet-kiosk", "issue-2"); !slices.Contains(deny, "Read(/"+home+"/.local/share/tablet-kiosk/**)") {
		t.Error("tablet-kiosk secret outside the worktree")
	}
	allow, deny = tools("mage-sys", "issue-4")
	if !slices.Contains(allow, "Bash(uv run pytest*)") || slices.Contains(allow, "Bash(python3*)") ||
		!slices.Contains(deny, "Bash(uv run council ask*)") {
		t.Errorf("mage-sys %q %q", allow, deny)
	}
	// hermes-dispatch's no_touch denied edits to the data dir; as a protected glob it still does.
	for _, d := range []string{"Edit(/" + home + "/.local/share/mage-sys/**)", "Write(/" + home + "/.local/share/mage-sys/**)"} {
		if !slices.Contains(deny, d) {
			t.Errorf("mage-sys deny lacks %s", d)
		}
	}
	if slices.Contains(deny, "Read(/"+home+"/.local/share/mage-sys/**)") {
		t.Error("mage-sys data dir became unreadable")
	}
}

func TestToolsOrderAndDedupe(t *testing.T) {
	home, cfg := setup(t)
	s, err := parseText(t, cfg, "hermes-relay", "[access]\nallow = [\"Read\", \"Bash(go test*)\"]\ndeny = [\"WebFetch\"]\n"+
		"protected = [\"/abs/x\"]\nsecret = [\"**/.env\"]\nread_only = [\"caduceus\"]\nno_touch = [\"endfield-dotfiles\"]")
	if err != nil {
		t.Fatal(err)
	}
	allow, deny := s.Tools("/wt/issue-1/")
	if want := append(slices.Clone(BaseAllow), "Bash(go test*)"); !reflect.DeepEqual(allow, want) {
		t.Errorf("allow %q", allow)
	}
	p := "/" + home + "/Projects/"
	want := slices.Concat(BaseDeny, []string{
		"Edit(//wt/issue-1/.caduceus/**)", "Write(//wt/issue-1/.caduceus/**)",
		"Edit(//abs/x)", "Write(//abs/x)",
		"Read(//wt/issue-1/**/.env)", "Edit(//wt/issue-1/**/.env)", "Write(//wt/issue-1/**/.env)",
		"Edit(" + p + "hermes-relay/**)", "Write(" + p + "hermes-relay/**)",
		"Edit(" + p + "caduceus/**)", "Write(" + p + "caduceus/**)",
		"Edit(" + p + "endfield-dotfiles/**)", "Write(" + p + "endfield-dotfiles/**)",
		"Edit(" + p + "caduceus-wt/**)", "Write(" + p + "caduceus-wt/**)",
	})
	for _, r := range Shipped[1:] {
		if r != "hermes-relay" {
			want = append(want, "Edit("+p+r+"/**)", "Write("+p+r+"/**)", "Edit("+p+r+"-wt/**)", "Write("+p+r+"-wt/**)")
		}
	}
	want = append(want, AgentFiles...)
	if !reflect.DeepEqual(deny, want) {
		t.Errorf("deny\n got %q\nwant %q", deny, want)
	}
}
