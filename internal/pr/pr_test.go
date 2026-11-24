package pr

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xMuramasa/caduceus/internal/dispatch"
	"github.com/xMuramasa/caduceus/internal/ext"
)

func TestOpenPushesOpensIntoDevelopAndPostsTheReview(t *testing.T) {
	e := setup(t, gateSets)
	wt := e.task(t, 27, "feat/27-ntfy-push", "push.go", "package relay\n")
	review := filepath.Join(e.home, "review.md")
	writeFile(t, review, "Gates pass; no findings.\n")
	var w bytes.Buffer
	if err := e.pr(&w).Open(ref(27), review); err != nil {
		t.Fatalf("%v\n%s", err, w.String())
	}
	if got, want := gitRun(t, "--git-dir", e.origin, "rev-parse", "feat/27-ntfy-push"), gitRun(t, "-C", wt, "rev-parse", "HEAD"); got != want {
		t.Errorf("origin has %s, worktree %s", got, want)
	}
	if len(e.f.created) != 1 {
		t.Fatalf("gh pr create calls: %q", e.f.created)
	}
	c := strings.Join(e.f.created[0], " ")
	contains(t, c, "-R "+relay, "--base develop", "--head feat/27-ntfy-push", "--title feat(relay): change push.go (#27)")
	contains(t, e.f.bodies[0], "Why it changed.", "Fixes #27")
	if e.f.reviews[31] != "Gates pass; no findings.\n" {
		t.Errorf("review comments: %v", e.f.reviews)
	}
	contains(t, w.String(), "pushed feat/27-ntfy-push", "opened https://github.com/"+relay+"/pull/31", "posted the review")
}

func TestMergeRebasesRunsGatesFastForwardsAndCleansUp(t *testing.T) {
	e := setup(t, gateSets)
	wt := e.task(t, 27, "feat/27-ntfy-push", "push.go", "package relay\n")
	e.f.addPR(t, "feat/27-ntfy-push", 31, "2026-11-23T02:05:00Z")
	// develop moves after the branch was cut, so the merge has to rebase.
	other := e.task(t, 28, "fix/28-other", "other.go", "package relay\n")
	gitRun(t, "-C", other, "push", "-q", "origin", "HEAD:develop")
	out, err := e.merge(t, 27)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := e.develop(t); !slices.Equal(got, []string{"feat(relay): change push.go (#27)",
		"feat(relay): change other.go (#28)", "chore(repo): init (#1)"}) {
		t.Errorf("develop: %q", got)
	}
	if gitRun(t, "--git-dir", e.origin, "rev-parse", "develop") != gitRun(t, "--git-dir", e.origin, "rev-parse", "feat/27-ntfy-push") {
		t.Error("the PR head on origin isn't what landed on develop")
	}
	if len(e.f.gates) != 3 {
		t.Fatalf("gates: %q", e.f.gates)
	}
	for i, cmd := range []string{"go test ./...", "gofmt -l .", "go test -race ./..."} {
		g := e.f.gates[i]
		if g[0] != "GOPROXY=off" || g[len(g)-2] != wt || g[len(g)-1] != cmd {
			t.Errorf("gate %d: %q", i, g)
		}
	}
	if e.f.polls[31] != 2 {
		t.Errorf("polled %d times", e.f.polls[31])
	}
	if exists(wt) || strings.Contains(gitRun(t, "-C", e.co, "branch", "--list", "feat/27-ntfy-push"), "feat") {
		t.Error("worktree or branch left behind")
	}
	contains(t, out, "rebased feat/27-ntfy-push onto origin/develop", "3 gate(s) passed", "develop fast-forwarded",
		"PR #31 merged")
}

