// Command caduceus is the one binary: CLI and (later) daemon. SPEC section 14.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/settings"
)

type command struct {
	name, args, help string
	run              func(args []string, x ext.Ext, stdout, stderr io.Writer) int // nil: not built yet
}

var commands = []command{
	{"component", "init|check <repo>", "settings scaffold from a profile; validate", component},
	{"initiative", "list|show <slug>", "initiatives in the workspace repo", nil},
	{"feature", "new|show|workspace <initiative>/<slug>", "a feature's tracking issue, spec and workspace", nil},
	{"pr", "open|merge <owner/repo#N>", "task PR into develop; merge after gates, in queue order", nil},
	{"release", "propose|confirm [<repo>...|<initiative>]", "release PRs; fast-forward main + tag on confirm", nil},
	{"dispatch", "check|start <owner/repo#N> [--model] [--effort] [--notes FILE] [--dry-run] [--force-quota]",
		"eligibility report; start a run", dispatchCmd},
	{"run", "list|status|stop|prune [<run>]", "runs and their files", runCmd},
	{"night", "select|report", "the night run", nil},
	{"squad", "start|status|stop <initiative>/<slug>", "squads for swarm features", nil},
	{"serve", "", "the daemon", nil},
}

func main() { os.Exit(run(os.Args[1:], ext.Real{}, os.Stdout, os.Stderr)) }

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: caduceus <command> ...")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-11s %-45s %s\n", c.name, c.args, c.help)
	}
}

func wantsHelp(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return a == "-h" || a == "-help" || a == "--help" })
}

// run is main without the process: exit 0 ok, 1 failed, 2 usage or not built yet.
func run(args []string, x ext.Ext, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(stdout)
		return 0
	}
	i := slices.IndexFunc(commands, func(c command) bool { return c.name == args[0] })
	if i < 0 {
		fmt.Fprintf(stderr, "caduceus: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
	c := commands[i]
	if c.run != nil {
		return c.run(args[1:], x, stdout, stderr)
	}
	if wantsHelp(args[1:]) {
		fmt.Fprintf(stdout, "usage: caduceus %s %s\n  %s\n", c.name, c.args, c.help)
		return 0
	}
	fmt.Fprintf(stderr, "caduceus %s: not built yet (see #3)\n", c.name)
	return 2
}

// parse lets flags come before or after the positional arguments, and wants exactly n of them.
func parse(fs *flag.FlagSet, args []string, n int) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	if len(pos) != n {
		return nil, fmt.Errorf("want %d argument(s), got %d", n, len(pos))
	}
	return pos, nil
}

func component(args []string, x ext.Ext, stdout, stderr io.Writer) int {
	help := "usage: caduceus component init <repo> [--from-profile FILE]\n" +
		"         print a .caduceus/project.toml: from a hermes-dispatch profile, or a commented scaffold\n" +
		"       caduceus component check <repo> [--file PATH]\n" +
		"         validate the settings on the main checkout's origin/HEAD (or PATH) and print a summary\n"
	if len(args) == 0 || (args[0] != "init" && args[0] != "check") {
		if wantsHelp(args) {
			fmt.Fprint(stdout, help)
			return 0
		}
		fmt.Fprint(stderr, help)
		return 2
	}
	if wantsHelp(args[1:]) {
		fmt.Fprint(stdout, help)
		return 0
	}
	fs := flag.NewFlagSet("caduceus component "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "read the settings from this file instead of origin/HEAD")
	profile := fs.String("from-profile", "", "a hermes-dispatch profile to convert")
	pos, err := parse(fs, args[1:], 1)
	if err == nil && args[0] == "init" && *file != "" {
		err = fmt.Errorf("--file is for check")
	}
	if err == nil && args[0] == "check" && *profile != "" {
		err = fmt.Errorf("--from-profile is for init")
	}
	if err != nil {
		fmt.Fprintf(stderr, "caduceus component %s: %v\n%s", args[0], err, help)
		return 2
	}
	repo := pos[0]
	if args[0] == "init" && *profile == "" {
		fmt.Fprint(stdout, settings.Scaffold(repo))
		return 0
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if args[0] == "init" {
		text, err := settings.FromProfile(*profile, repo, cfg)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprint(stdout, text)
		return 0
	}
	var s *settings.Settings
	if *file != "" {
		s, err = settings.LoadFile(cfg, repo, *file)
	} else {
		s, err = settings.Load(x, cfg, repo)
	}
	if err != nil {
		fmt.Fprintln(stderr, strings.TrimSpace(err.Error()))
		return 1
	}
	fmt.Fprintln(stdout, s.Summary())
	return 0
}
