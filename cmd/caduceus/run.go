package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/runs"
)

func runCmd(args []string, x ext.Ext, stdout, stderr io.Writer) int {
	help := "usage: caduceus run list\n" +
		"         runs: alive / finished\n" +
		"       caduceus run status <run> [--lines N]\n" +
		"         tail of progress, tool-call count, result line\n" +
		"       caduceus run stop <run>\n" +
		"         stop the run's systemd unit (the worktree stays)\n" +
		"       caduceus run prune [--dry-run]\n" +
		fmt.Sprintf("         delete finished runs' files older than %d days (worktrees untouched)\n", runs.PruneDays)
	if len(args) == 0 || !slices.Contains([]string{"list", "status", "stop", "prune"}, args[0]) {
		if wantsHelp(args) {
			fmt.Fprint(stdout, help)
			return 0
		}
		fmt.Fprint(stderr, help)
		return 2
	}
	sub := args[0]
	if wantsHelp(args[1:]) {
		fmt.Fprint(stdout, help)
		return 0
	}
	fs := flag.NewFlagSet("caduceus run "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lines := fs.Int("lines", 5, "progress lines to show")
	dry := fs.Bool("dry-run", false, "list what prune would remove")
	n := 0
	if sub == "status" || sub == "stop" {
		n = 1
	}
	pos, err := parse(fs, args[1:], n)
	fs.Visit(func(f *flag.Flag) {
		if err == nil && f.Name == "lines" && sub != "status" {
			err = fmt.Errorf("--lines is for status")
		}
		if err == nil && f.Name == "dry-run" && sub != "prune" {
			err = fmt.Errorf("--dry-run is for prune")
		}
	})
	if err != nil {
		fmt.Fprintf(stderr, "caduceus run %s: %v\n%s", sub, err, help)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	dir := cfg.Runs
	var r *runs.Run
	if n == 1 {
		var ok bool
		if r, ok = runs.Find(dir, pos[0]); !ok {
			fmt.Fprintf(stderr, "caduceus run %s: no run %s in %s\n", sub, pos[0], dir)
			return 1
		}
	}
	switch sub {
	case "list":
		return runList(x, dir, stdout)
	case "status":
		return runStatus(x, dir, *r, *lines, stdout)
	case "stop":
		return runStop(x, *r, stdout, stderr)
	}
	return runPrune(x, dir, *dry, stdout, stderr)
}

func runList(x ext.Ext, dir string, stdout io.Writer) int {
	now := time.Now()
	var rows [][]string
	for _, r := range runs.LoadAll(dir) {
		alive, state := runs.IsActive(x, r.Unit), "finished"
		outcome := "running"
		if alive {
			state = "alive"
		} else {
			outcome = runs.Outcome(runs.Result(runs.Stream(dir, r.Name)))
		}
		issue := "?"
		if r.Issue != 0 {
			issue = fmt.Sprint(r.Issue)
		}
		rows = append(rows, []string{"run_" + r.Name, state, or(r.Repo, "?") + "#" + issue, or(r.Model, "?"),
			runs.Age(r.StartedAt, now), outcome})
	}
	if len(rows) == 0 {
		fmt.Fprintf(stdout, "no runs in %s\n", dir)
		return 0
	}
	rows = slices.Insert(rows, 0, []string{"RUN", "STATE", "ISSUE", "MODEL", "AGE", "OUTCOME"})
	widths := make([]int, 5)
	for _, row := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len(row[i]))
		}
	}
	for _, row := range rows {
		for i, w := range widths {
			fmt.Fprintf(stdout, "%-*s  ", w, row[i])
		}
		fmt.Fprintln(stdout, row[5])
	}
	return 0
}

func runStatus(x ext.Ext, dir string, r runs.Run, n int, stdout io.Writer) int {
	alive, state := runs.IsActive(x, r.Unit), "inactive"
	if alive {
		state = "active"
	}
	stream := runs.Stream(dir, r.Name)
	fmt.Fprintf(stdout, "run_%s: %s#%d %s\n", r.Name, r.Repo, r.Issue, r.Title)
	fmt.Fprintf(stdout, "unit: %s (%s); model %s/%s; started %s\n", or(r.Unit, "-"), state, r.Model, r.Effort,
		r.StartedAt)
	fmt.Fprintf(stdout, "worktree: %s (%s)\n", r.Worktree, r.Branch)
	fmt.Fprintf(stdout, "tool calls: %d\n", runs.ToolCalls(stream))
	b, _ := os.ReadFile(filepath.Join(dir, r.Name+"-progress.md"))
	if text := strings.TrimRight(string(b), "\n"); text == "" {
		fmt.Fprintln(stdout, "progress: none yet")
	} else {
		lines := strings.Split(text, "\n")
		fmt.Fprintln(stdout, "progress:")
		for _, l := range lines[max(len(lines)-n, 0):] {
			fmt.Fprintf(stdout, "  %s\n", l)
		}
	}
	fmt.Fprintf(stdout, "result: %s\n", runs.Describe(runs.Result(stream), alive))
	return 0
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func runStop(x ext.Ext, r runs.Run, stdout, stderr io.Writer) int {
	if r.Unit == "" {
		fmt.Fprintf(stdout, "run_%s has no unit (an archived attempt); nothing to stop\n", r.Name)
		return 1
	}
	if !runs.IsActive(x, r.Unit) {
		fmt.Fprintf(stdout, "%s is not active\n", r.Unit)
		return 0
	}
	if _, err := x.Run([]string{"systemctl", "--user", "stop", r.Unit}); err != nil {
		fmt.Fprintf(stderr, "caduceus run stop: %v\n", err)
		return 1
	}
	if runs.IsActive(x, r.Unit) {
		fmt.Fprintf(stdout, "%s is still active after stop\n", r.Unit)
		return 1
	}
	fmt.Fprintf(stdout, "stopped %s; worktree %s left in place\n", r.Unit, r.Worktree)
	return 0
}

func runPrune(x ext.Ext, dir string, dry bool, stdout, stderr io.Writer) int {
	verb, k, now := "removed", 0, time.Now()
	if dry {
		verb = "would remove"
	}
	for _, r := range runs.LoadAll(dir) {
		if !runs.Prunable(x, dir, r, now) {
			continue
		}
		fs := runs.Files(dir, r.Name)
		for _, f := range fs {
			if dry {
				continue
			}
			if err := os.Remove(f); err != nil {
				fmt.Fprintf(stderr, "caduceus run prune: %v\n", err)
				return 1
			}
		}
		fmt.Fprintf(stdout, "%s run_%s (%d files)\n", verb, r.Name, len(fs))
		k++
	}
	verb = "pruned"
	if dry {
		verb = "would prune"
	}
	fmt.Fprintf(stdout, "%s %d finished run(s) older than %d days (worktrees untouched)\n", verb, k, runs.PruneDays)
	return 0
}