func TestQueueMergesInPriorityOrderThenOldest(t *testing.T) {
	e := setup(t, gateSets)
	for _, c := range []struct {
		n        int
		created  string
		priority string
		label    string
	}{
		{1, "2026-11-20T01:00:00Z", "P2", ""},
		{2, "2026-11-22T03:00:00Z", "P0", ""},
		{3, "2026-11-21T02:00:00Z", "", "P1"}, // label when the board has none
		{4, "2026-11-19T00:00:00Z", "", ""},   // oldest, but no priority
		{5, "2026-11-19T00:00:00Z", "P2", ""}, // P2 and older than #1
	} {
		branch := "feat/" + string(rune('0'+c.n)) + "-task"
		e.task(t, c.n, branch, branch[5:6]+".txt", "x\n")
		e.f.addPR(t, branch, 40+c.n, c.created)
		e.f.priority[c.n] = c.priority
		if c.label != "" {
			e.f.labels[c.n] = []string{"enhancement", c.label}
		}
	}
	out, err := e.merge(t, 1, 2, 3, 4, 5)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	contains(t, out, "queue: "+relay+"#2 (P0, PR #42), "+relay+"#3 (P1, PR #43), "+relay+"#5 (P2, PR #45), "+
		relay+"#1 (P2, PR #41), "+relay+"#4 (no priority, PR #44)")
	var got []string
	for _, s := range e.develop(t) {
		got = append(got, s[strings.Index(s, "#"):])
	}
	if !slices.Equal(got, []string{"#4)", "#1)", "#5)", "#3)", "#2)", "#1)"}) { // newest first; the last is init
		t.Errorf("develop: %q", e.develop(t))
	}
	if len(e.f.gates) != 15 {
		t.Errorf("%d gate runs, want 3 per PR", len(e.f.gates))
	}
}

func TestConflictStopsThatPROnlyAndListsTheFiles(t *testing.T) {
	e := setup(t, gateSets)
	e.task(t, 1, "feat/1-first", "README.md", "one\nTWO\nthree\n")
	wt := e.task(t, 2, "feat/2-second", "README.md", "one\n2\nthree\n")
	e.task(t, 3, "feat/3-third", "third.txt", "x\n")
	for n, p := range map[int]string{1: "P0", 2: "P1", 3: "P2"} {
		e.f.addPR(t, []string{"", "feat/1-first", "feat/2-second", "feat/3-third"}[n], 50+n, "2026-11-23T00:00:00Z")
		e.f.priority[n] = p
	}
	before := gitRun(t, "-C", wt, "rev-parse", "HEAD")
	out, err := e.merge(t, 3, 2, 1)
	if err == nil || err.Error() != "1 of 3 PR(s) not merged" {
		t.Fatalf("err %v\n%s", err, out)
	}
	contains(t, out, relay+"#2: FAIL rebase onto origin/develop conflicts in README.md (aborted; feat/2-second is unchanged)")
	if got := e.develop(t); len(got) != 3 || !strings.HasSuffix(got[0], "(#3)") || !strings.HasSuffix(got[1], "(#1)") {
		t.Errorf("develop: %q", got)
	}
	if gitRun(t, "-C", wt, "rev-parse", "HEAD") != before || gitRun(t, "-C", wt, "status", "--porcelain") != "" ||
		gitRun(t, "-C", wt, "rev-parse", "--abbrev-ref", "HEAD") != "feat/2-second" {
		t.Error("the conflicting worktree was left mid-rebase or moved")
	}
}

