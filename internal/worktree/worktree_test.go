package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

func TestBranchTypeComesFromLabels(t *testing.T) {
	for _, tc := range []struct {
		n      int
		title  string
		labels []string
		want   string
	}{
		{28, "ntfy push", []string{"P1", "hermes"}, "feat/28-ntfy-push"},
		{29, "Cross-device latency", []string{"bug", "P1"}, "fix/29-cross-device-latency"},
		{3, "README", []string{"documentation"}, "docs/3-readme"},
		{4, "Bump", []string{"chore"}, "chore/4-bump"},
		{5, "x", []string{"documentation", "bug"}, "fix/5-x"}, // bug wins
	} {
		if got := Branch(tc.n, tc.title, tc.labels); got != tc.want {
			t.Errorf("Branch(%d, %q, %q) = %q, want %q", tc.n, tc.title, tc.labels, got, tc.want)
		}
	}
}

func TestSlugDropsFillerAndStaysShort(t *testing.T) {
	for title, want := range map[string]string{
		"Agents helper: read runs from ~/.local/state/hermes-runs and name each run's issue": "agents-helper-read-runs-local",
		strings.Repeat("a", 50): strings.Repeat("a", 32),
		"The":                   "the",
		"¿?":                    "task",
	} {
		if got := Slug(title); got != want {
			t.Errorf("Slug(%q) = %q, want %q", title, got, want)
		}
	}
	if s := Slug("one two three four five six seven eight nine ten eleven twelve"); len(s) > SlugMax {
		t.Errorf("%q is longer than %d", s, SlugMax)
	}
}

func git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// repo is a bare origin with one commit on main and one more on develop, and its clone; it returns the clone.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range map[string]string{"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.invalid", "GIT_COMMITTER_NAME": "t",
		"GIT_COMMITTER_EMAIL": "t@example.invalid"} {
		t.Setenv(k, v)
	}
	origin, co := filepath.Join(dir, "relay.git"), filepath.Join(dir, "relay")
	git(t, "init", "-q", "--bare", "-b", "main", origin)
	git(t, "clone", "-q", origin, co)
	for _, f := range []struct{ file, branch string }{{"README.md", "main"}, {"develop.txt", "develop"}} {
		if err := os.WriteFile(filepath.Join(co, f.file), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, "-C", co, "add", ".")
		git(t, "-C", co, "commit", "-qm", f.file)
		git(t, "-C", co, "push", "-q", "origin", "HEAD:"+f.branch)
	}
	git(t, "-C", co, "reset", "-q", "--hard", "origin/main") // the main checkout sits on main
	return co
}

func ensure(t *testing.T, co string) (string, string, string, error) {
	return Ensure(&exttest.Fake{T: t, Git: true}, co, co+"-wt", "develop", 27, "feat/27-agents-helper")
}

func TestCreateThenReuseClean(t *testing.T) {
	co := repo(t)
	wt, branch, what, err := ensure(t, co)
	if err != nil {
		t.Fatal(err)
	}
	if wt != co+"-wt/issue-27" || branch != "feat/27-agents-helper" ||
		what != "created "+wt+" on feat/27-agents-helper from origin/develop" {
		t.Errorf("%q %q %q", wt, branch, what)
	}
	if got := git(t, "-C", wt, "branch", "--show-current"); got != "feat/27-agents-helper\n" {
		t.Errorf("branch %q", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "develop.txt")); err != nil {
		t.Error("not based on origin/develop:", err)
	}
	if _, _, what, err = ensure(t, co); err != nil || !strings.HasPrefix(what, "reusing clean worktree") {
		t.Errorf("%q %v", what, err)
	}
	if err := os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = ensure(t, co); err == nil || !strings.Contains(err.Error(), "has uncommitted changes") {
		t.Errorf("dirty worktree: %v", err)
	}
}

func TestExistingBranches(t *testing.T) {
	co := repo(t)
	git(t, "-C", co, "branch", "fix/27-older-title")
	git(t, "-C", co, "branch", "feat/270-other-issue")
	_, branch, what, err := ensure(t, co)
	if err != nil || branch != "fix/27-older-title" || !strings.HasPrefix(what, "reusing existing branch fix/27-older-title") {
		t.Errorf("%q %q %v", branch, what, err)
	}

	co = repo(t)
	git(t, "-C", co, "branch", "fix/27-one")
	git(t, "-C", co, "branch", "feat/27-two")
	if _, _, _, err = ensure(t, co); err == nil ||
		err.Error() != "several branches for issue 27: feat/27-two, fix/27-one; delete the stale ones" {
		t.Errorf("%v", err)
	}
}

func TestNotAWorktreeOfTheIssue(t *testing.T) {
	co := repo(t)
	if _, _, _, err := Ensure(&exttest.Fake{T: t, Git: true}, co+"-missing", co+"-wt", "develop", 27, "x/27-y"); err == nil ||
		!strings.Contains(err.Error(), "no main checkout") {
		t.Errorf("%v", err)
	}
	if err := os.MkdirAll(co+"-wt/issue-27", 0o755); err != nil {
		t.Fatal(err)
	}
	// A plain folder inside no repo: rev-parse fails.
	if _, _, _, err := ensure(t, co); err == nil || !strings.Contains(err.Error(), "is not a git worktree") {
		t.Errorf("%v", err)
	}
	if err := os.Remove(co + "-wt/issue-27"); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", co, "worktree", "add", "-q", "-b", "feat/9-other", co+"-wt/issue-27")
	if _, _, _, err := ensure(t, co); err == nil || !strings.Contains(err.Error(), "(it is on feat/9-other)") {
		t.Errorf("%v", err)
	}
}
