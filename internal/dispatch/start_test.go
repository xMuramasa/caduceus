package dispatch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var (
	notes    = map[string]any{"author": map[string]any{"login": "xMuramasa"}, "body": "## Notes from Hermes\n\nThe helper lives in `agents/`."}
	oldNotes = map[string]any{"body": "Notes from Hermes (old)\n\nstale"}
)

const branch27 = "feat/27-agents-helper-read-runs-local"

func startEnv(t *testing.T) (*env, string) {
	e := checkEnv(t)
	e.f.issue["comments"] = []any{oldNotes, map[string]any{"body": "unrelated"}, notes}
	nf := filepath.Join(t.TempDir(), "notes.md")
	writeFile(t, nf, "Extra notes from the file.\n")
	return e, nf
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDryRunWritesFilesOnly(t *testing.T) {
	e, nf := startEnv(t)
	out := filepath.Join(t.TempDir(), "out")
	text, err := e.start(t, Options{DryRun: true, Out: out, Notes: nf})
	if err != nil {
		t.Fatal(err, text)
	}
	entries, _ := os.ReadDir(out)
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if want := []string{name27 + "-brief.md", name27 + "-launch.sh", name27 + "-rules.md", name27 + ".run.json"}; !reflect.DeepEqual(names, want) {
		t.Errorf("files %q", names)
	}
	contains(t, text, "[5/8] worktree: would create or reuse",
		"on "+branch27+" from origin/develop (dry run)", "[8/8] github: skipped (dry run)")
	wt := filepath.Join(e.home, "Projects/hermes-relay-wt/issue-27")
	if _, err := os.Stat(filepath.Dir(wt)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a dry run made the worktree folder")
	}
	if _, err := os.Stat(e.runs); !errors.Is(err, os.ErrNotExist) {
		t.Error("a dry run made the runs folder")
	}
	if e.f.wroteToGitHub() || len(e.f.calls("systemd-run")) > 0 || len(e.f.calls("date")) > 0 {
		t.Errorf("calls %q", e.f.x.Calls)
	}
	for _, c := range e.f.calls("git") {
		if c[3] != "show" { // reading the settings is the only git a dry run does
			t.Errorf("git call %q", c)
		}
	}

	brief := read(t, filepath.Join(out, name27+"-brief.md"))
	if !strings.HasPrefix(brief, "# xMuramasa/hermes-relay#27: Agents helper: read runs") ||
		!strings.HasSuffix(brief, "Extra notes from the file.\n") || strings.Contains(brief, "stale") {
		t.Errorf("brief:\n%s", brief)
	}
	contains(t, brief, "You work in the git worktree `"+wt+"` (branch `"+branch27+"`)",
		"`"+e.runs+"/"+name27+"-progress.md`", "- [ ] Default `-runs` to `~/.local/state/hermes-runs`",
		"## Notes from Hermes\n\nThe helper lives in `agents/`.")
	rules := read(t, filepath.Join(out, name27+"-rules.md"))
	if !strings.HasPrefix(rules, "# Rules for this run (hermes-relay #27, parallel worktree)\n\n- ") {
		t.Errorf("rules:\n%s", rules)
	}
	contains(t, rules, "Work only inside `"+wt+"`", "`"+name27+"-progress.md` and `"+name27+"-report.md`",
		"Never touch `"+e.cfg.Checkout("hermes-relay")+"` (the main checkout)",
		"Do not edit `apps/**`, `probe/**`, `compose*.yml`, `Dockerfile*`, `.caduceus/**`: they belong to Hermes.",
		"Gates, from the worktree root: `go -C relay test ./...`, ", "`gofmt -l relay agents term`. Run them",
		"Hermes runs `go -C relay test -race ./...`, ", "No network: no WebFetch",
		"Never log tokens, pairing codes, image bytes")
	launch := read(t, filepath.Join(out, name27+"-launch.sh"))
	if !strings.HasPrefix(launch, "cd "+wt+" && GOFLAGS=-mod=readonly GOPROXY=off claude -p \"$(cat "+e.runs+"/"+name27+"-brief.md)\"") {
		t.Errorf("launch:\n%s", launch)
	}
	contains(t, launch, "--model opus --effort high", "--add-dir "+e.runs+" --model")
	if strings.Contains(launch, "Agents helper") {
		t.Error("issue text reached the command line")
	}
	sh := exec.Command("bash", "-n")
	sh.Stdin = strings.NewReader(launch)
	if b, err := sh.CombinedOutput(); err != nil {
		t.Errorf("bash -n: %v %s", err, b)
	}
	meta := map[string]any{}
	for k, v := range e.readRunIn(t, out) {
		if k != "title" {
			meta[k] = v
		}
	}
	want := map[string]any{"id": "run_" + name27, "repo": relay, "issue": float64(27), "model": "opus", "effort": "high",
		"worktree": wt, "branch": branch27, "unit": unit27, "started_at": "2026-11-23T03:31:00+00:00"}
	if !reflect.DeepEqual(meta, want) {
		t.Errorf("run.json %v", meta)
	}
}

func (e *env) readRunIn(t *testing.T, dir string) map[string]any {
	saved := e.runs
	defer func() { e.runs = saved }()
	e.runs = dir
	return e.readRun(t, name27)
}

// flags lists a launch line's --options in order: the verified shape.
func flags(line string) []string {
	return regexp.MustCompile(`(?:^| )(--[a-z-]+)`).FindAllString(line, -1)
}

func TestDryRunLaunchHasTheVerifiedShape(t *testing.T) {
	e, _ := startEnv(t)
	out := t.TempDir()
	if text, err := e.start(t, Options{DryRun: true, Out: out}); err != nil {
		t.Fatal(err, text)
	}
	got := read(t, filepath.Join(out, name27+"-launch.sh"))
	example := read(t, testdata("launch-examples", "trismegistus-relay-25-launch.sh"))
	if g, w := flags(got), flags(example); !reflect.DeepEqual(g, w) {
		t.Errorf("flags %q\nwant %q", g, w)
	}
	shape := regexp.MustCompile(`^cd \S+ && (\w+=\S+ )*claude -p "\$\(cat \S+-brief\.md\)" .* --verbose > \S+\.stream\.jsonl 2> \S+\.stderr; echo "\S+ exit \$\?"\n$`)
	for _, l := range []string{got, example} {
		if !shape.MatchString(l) {
			t.Errorf("not the launch shape:\n%s", l)
		}
	}
}

func TestStartCreatesWorktreeLaunchesAndReports(t *testing.T) {
	e, _ := startEnv(t)
	out, err := e.start(t, Options{Model: "sonnet", Effort: "max"})
	if err != nil {
		t.Fatal(err, out)
	}
	if n := len(regexp.MustCompile(`(?m)^\[\d/8\] `).FindAllString(out, -1)); n != 8 {
		t.Errorf("%d step lines:\n%s", n, out)
	}
	wt := filepath.Join(e.home, "Projects/hermes-relay-wt/issue-27")
	if b := gitRun(t, "-C", wt, "branch", "--show-current"); b != branch27+"\n" {
		t.Errorf("branch %q", b)
	}
	if _, err := os.Stat(filepath.Join(wt, "develop.txt")); err != nil {
		t.Error("not based on origin/develop")
	}
	contains(t, out, "created", "[7/8] launch: "+unit27+" started (run_"+name27+", sonnet/max)")
	for _, s := range []string{"-brief.md", "-rules.md", "-launch.sh", ".run.json"} {
		if _, err := os.Stat(filepath.Join(e.runs, name27+s)); err != nil {
			t.Error(err)
		}
	}
	launch := e.f.calls("systemd-run")
	if len(launch) != 1 {
		t.Fatalf("calls %q", launch)
	}
	c := launch[0]
	if !reflect.DeepEqual(c[:6], []string{"systemd-run", "--user", "--unit=caduceus-" + name27, "--collect", "-p",
		"WorkingDirectory=" + wt}) || !strings.HasPrefix(c[6], "--setenv=PATH=") ||
		!reflect.DeepEqual(c[7:], []string{"bash", filepath.Join(e.runs, name27+"-launch.sh")}) {
		t.Errorf("launch %q", c)
	}
	if want := []string{"Started 03:31 (sonnet, run_hermes-relay-issue-27)"}; !reflect.DeepEqual(e.f.comments, want) {
		t.Errorf("comments %q", e.f.comments)
	}
	want := []map[string]string{{"p": "PVT_kwHOAkoXDc4Bo1_m", "i": "PVTI_FAKE", "f": "PVTSSF_lAHOAkoXDc4Bo1_mzhnUMsA",
		"o": "47fc9ee4"}}
	if !reflect.DeepEqual(e.f.mutations, want) {
		t.Errorf("mutations %v", e.f.mutations)
	}
	if m := e.readRun(t, name27); m["effort"] != "max" || m["model"] != "sonnet" {
		t.Errorf("run.json %v", m)
	}
}

func TestRetryArchivesEarlierAttemptAndReusesCleanWorktree(t *testing.T) {
	e, _ := startEnv(t)
	if out, err := e.start(t, Options{}); err != nil {
		t.Fatal(err, out)
	}
	out, err := e.start(t, Options{})
	if err == nil || !strings.Contains(out, "[1/8] issue: FAIL not running: "+unit27+" is active") {
		t.Errorf("%v\n%s", err, out)
	}
	e.f.active = map[string]bool{}
	writeFile(t, filepath.Join(e.runs, name27+"-progress.md"), "item 1 done\n")
	if out, err = e.start(t, Options{}); err != nil {
		t.Fatal(err, out)
	}
	contains(t, out, "reusing clean worktree", "earlier run archived as hermes-relay-issue-27-attempt1")
	if old := e.readRun(t, name27+"-attempt1"); old["id"] != "run_"+name27+"-attempt1" || old["unit"] != nil {
		t.Errorf("archived %v", old)
	}
	if _, err := os.Stat(filepath.Join(e.runs, name27+"-attempt1-progress.md")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(e.runs, name27+"-progress.md")); !errors.Is(err, os.ErrNotExist) {
		t.Error("progress of the earlier attempt kept its name")
	}
	if m := e.readRun(t, name27); m["unit"] != unit27 {
		t.Errorf("run.json %v", m)
	}
}

func TestDirtyWorktreeIsRefused(t *testing.T) {
	e, _ := startEnv(t)
	if out, err := e.start(t, Options{}); err != nil {
		t.Fatal(err, out)
	}
	e.f.active = map[string]bool{}
	writeFile(t, filepath.Join(e.home, "Projects/hermes-relay-wt/issue-27/new.txt"), "x")
	out, err := e.start(t, Options{})
	if err == nil {
		t.Error("started")
	}
	contains(t, out, "[5/8] worktree: FAIL", "has uncommitted changes")
}

func TestExistingBranchWithoutWorktreeIsReused(t *testing.T) {
	e, _ := startEnv(t)
	gitRun(t, "-C", e.cfg.Checkout("hermes-relay"), "branch", "fix/27-older-title")
	out, err := e.start(t, Options{})
	if err != nil {
		t.Fatal(err, out)
	}
	contains(t, out, "reusing existing branch fix/27-older-title")
	if m := e.readRun(t, name27); m["branch"] != "fix/27-older-title" {
		t.Errorf("run.json %v", m)
	}
}

func TestTwoBranchesForOneIssueAreRefused(t *testing.T) {
	e, _ := startEnv(t)
	co := e.cfg.Checkout("hermes-relay")
	for _, b := range []string{"fix/27-one", "feat/27-two", "feat/270-other-issue"} {
		gitRun(t, "-C", co, "branch", b)
	}
	out, err := e.start(t, Options{})
	if err == nil {
		t.Error("started")
	}
	contains(t, out, "several branches for issue 27: feat/27-two, fix/27-one")
}

func TestLabelModelAndStopsAtQuota(t *testing.T) {
	e, _ := startEnv(t)
	e.f.issue["labels"] = append(e.f.issue["labels"].([]any), map[string]any{"name": "agent: sonnet"})
	out, _ := e.start(t, Options{DryRun: true, Out: t.TempDir()})
	contains(t, out, "model sonnet, effort high")
	e.f.x.Calls = nil
	e.f.x.JSON[summary] = map[string]any{"session": "70%", "weekly": "10%"}
	out, err := e.start(t, Options{})
	if err == nil || strings.Contains(out, "[3/8]") {
		t.Errorf("%v\n%s", err, out)
	}
	contains(t, out, "[2/8] quota: FAIL session 70%, weekly 10%: session over its limit")
	if len(e.f.calls("git")) > 0 || len(e.f.calls("systemd-run")) > 0 {
		t.Errorf("calls %q", e.f.x.Calls)
	}
}

func TestConcurrencyStopsStart(t *testing.T) {
	e, _ := startEnv(t)
	for _, n := range []int{1, 2, 3} {
		e.activeRun(t, "xMuramasa/news-reader", n)
	}
	out, err := e.start(t, Options{})
	if err == nil {
		t.Error("started")
	}
	contains(t, out, "[4/8] concurrency: FAIL slots: 3 of 3 in use")
	if len(e.f.calls("systemd-run")) > 0 {
		t.Error("launched")
	}
}

func TestGitHubFailureAfterLaunchSaysTheRunIsGoing(t *testing.T) {
	e, _ := startEnv(t)
	e.f.date = func() (string, error) { return "", errors.New("date: exit 1") }
	out, err := e.start(t, Options{})
	if err == nil {
		t.Error("no error")
	}
	contains(t, out, "[8/8] github: FAIL", "the run is going")
}

func TestStartWithoutBoardIdsFailsBeforeLaunching(t *testing.T) {
	e, _ := startEnv(t)
	e.cfg.Board.ID = ""
	out, err := e.start(t, Options{})
	if err == nil || out != "[0/8] setup: FAIL set board.id in "+e.cfg.Path+"\n" {
		t.Errorf("%v %q", err, out)
	}
	if out, err = e.start(t, Options{DryRun: true, Out: t.TempDir()}); err != nil { // a dry run doesn't need them
		t.Error(err, out)
	}
}
