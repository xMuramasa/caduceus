package pr

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
	"github.com/xMuramasa/caduceus/internal/dispatch"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
	"github.com/xMuramasa/caduceus/internal/settings"
	"github.com/xMuramasa/caduceus/internal/worktree"
)

const (
	relay    = "xMuramasa/hermes-relay"
	board    = "PVT_kwHOAkoXDc4Bo1_m"
	account  = "xMuramasa"
	gateSets = `kind = "service"

[env]
GOPROXY = "off"

[gates]
test = ["go test ./..."]
format = ["gofmt -l ."]

[review]
gates = ["go test -race ./..."]
`
)

// fake answers gh in the shapes of real answers (testdata/gh) and the gates' `env ... sh -c` lines; git runs for
// real, only against the test's temporary repos.
type fake struct {
	t        *testing.T
	x        *exttest.Fake
	prs      map[string]map[string]any // `gh pr view <branch>` answers by branch
	polls    map[int]int               // `gh pr view <n> --json state` calls by PR: OPEN on the first, MERGED after
	created  [][]string                // `gh pr create` args
	bodies   []string                  // their --body-file contents
	reviews  map[int]string            // `gh pr comment` bodies by PR
	priority map[int]string            // board Priority by issue
	labels   map[int][]string          // issue labels
	gates    [][]string                // `env ...` calls
	fail     map[string]func() (string, error)
	nextPR   int
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, prs: map[string]map[string]any{}, polls: map[int]int{}, reviews: map[int]string{},
		priority: map[int]string{}, labels: map[int][]string{}, fail: map[string]func() (string, error){}, nextPR: 30}
	f.x = &exttest.Fake{T: t, Git: true, Commands: map[string]func([]string) (string, error){
		"gh":  f.gh,
		"env": f.env,
	}}
	return f
}

func fixture(t *testing.T, parts ...string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...))
	var v map[string]any
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func bodyFile(t *testing.T, a []string) string {
	b, err := os.ReadFile(a[slices.Index(a, "--body-file")+1])
	if err != nil {
		t.Error(err)
	}
	return string(b)
}

// addPR makes branch's PR as GitHub would show it once `pr open` ran.
func (f *fake) addPR(t *testing.T, branch string, n int, createdAt string) map[string]any {
	v := fixture(t, "gh", "gh-pr-view.json")
	v["number"], v["createdAt"] = n, createdAt
	f.prs[branch] = v
	return v
}

func (f *fake) gh(a []string) (string, error) {
	switch {
	case a[0] == "api" && a[1] == "user":
		return asJSON(map[string]any{"login": account, "id": 1, "type": "User"}), nil
	case a[0] == "api" && a[1] == "graphql":
		n, _ := strconv.Atoi(strings.TrimPrefix(a[slices.Index(a, "-F")+1], "n="))
		var labels []any
		for _, l := range f.labels[n] {
			labels = append(labels, map[string]any{"name": l})
		}
		items := []any{map[string]any{"project": map[string]any{"id": "PVT_OTHER"}, "priority": map[string]any{"name": "P0"}}}
		if p := f.priority[n]; p != "" {
			items = append(items, map[string]any{"project": map[string]any{"id": board}, "priority": map[string]any{"name": p}})
		}
		return asJSON(map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{
			"labels": map[string]any{"nodes": labels}, "projectItems": map[string]any{"nodes": items}}}}}), nil
	case a[0] == "pr" && a[1] == "create":
		f.created = append(f.created, a)
		f.bodies = append(f.bodies, bodyFile(f.t, a))
		f.nextPR++
		f.addPR(f.t, a[slices.Index(a, "--head")+1], f.nextPR, "2026-11-23T02:05:00Z")["comments"] = []any{}
		return fmt.Sprintf("\nCreating pull request for x into develop in %s\n\nhttps://github.com/%s/pull/%d\n", relay,
			relay, f.nextPR), nil
	case a[0] == "pr" && a[1] == "comment":
		n, _ := strconv.Atoi(a[2])
		f.reviews[n] = bodyFile(f.t, a)
		return fmt.Sprintf("https://github.com/%s/pull/%d#issuecomment-1000000002\n", relay, n), nil
	case a[0] == "pr" && a[1] == "view" && a[len(a)-1] == "state":
		n, _ := strconv.Atoi(a[2])
		f.polls[n]++
		if f.polls[n] == 1 {
			return `{"state":"OPEN"}`, nil
		}
		return `{"state":"MERGED"}`, nil
	case a[0] == "pr" && a[1] == "view":
		if v, ok := f.prs[a[2]]; ok {
			return asJSON(v), nil
		}
		return "", &ext.Error{Args: a, Code: 1, Msg: fmt.Sprintf("gh pr view %s: exit 1: no pull requests found for branch %q", a[2], a[2])}
	}
	f.t.Errorf("unexpected gh call %q", a)
	return "", &ext.Error{Args: a, Code: 1, Msg: "unexpected"}
}