func TestFailingGateStopsThePR(t *testing.T) {
	for name, fail := range map[string]struct {
		cmd  string
		run  func() (string, error)
		want string
	}{
		"test": {"go test ./...", func() (string, error) {
			return "", &ext.Error{Code: 1, Msg: "env GOPROXY=off sh: exit 1: --- FAIL: TestPush"}
		}, "test gate `go test ./...` failed: env GOPROXY=off sh: exit 1: --- FAIL: TestPush"},
		"format": {"gofmt -l .", func() (string, error) { return "push.go\nrelay/a.go\n", nil },
			"format gate `gofmt -l .` lists push.go, relay/a.go"},
		"review": {"go test -race ./...", func() (string, error) {
			return "", &ext.Error{Code: 1, Msg: "env GOPROXY=off sh: exit 1: DATA RACE"}
		}, "review gate `go test -race ./...` failed"},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, gateSets)
			wt := e.task(t, 27, "feat/27-ntfy-push", "push.go", "package relay\n")
			e.f.addPR(t, "feat/27-ntfy-push", 31, "2026-11-23T02:05:00Z")
			e.f.fail[fail.cmd] = fail.run
			out, err := e.merge(t, 27)
			if err == nil {
				t.Fatal("merged")
			}
			contains(t, out, relay+"#27: FAIL "+fail.want)
			if len(e.f.pushes()) != 0 || len(e.develop(t)) != 1 || !exists(wt) {
				t.Errorf("pushed or cleaned up after a failing gate: %q", e.f.pushes())
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		prep     func(t *testing.T, e *env)
		open     bool
		want     string
	}{
		{"worktree on main", gateSets, func(t *testing.T, e *env) {
			gitRun(t, "-C", e.co, "worktree", "add", "-q", filepath.Join(e.cfg.Worktrees("hermes-relay"), "issue-27"), "main")
		}, true, "is on main, not a task branch <type>/27-<slug>; refusing"},
		{"merge from main", gateSets, func(t *testing.T, e *env) {
			gitRun(t, "-C", e.co, "worktree", "add", "-q", filepath.Join(e.cfg.Worktrees("hermes-relay"), "issue-27"), "main")
		}, false, "is on main, not a task branch"},
		{"another issue's branch", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/28-other", "a.txt", "x\n")
		}, false, "is on feat/28-other, not a task branch <type>/27-<slug>"},
		{"hotfix branch", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "hotfix/27-urgent", "a.txt", "x\n")
		}, true, "is on hotfix/27-urgent, not a task branch"},
		{"repo outside gitflow", "base_branch = \"main\"\n" + gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
		}, false, `hermes-relay has base_branch "main": pr works only for repos whose task branches start from develop`},
		{"dirty worktree", gateSets, func(t *testing.T, e *env) {
			writeFile(t, filepath.Join(e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n"), "b.txt"), "y\n")
		}, true, "has uncommitted changes"},
		{"bad subject", gateSets, func(t *testing.T, e *env) {
			wt := e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
			gitRun(t, "-C", wt, "commit", "-q", "--amend", "-m", "wip")
		}, true, `commit subject "wip" should be <type>(<scope>): <what> (#N)`},
		{"PR into main", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
			e.f.addPR(t, "feat/27-ntfy-push", 31, "2026-11-23T02:05:00Z")["baseRefName"] = "main"
		}, false, "PR #31 is into main, not develop; refusing"},
		{"no review by the account", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
			v := e.f.addPR(t, "feat/27-ntfy-push", 31, "2026-11-23T02:05:00Z")
			v["comments"].([]any)[0].(map[string]any)["author"] = map[string]any{"login": "someone-else"}
		}, false, "PR #31 has no review comment by xMuramasa (pr open --review FILE)"},
		{"closed PR", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
			e.f.addPR(t, "feat/27-ntfy-push", 31, "2026-11-23T02:05:00Z")["state"] = "CLOSED"
		}, false, "PR #31 is closed"},
		{"no PR", gateSets, func(t *testing.T, e *env) {
			e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
		}, false, "no pull requests found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := setup(t, c.settings)
			c.prep(t, e)
			review := filepath.Join(e.home, "review.md")
			writeFile(t, review, "ok\n")
			var w bytes.Buffer
			var err error
			if c.open {
				err = e.pr(&w).Open(ref(27), review)
			} else {
				err = e.pr(&w).Merge([]dispatch.Ref{ref(27)})
			}
			if err == nil || !strings.Contains(w.String()+err.Error(), c.want) {
				t.Errorf("err %v, output %q; want %q", err, w.String(), c.want)
			}
			if p := e.f.pushes(); len(p) != 0 || len(e.f.created) != 0 {
				t.Errorf("pushed %q or opened %q", p, e.f.created)
			}
		})
	}
}

func TestPushRefusesMainAndNonFastForwardDevelop(t *testing.T) {
	e := setup(t, gateSets)
	e.task(t, 27, "feat/27-ntfy-push", "a.txt", "x\n")
	other := e.task(t, 28, "fix/28-other", "b.txt", "y\n")
	gitRun(t, "-C", other, "push", "-q", "origin", "HEAD:develop")
	var w bytes.Buffer
	p := e.pr(&w)
	tk, err := p.load(ref(27))
	if err != nil {
		t.Fatal(err)
	}
	for dest, want := range map[string]string{
		"main":          "refusing to push to main",
		"fix/28-other":  "refusing to push to fix/28-other",
		"develop":       "feat/27-ntfy-push is not a fast-forward of origin/develop; refusing to push develop",
		"refs/heads/xx": "refusing to push to refs/heads/xx",
	} {
		if err := p.push(tk, dest); err == nil || err.Error() != want {
			t.Errorf("push %s: %v", dest, err)
		}
	}
	if len(e.f.pushes()) != 0 || gitRun(t, "--git-dir", e.origin, "rev-parse", "develop") != gitRun(t, "-C", other, "rev-parse", "HEAD") {
		t.Error("something was pushed")
	}
}
