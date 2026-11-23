package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/ext/exttest"
)

// systemd fakes systemctl --user is-active|stop over a set of active units.
type systemd struct {
	active   map[string]bool
	stopKeep bool // stop leaves the unit active
}

func (s *systemd) fake(t *testing.T) *exttest.Fake {
	return &exttest.Fake{T: t, Commands: map[string]func([]string) (string, error){
		"systemctl": func(a []string) (string, error) {
			switch a[1] {
			case "is-active":
				if s.active[a[2]] {
					return "active\n", nil
				}
				return "inactive\n", &ext.Error{Args: a, Code: 3, Msg: "systemctl --user is-active: exit 3: inactive"}
			case "stop":
				if !s.stopKeep {
					delete(s.active, a[2])
				}
				return "", nil
			}
			t.Errorf("unexpected systemctl %q", a)
			return "", nil
		},
	}}
}

type runsFixture struct {
	dir string
	sd  *systemd
}

func (f runsFixture) add(t *testing.T, name, unit string, stream []map[string]any, progress string, ageDays int) {
	t.Helper()
	meta := map[string]any{"id": "run_" + name, "repo": "xMuramasa/news-reader", "issue": 11, "title": "Feed UI",
		"model": "sonnet", "effort": "high", "worktree": "/wt", "branch": "issue-11",
		"started_at": "2026-11-02T01:00:00+00:00", "unit": nil}
	if unit != "" {
		meta["unit"] = unit
	}
	b, _ := json.Marshal(meta)
	var sb strings.Builder
	for _, e := range stream {
		l, _ := json.Marshal(e)
		sb.Write(append(l, '\n'))
	}
	sb.WriteString(`{"trunc`)
	files := map[string]string{".run.json": string(b), ".stream.jsonl": sb.String()}
	if progress != "" {
		files["-progress.md"] = progress
	}
	at := time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour)
	for suffix, text := range files {
		p := filepath.Join(f.dir, name+suffix)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if ageDays > 0 {
			if err := os.Chtimes(p, at, at); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func setupRuns(t *testing.T) runsFixture {
	f := runsFixture{filepath.Join(home(t), ".local/state/caduceus/runs"), &systemd{active: map[string]bool{}}}
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := map[string]any{"type": "assistant", "message": map[string]any{"content": []any{
		map[string]any{"type": "text", "text": "x"}, map[string]any{"type": "tool_use", "name": "Read"}}}}
	done := map[string]any{"type": "result", "subtype": "success", "is_error": false, "num_turns": 12,
		"duration_ms": 600000, "total_cost_usd": 1.5, "result": "All items done.\nmore"}
	var progress strings.Builder
	for i := range 7 {
		fmt.Fprintf(&progress, "10:0%d item %d\n", i, i)
	}
	f.add(t, "news-reader-issue-11", "caduceus-news-reader-issue-11.service", []map[string]any{tool, tool, tool},
		progress.String(), 0)
	f.add(t, "news-reader-issue-9", "caduceus-news-reader-issue-9.service", []map[string]any{tool, done}, "", 31)
	f.add(t, "news-reader-issue-9-attempt1", "", []map[string]any{tool}, "", 40)
	f.add(t, "news-reader-issue-8", "caduceus-news-reader-issue-8.service", []map[string]any{done}, "", 2)
	f.sd.active["caduceus-news-reader-issue-11.service"] = true
	return f
}

func TestRunList(t *testing.T) {
	f := setupRuns(t)
	code, out, _ := cli(t, f.sd.fake(t), "run", "list")
	if code != 0 {
		t.Fatal(code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "RUN ") || len(lines) != 5 {
		t.Fatalf("%q", out)
	}
	rows := map[string][]string{}
	for _, l := range lines[1:] {
		rows[strings.Fields(l)[0]] = strings.Fields(l)
	}
	live := rows["run_news-reader-issue-11"]
	if !reflect.DeepEqual(live[:4], []string{"run_news-reader-issue-11", "alive", "xMuramasa/news-reader#11", "sonnet"}) ||
		live[len(live)-1] != "running" {
		t.Errorf("live row %q", live)
	}
	if r := rows["run_news-reader-issue-9"]; r[1] != "finished" || r[len(r)-1] != "success" {
		t.Errorf("finished row %q", r)
	}
	if r := rows["run_news-reader-issue-9-attempt1"]; !strings.HasSuffix(strings.Join(r, " "), "no result") {
		t.Errorf("attempt row %q", r)
	}
	if err := os.RemoveAll(f.dir); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := cli(t, f.sd.fake(t), "run", "list"); code != 0 || out != "no runs in "+f.dir+"\n" {
		t.Errorf("empty: %d %q", code, out)
	}
}

func TestRunStatus(t *testing.T) {
	f := setupRuns(t)
	code, out, _ := cli(t, f.sd.fake(t), "run", "status", "run_news-reader-issue-11")
	if code != 0 || !strings.Contains(out, "tool calls: 3\n") || !strings.Contains(out, "progress:\n  10:02 item 2") ||
		strings.Contains(out, "item 1") || !strings.Contains(out, "result: none yet\n") ||
		!strings.Contains(out, "unit: caduceus-news-reader-issue-11.service (active); model sonnet/high") {
		t.Errorf("%d %q", code, out)
	}
	code, out, _ = cli(t, f.sd.fake(t), "run", "status", "news-reader-issue-9", "--lines", "1")
	if code != 0 || !strings.Contains(out, "result: success, 12 turns, 10 min, $1.50: All items done.\n") ||
		!strings.Contains(out, "progress: none yet\n") {
		t.Errorf("%d %q", code, out)
	}
	code, out, _ = cli(t, f.sd.fake(t), "run", "status", "news-reader-issue-9-attempt1")
	if !strings.Contains(out, "unit: - (inactive)") || !strings.Contains(out, "result: none (ended without a result line)") {
		t.Errorf("%d %q", code, out)
	}
	code, _, errOut := cli(t, f.sd.fake(t), "run", "status", "nope")
	if code != 1 || errOut != "caduceus run status: no run nope in "+f.dir+"\n" {
		t.Errorf("%d %q", code, errOut)
	}
}

func TestRunStop(t *testing.T) {
	f := setupRuns(t)
	x := f.sd.fake(t)
	code, out, _ := cli(t, x, "run", "stop", "news-reader-issue-11")
	if code != 0 || out != "stopped caduceus-news-reader-issue-11.service; worktree /wt left in place\n" {
		t.Errorf("%d %q", code, out)
	}
	if !slices.ContainsFunc(x.Calls, func(c []string) bool {
		return reflect.DeepEqual(c, []string{"systemctl", "--user", "stop", "caduceus-news-reader-issue-11.service"})
	}) {
		t.Errorf("calls %q", x.Calls)
	}
	if code, out, _ = cli(t, f.sd.fake(t), "run", "stop", "news-reader-issue-11"); code != 0 ||
		out != "caduceus-news-reader-issue-11.service is not active\n" {
		t.Errorf("gone: %d %q", code, out)
	}
	if code, out, _ = cli(t, f.sd.fake(t), "run", "stop", "news-reader-issue-9-attempt1"); code != 1 ||
		!strings.Contains(out, "nothing to stop") {
		t.Errorf("archived: %d %q", code, out)
	}
}

func TestRunStopReportsAUnitThatStaysActive(t *testing.T) {
	f := setupRuns(t)
	f.sd.stopKeep = true
	code, out, _ := cli(t, f.sd.fake(t), "run", "stop", "news-reader-issue-11")
	if code != 1 || !strings.Contains(out, "still active after stop") {
		t.Errorf("%d %q", code, out)
	}
}

func TestRunPruneRemovesOnlyOldFinishedRuns(t *testing.T) {
	f := setupRuns(t)
	f.sd.active["caduceus-news-reader-issue-9.service"] = true
	code, out, _ := cli(t, f.sd.fake(t), "run", "prune", "--dry-run")
	if code != 0 || out != "would remove run_news-reader-issue-9-attempt1 (2 files)\n"+
		"would prune 1 finished run(s) older than 30 days (worktrees untouched)\n" {
		t.Errorf("dry run: %d %q", code, out)
	}
	if n, _ := os.ReadDir(f.dir); len(n) != 9 {
		t.Errorf("dry run removed files: %d left", len(n))
	}
	code, out, _ = cli(t, f.sd.fake(t), "run", "prune")
	if code != 0 || !strings.Contains(out, "removed run_news-reader-issue-9-attempt1 (2 files)\n") ||
		!strings.Contains(out, "pruned 1 finished run(s) older than 30 days (worktrees untouched)\n") {
		t.Errorf("%d %q", code, out)
	}
	clear(f.sd.active)
	if _, out, _ = cli(t, f.sd.fake(t), "run", "prune"); !strings.Contains(out, "removed run_news-reader-issue-9 (2 files)") {
		t.Errorf("%q", out)
	}
	var left []string
	entries, _ := os.ReadDir(f.dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := []string{"news-reader-issue-11-progress.md", "news-reader-issue-11.run.json",
		"news-reader-issue-11.stream.jsonl", "news-reader-issue-8.run.json", "news-reader-issue-8.stream.jsonl"}
	if !reflect.DeepEqual(left, want) {
		t.Errorf("left %q", left)
	}
}

func TestRunUsage(t *testing.T) {
	home(t)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"run"}, 2}, {[]string{"run", "nope"}, 2}, {[]string{"run", "--help"}, 0},
		{[]string{"run", "status", "-h"}, 0}, {[]string{"run", "status"}, 2}, {[]string{"run", "list", "x"}, 2},
		{[]string{"run", "stop", "a", "b"}, 2}, {[]string{"run", "list", "--dry-run"}, 2},
		{[]string{"run", "prune", "--lines", "3"}, 2}, {[]string{"run", "status", "a", "--bogus"}, 2},
	} {
		if code, _, _ := cli(t, nil, tc.args...); code != tc.code {
			t.Errorf("%q: exit %d, want %d", tc.args, code, tc.code)
		}
	}
}
