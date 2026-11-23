package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/dispatch"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/pr"
)

func prCmd(args []string, x ext.Ext, stdout, stderr io.Writer) int {
	help := "usage: caduceus pr open <owner/repo#N> --review FILE\n" +
		"         push the task branch, open its PR into develop, post FILE as the review comment\n" +
		"       caduceus pr merge <owner/repo#N>...\n" +
		"         in priority order (P0, P1, P2, then oldest): rebase onto origin/develop, run the gates and review\n" +
		"         gates, fast-forward develop, wait for GitHub to mark the PR merged, remove the worktree and branch\n"
	if len(args) == 0 || (args[0] != "open" && args[0] != "merge") {
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
	fs := flag.NewFlagSet("caduceus pr "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	review := fs.String("review", "", "")
	n := 1
	if sub == "merge" {
		n = -1
	}
	pos, err := parse(fs, args[1:], n)
	if err == nil && sub == "open" && *review == "" {
		err = fmt.Errorf("--review FILE is required")
	}
	if err == nil && sub == "merge" && *review != "" {
		err = fmt.Errorf("--review is for open")
	}
	var refs []dispatch.Ref
	for _, p := range pos {
		r, perr := dispatch.ParseRef(p)
		if err == nil {
			err = perr
		}
		refs = append(refs, r)
	}
	if err != nil {
		fmt.Fprintf(stderr, "caduceus pr %s: %v\n%s", sub, err, help)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	p := &pr.PR{X: x, Cfg: cfg, W: stdout}
	if sub == "open" {
		err = p.Open(refs[0], *review)
	} else {
		err = p.Merge(refs)
	}
	if err != nil {
		fmt.Fprintf(stderr, "caduceus pr %s: %v\n", sub, err)
		return 1
	}
	return 0
}