func (f *fake) env(a []string) (string, error) {
	f.gates = append(f.gates, a)
	if fn, ok := f.fail[a[len(a)-1]]; ok {
		return fn()
	}
	return "", nil
}

func (f *fake) pushes() [][]string {
	var out [][]string
	for _, c := range f.x.Calls {
		if c[0] == "git" && slices.Contains(c, "push") {
			out = append(out, c)
		}
	}
	return out
}

// env is a temporary home with ~/Projects/hermes-relay cloned from a bare origin, and git kept off the user's config.
type env struct {
	home, origin, co string
	cfg              *config.Config
	f                *fake
}

func setup(t *testing.T, settingsText string) *env {
	t.Helper()
	home := t.TempDir()
	for k, v := range map[string]string{"HOME": home, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.invalid", "GIT_COMMITTER_NAME": "t",
		"GIT_COMMITTER_EMAIL": "t@example.invalid"} {
		t.Setenv(k, v)
	}
	path := filepath.Join(home, "config.toml")
	writeFile(t, path, fmt.Sprintf("[board]\nid = %q\nstatus_field = \"PVTSSF_FAKE\"\n", board))
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{home: home, origin: filepath.Join(home, "hermes-relay.git"), co: cfg.Checkout("hermes-relay"), cfg: cfg,
		f: newFake(t)}
	seed := filepath.Join(home, "seed")
	gitRun(t, "init", "-q", "--bare", "-b", "develop", e.origin)
	gitRun(t, "init", "-q", "-b", "main", seed)
	writeFile(t, filepath.Join(seed, "README.md"), "one\ntwo\nthree\n")
	writeFile(t, filepath.Join(seed, settings.File), settingsText)
	gitRun(t, "-C", seed, "add", ".")
	gitRun(t, "-C", seed, "commit", "-qm", "chore(repo): init (#1)")
	gitRun(t, "-C", seed, "push", "-q", e.origin, "HEAD:main", "HEAD:develop")
	gitRun(t, "clone", "-q", e.origin, e.co)
	return e
}

func gitRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
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

// task makes issue n's worktree on branch from origin/develop with one commit writing file.
func (e *env) task(t *testing.T, n int, branch, file, text string) string {
	t.Helper()
	wt := worktree.Path(e.cfg.Worktrees("hermes-relay"), n)
	gitRun(t, "-C", e.co, "worktree", "add", "-q", "-b", branch, wt, "origin/develop")
	writeFile(t, filepath.Join(wt, file), text)
	gitRun(t, "-C", wt, "add", ".")
	gitRun(t, "-C", wt, "commit", "-qm", fmt.Sprintf("feat(relay): change %s (#%d)", file, n), "-m",
		fmt.Sprintf("Why it changed.\n\nFixes #%d", n))
	return wt
}

func (e *env) pr(w *bytes.Buffer) *PR {
	return &PR{X: e.f.x, Cfg: e.cfg, W: w, Sleep: func(time.Duration) {}}
}

func ref(n int) dispatch.Ref { return dispatch.Ref{Repo: relay, Name: "hermes-relay", N: n} }

func (e *env) merge(t *testing.T, ns ...int) (string, error) {
	t.Helper()
	var refs []dispatch.Ref
	for _, n := range ns {
		refs = append(refs, ref(n))
	}
	var w bytes.Buffer
	err := e.pr(&w).Merge(refs)
	return w.String(), err
}

func (e *env) develop(t *testing.T) []string {
	t.Helper()
	return strings.Split(gitRun(t, "--git-dir", e.origin, "log", "--format=%s", "develop"), "\n")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contains(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}
