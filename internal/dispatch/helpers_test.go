package dispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
	"github.com/xMuramasa/caduceus/internal/settings"
)

const (
	relay   = "xMuramasa/hermes-relay"
	unit27  = "caduceus-hermes-relay-issue-27.service"
	name27  = "hermes-relay-issue-27"
	summary = "http://172.18.0.9:8088/api/summary"
	usage   = "http://172.18.0.9:8088/api/usage"
)

func testdata(parts ...string) string {
	p, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...))
	if err != nil {
		panic(err)
	}
	return p
}

func fixture(t *testing.T, parts ...string) any {
	t.Helper()
	b, err := os.ReadFile(testdata(parts...))
	var v any
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fake answers gh, docker, systemd and date in the shapes of testdata (real samples); git runs for real, only
// against the test's temporary repos.
type fake struct {
	t         *testing.T
	x         *exttest.Fake
	issue     map[string]any         // xMuramasa/hermes-relay#27, from gh-issue-view.json
	others    map[int]map[string]any // other issues by number (any repo), for active runs' Owns
	blocked   []any                  // from gh-blocked-by.json: one open blocker
	active    map[string]bool
	comments  []string
	mutations []map[string]string
	date      func() (string, error)
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, issue: fixture(t, "gh", "gh-issue-view.json").(map[string]any), others: map[int]map[string]any{},
		blocked: fixture(t, "gh", "gh-blocked-by.json").([]any), active: map[string]bool{},
		date: func() (string, error) { return "03:31\n", nil }}
	f.x = &exttest.Fake{T: t, Git: true,
		JSON: map[string]any{summary: fixture(t, "horus", "horus-summary-ok.json"),
			usage: fixture(t, "horus", "horus-usage-live-ok.json")},
		Commands: map[string]func([]string) (string, error){
			"gh":          f.gh,
			"systemctl":   f.systemctl,
			"systemd-run": f.systemdRun,
			"docker":      func([]string) (string, error) { return "172.18.0.9\n", nil },
			"date":        func([]string) (string, error) { return f.date() },
		}}
	return f
}

func (f *fake) gh(a []string) (string, error) {
	switch {
	case a[0] == "issue" && a[1] == "view":
		n, _ := strconv.Atoi(a[2])
		v := any(f.issue)
		if o, ok := f.others[n]; ok {
			v = o
		}
		b, _ := json.Marshal(v)
		return string(b), nil
	case a[0] == "api" && strings.HasSuffix(a[1], "/dependencies/blocked_by?per_page=100"):
		b, _ := json.Marshal(f.blocked)
		return string(b), nil
	case a[0] == "issue" && a[1] == "comment":
		b, err := os.ReadFile(a[slices.Index(a, "--body-file")+1])
		if err != nil {
			f.t.Error(err)
		}
		f.comments = append(f.comments, string(b))
		return "", nil
	case a[0] == "api" && a[1] == "graphql":
		v := map[string]string{}
		for i := 2; i+1 < len(a); i += 2 {
			k, val, _ := strings.Cut(a[i+1], "=")
			v[k] = val
		}
		q := v["query"]
		delete(v, "query")
		var out any
		switch {
		case strings.Contains(q, "projectItems"):
			out = map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
				"projectItems": map[string]any{"nodes": []any{
					map[string]any{"id": "PVTI_OTHER", "project": map[string]any{"id": "PVT_OTHER"}},
					map[string]any{"id": "PVTI_FAKE", "project": map[string]any{"id": "PVT_kwHOAkoXDc4Bo1_m"}}}}}}}}
		case strings.HasPrefix(q, "mutation"):
			f.mutations = append(f.mutations, v)
			out = map[string]any{"data": map[string]any{"updateProjectV2ItemFieldValue": map[string]any{
				"projectV2Item": map[string]any{"id": v["i"]}}}}
		default:
			f.t.Errorf("unexpected graphql %s", q)
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	}
	f.t.Errorf("unexpected gh call %q", a)
	return "", &ext.Error{Args: a, Code: 1, Msg: "unexpected"}
}

func (f *fake) systemctl(a []string) (string, error) {
	if a[1] == "is-active" {
		if f.active[a[2]] {
			return "active\n", nil
		}
		return "inactive\n", &ext.Error{Args: a, Code: 3, Msg: "systemctl --user is-active: exit 3: inactive"}
	}
	f.t.Errorf("unexpected systemctl %q", a)
	return "", nil
}

