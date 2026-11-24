package main

import (
	"flag"
	"fmt"
	"io"
	"slices"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/dispatch"
	"github.com/xMuramasa/caduceus/internal/ext"
)

var efforts = []string{"low", "medium", "high", "xhigh", "max"}

func dispatchCmd(args []string, x ext.Ext, stdout, stderr io.Writer) int {
	help := "usage: caduceus dispatch check <owner/repo#N> [--force-quota]\n" +
		"         eligibility report, no side effects\n" +
		"       caduceus dispatch start <owner/repo#N> [--model M] [--effort E] [--notes FILE] [--force-quota]\n" +
		"                               [--dry-run [--out DIR]]\n" +
		"         worktree, brief, rules, launch, comment, Status In progress\n" +
		"         --model: alias or id (default: an 'agent: opus|sonnet' label, else the settings')\n" +
		"         --effort: low|medium|high|xhigh|max (default: the settings')\n" +
		"         --notes: appended to the brief\n" +
		"         --force-quota: launch even when over the limits or usage is n/a\n" +
		"         --dry-run: read GitHub, the settings and horus-eye, write the files to a temporary folder (or --out)\n"
	if len(args) == 0 || (args[0] != "check" && args[0] != "start") {
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
	fs := flag.NewFlagSet("caduceus dispatch "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var o dispatch.Options
	fs.BoolVar(&o.ForceQuota, "force-quota", false, "")
	fs.StringVar(&o.Model, "model", "", "")
	fs.StringVar(&o.Effort, "effort", "", "")
	fs.StringVar(&o.Notes, "notes", "", "")
	fs.BoolVar(&o.DryRun, "dry-run", false, "")
	fs.StringVar(&o.Out, "out", "", "")
	pos, err := parse(fs, args[1:], 1)
	fs.Visit(func(f *flag.Flag) {
		if err == nil && sub == "check" && f.Name != "force-quota" {
			err = fmt.Errorf("--%s is for start", f.Name)
		}
	})
	var ref dispatch.Ref
	if err == nil {
		ref, err = dispatch.ParseRef(pos[0])
	}
	if err == nil && o.Effort != "" && !slices.Contains(efforts, o.Effort) {
		err = fmt.Errorf("--effort %q is not one of %v", o.Effort, efforts)
	}
	if err == nil && o.Out != "" && !o.DryRun {
		err = fmt.Errorf("--out needs --dry-run")
	}
	if err != nil {
		fmt.Fprintf(stderr, "caduceus dispatch %s: %v\n%s", sub, err, help)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	d := &dispatch.Dispatcher{X: x, Cfg: cfg, W: stdout}
	if sub == "start" {
		if d.Start(ref, o) != nil {
			return 1
		}
		return 0
	}
	ok, err := d.Check(ref, o.ForceQuota)
	if err != nil {
		fmt.Fprintf(stderr, "caduceus dispatch check: %v\n", err)
		return 1
	}
	if !ok {
		return 1
	}
	return 0
}