func (f *fake) systemdRun(a []string) (string, error) {
	u := strings.TrimPrefix(a[1], "--unit=") + ".service"
	f.active[u] = true
	return "Running as unit: " + u + "\n", nil
}

func (f *fake) calls(prog string) [][]string {
	var out [][]string
	for _, c := range f.x.Calls {
		if c[0] == prog {
			out = append(out, c)
		}
	}
	return out
}

func (f *fake) wroteToGitHub() bool { return len(f.comments) > 0 || len(f.mutations) > 0 }

// env is a temporary home with the dispatch test config, and git kept off the user's config.
type env struct {
	home, runs string
	cfg        *config.Config
	f          *fake
}

func setup(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "CADUCEUS_CONFIG": testdata("dispatch-config.toml"),
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_AUTHOR_NAME": "t",
		"GIT_AUTHOR_EMAIL": "t@example.invalid", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.invalid"} {
		t.Setenv(k, v)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return &env{home: home, runs: cfg.Runs, cfg: cfg, f: newFake(t)}
}

func gitRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// relaySettings is the hermes-relay profile from testdata as project.toml text, with extra top-level lines first.
func (e *env) relaySettings(t *testing.T, extra string) string {
	t.Helper()
	text, err := settings.FromProfile(testdata("profiles", "hermes-relay.toml"), "hermes-relay", e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return extra + text
}

// repo makes a bare origin whose default branch develop has the settings (main has one commit less) and its clone at
// ~/Projects/<name>.
func (e *env) repo(t *testing.T, name, settingsText string) string {
	t.Helper()
	origin, seed, co := filepath.Join(e.home, name+".git"), filepath.Join(e.home, name+"-seed"), e.cfg.Checkout(name)
	gitRun(t, "init", "-q", "--bare", "-b", "develop", origin)
	gitRun(t, "init", "-q", "-b", "main", seed)
	writeFile(t, filepath.Join(seed, "README.md"), "hello\n")
	gitRun(t, "-C", seed, "add", ".")
	gitRun(t, "-C", seed, "commit", "-qm", "init")
	gitRun(t, "-C", seed, "push", "-q", origin, "HEAD:main")
	writeFile(t, filepath.Join(seed, "develop.txt"), "only on develop\n")
	writeFile(t, filepath.Join(seed, settings.File), settingsText)
	gitRun(t, "-C", seed, "add", ".")
	gitRun(t, "-C", seed, "commit", "-qm", "develop")
	gitRun(t, "-C", seed, "push", "-q", origin, "HEAD:develop")
	gitRun(t, "clone", "-q", origin, co)
	return co
}

// activeRun writes a run.json for an active run of repo#n.
func (e *env) activeRun(t *testing.T, repo string, n int) string {
	t.Helper()
	_, short, _ := strings.Cut(repo, "/")
	name := fmt.Sprintf("%s-issue-%d", short, n)
	writeFile(t, filepath.Join(e.runs, name+".run.json"), fmt.Sprintf(
		`{"id": "run_%s", "repo": %q, "issue": %d, "unit": "caduceus-%s.service", "started_at": "2026-11-23T01:00:00+00:00"}`,
		name, repo, n, name))
	e.f.active["caduceus-"+name+".service"] = true
	return name
}

func (e *env) dispatcher(w *bytes.Buffer) *Dispatcher {
	return &Dispatcher{X: e.f.x, Cfg: e.cfg, W: w, Sleep: func(time.Duration) {},
		Now: func() time.Time { return time.Date(2026, 11, 23, 3, 31, 0, 0, time.UTC) }}
}

func (e *env) check(t *testing.T, ref string, force bool) (bool, string) {
	t.Helper()
	r, err := ParseRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	ok, err := e.dispatcher(&w).Check(r, force)
	if err != nil {
		t.Fatal(err)
	}
	return ok, w.String()
}

func (e *env) start(t *testing.T, o Options) (string, error) {
	t.Helper()
	var w bytes.Buffer
	err := e.dispatcher(&w).Start(Ref{relay, "hermes-relay", 27}, o)
	return w.String(), err
}

func (e *env) readRun(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.runs, name+".run.json"))
	var m map[string]any
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func contains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}
