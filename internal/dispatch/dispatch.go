// Package dispatch is `caduceus dispatch check|start` (SPEC sections 7 and 11), ported from hermes-dispatch:
// eligibility, the task worktree, brief, rules and launch script, the transient unit, and the issue's comment and
// Status.
package dispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/github"
	"github.com/xMuramasa/caduceus/internal/quota"
	"github.com/xMuramasa/caduceus/internal/runs"
	"github.com/xMuramasa/caduceus/internal/settings"
	"github.com/xMuramasa/caduceus/internal/worktree"
)

// BlockingLabels keep an issue from being dispatched.
var BlockingLabels = []string{"needs-decision", "needs-design", "needs-phone-check", "on-hold", "phase"}

var (
	refRE   = regexp.MustCompile(`^([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)#(\d+)$`)
	agentRE = regexp.MustCompile(`^agent:\s*(opus|sonnet)$`)
)

// Ref is owner/repo#N: Repo is owner/repo, Name the repo alone.
type Ref struct {
	Repo, Name string
	N          int
}

// ParseRef reads owner/repo#N.
func ParseRef(s string) (Ref, error) {
	m := refRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Ref{}, fmt.Errorf("expected owner/repo#N, got %q", s)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return Ref{}, fmt.Errorf("expected owner/repo#N, got %q", s)
	}
	return Ref{m[1] + "/" + m[2], m[2], n}, nil
}

func (r Ref) String() string { return fmt.Sprintf("%s#%d", r.Repo, r.N) }

// LabelModel is the model an `agent: opus|sonnet` label asks for, or "".
func LabelModel(labels []string) string {
	for _, l := range labels {
		if m := agentRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			return m[1]
		}
	}
	return ""
}

// Check is one line of the eligibility report.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

func firstFail(cs []Check) error {
	for _, c := range cs {
		if !c.OK {
			return fmt.Errorf("%s: %s", c.Name, c.Detail)
		}
	}
	return nil
}

// Dispatcher holds what check and start share. W gets the report lines.
type Dispatcher struct {
	X     ext.Ext
	Cfg   *config.Config
	W     io.Writer
	Sleep func(time.Duration) // between quota readings; nil sleeps for real
	Now   func() time.Time    // nil is time.Now
}

// issueChecks: open, labels, blocked-by, not running.
func (d *Dispatcher) issueChecks(ref Ref, name string, iss *github.Issue) ([]Check, error) {
	blockers, err := github.BlockedBy(d.X, ref.Repo, ref.N)
	if err != nil {
		return nil, err
	}
	var open []string
	for _, b := range blockers {
		if !strings.EqualFold(b.State, "closed") {
			open = append(open, or(b.HTMLURL, "?"))
		}
	}
	var bad []string
	for _, l := range iss.LabelNames() {
		if slices.Contains(BlockingLabels, l) {
			bad = append(bad, l)
		}
	}
	slices.Sort(bad)
	bad = slices.Compact(bad)
	unit := runs.Unit(name)
	running := runs.IsActive(d.X, unit)
	cs := []Check{
		{"open", strings.EqualFold(iss.State, "open"), strings.ToLower(iss.State)},
		{"labels", len(bad) == 0, or(strings.Join(bad, ", "), "none blocking")},
		{"blocked-by", len(open) == 0, "all closed"},
		{"not running", !running, "no active unit"},
	}
	if len(open) > 0 {
		cs[2].Detail = "open: " + strings.Join(open, ", ")
	}
	if running {
		cs[3].Detail = unit + " is active"
	}
	return cs, nil
}

// active lists the other runs whose unit is active.
func (d *Dispatcher) active(name string) []runs.Run {
	var out []runs.Run
	for _, r := range runs.LoadAll(d.Cfg.Runs) {
		if r.Name != name && runs.IsActive(d.X, r.Unit) {
			out = append(out, r)
		}
	}
	return out
}

// concurrency: a free slot, no lock another active run holds, no Owns overlap with an active run in the same repo.
func (d *Dispatcher) concurrency(ref Ref, name string, iss *github.Issue, locks []string) []Check {
	active := d.active(name)
	var ids []string
	for _, r := range active {
		ids = append(ids, "run_"+r.Name)
	}
	slots := Check{"slots", len(active) < d.Cfg.Slots, fmt.Sprintf("%d of %d in use", len(active), d.Cfg.Slots)}
	if len(ids) > 0 {
		slots.Detail += ": " + strings.Join(ids, ", ")
	}
	return []Check{slots, d.lockCheck(locks, active), d.ownsCheck(ref, iss, active)}
}

func (d *Dispatcher) lockCheck(locks []string, active []runs.Run) Check {
	if len(locks) == 0 {
		return Check{"locks", true, "none needed"}
	}
	var held []string
	for _, r := range active {
		_, repo, _ := strings.Cut(r.Repo, "/")
		s, err := settings.Load(d.X, d.Cfg, repo)
		if err != nil {
			return Check{"locks", false, fmt.Sprintf("can't read the locks of run_%s: %v", r.Name, oneLine(err))}
		}
		for _, l := range s.Locks {
			if slices.Contains(locks, l) {
				held = append(held, fmt.Sprintf("%s held by run_%s", l, r.Name))
			}
		}
	}
	if len(held) > 0 {
		return Check{"locks", false, strings.Join(held, ", ")}
	}
	return Check{"locks", true, strings.Join(locks, ", ") + " free"}
}

func (d *Dispatcher) ownsCheck(ref Ref, iss *github.Issue, active []runs.Run) Check {
	mine := Owns(iss.Body)
	if len(mine) == 0 {
		return Check{"owns", true, "no Owns"}
	}
	k := 0
	for _, r := range active {
		if r.Repo != ref.Repo {
			continue
		}
		k++
		other, err := github.View(d.X, r.Repo, r.Issue)
		if err != nil {
			return Check{"owns", false, fmt.Sprintf("can't read the issue of run_%s: %v", r.Name, err)}
		}
		for _, a := range mine {
			for _, b := range Owns(other.Body) {
				if Overlap(a, b) {
					return Check{"owns", false, fmt.Sprintf("`%s` overlaps `%s` of run_%s (#%d)", a, b, r.Name,
						r.Issue)}
				}
			}
		}
	}
	return Check{"owns", true, fmt.Sprintf("%s; no overlap with %d active run(s) in %s", code(mine), k, ref.Repo)}
}

// quota checks the day limits, printing a line before each retry of an unknown reading.
func (d *Dispatcher) quota(force bool) (bool, string) {
	q, err := quota.New(d.X, d.Cfg)
	if err != nil {
		return false, err.Error()
	}
	sleep, k := d.Sleep, 0
	if sleep == nil {
		sleep = time.Sleep
	}
	q.Sleep = func(w time.Duration) {
		k++
		fmt.Fprintf(d.W, "usage unknown, retrying in %d s (%d/%d)\n", int(w.Seconds()), k, quota.Retries)
		sleep(w)
	}
	return q.Check(d.Cfg.Quota.Day, force)
}

func oneLine(err error) string { return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ") }

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Check prints the eligibility report and the verdict; the error is for what kept the report from being made.
func (d *Dispatcher) Check(ref Ref, force bool) (bool, error) {
	name, err := runs.Name(ref.Name, ref.N)
	if err != nil {
		return false, err
	}
	iss, err := github.View(d.X, ref.Repo, ref.N)
	if err != nil {
		return false, err
	}
	cs, err := d.issueChecks(ref, name, iss)
	if err != nil {
		return false, err
	}
	sc := Check{"settings", true, ""}
	s, err := settings.Load(d.X, d.Cfg, ref.Name)
	var locks []string
	if err != nil {
		sc.OK, sc.Detail = false, oneLine(err)
	} else {
		sc.Detail, locks = s.Summary(), s.Locks
	}
	cs = append(cs, sc)
	cs = append(cs, d.concurrency(ref, name, iss, locks)...)
	for _, c := range cs {
		d.print(c)
	}
	qc := Check{Name: "quota"}
	qc.OK, qc.Detail = d.quota(force) // last: it can take minutes
	d.print(qc)
	cs = append(cs, qc)
	var failed []string
	for _, c := range cs {
		if !c.OK {
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintf(d.W, "verdict: %s is not eligible (%s)\n", ref, strings.Join(failed, ", "))
		return false, nil
	}
	fmt.Fprintf(d.W, "verdict: %s can be dispatched\n", ref)
	return true, nil
}

func (d *Dispatcher) print(c Check) {
	mark := "ok  "
	if !c.OK {
		mark = "FAIL"
	}
	fmt.Fprintf(d.W, "%s %s: %s\n", mark, c.Name, c.Detail)
}

// Options are start's flags.
type Options struct {
	Model, Effort string // "" takes the label's or the settings'
	Notes         string // a file appended to the brief
	DryRun        bool
	Out           string // with DryRun: where the files go (default a new temporary folder)
	ForceQuota    bool
}

// steps prints one line per start step: "[k/total] title: detail".
type steps struct {
	w     io.Writer
	k     int
	title string
}

const totalSteps = 8

func (s *steps) begin(title string) { s.k, s.title = s.k+1, title }

func (s *steps) done(detail string) {
	fmt.Fprintf(s.w, "[%d/%d] %s: %s\n", s.k, totalSteps, s.title, detail)
}

// Start runs the steps: issue, quota, settings, concurrency, worktree, brief and rules, launch, github. A failure is
// printed as its step's FAIL line and returned.
func (d *Dispatcher) Start(ref Ref, o Options) error {
	s := &steps{w: d.W, title: "setup"}
	err := d.start(ref, o, s)
	if err != nil {
		msg := oneLine(err)
		if s.title == "github" {
			msg += " (the run is going; comment and set Status by hand)"
		}
		fmt.Fprintf(d.W, "[%d/%d] %s: FAIL %s\n", s.k, totalSteps, s.title, msg)
	}
	return err
}

func (d *Dispatcher) start(ref Ref, o Options, s *steps) error {
	var notes string
	if o.Notes != "" {
		b, err := os.ReadFile(o.Notes)
		if err != nil {
			return err
		}
		notes = string(b)
	}
	if !o.DryRun { // fail before launching, not after
		if err := d.Cfg.Require("board.id", "board.status_field"); err != nil {
			return err
		}
		if d.Cfg.Board.Status["In progress"] == "" {
			return fmt.Errorf("set board.status.\"In progress\" in %s", d.Cfg.Path)
		}
	}

	s.begin("issue")
	name, err := runs.Name(ref.Name, ref.N)
	if err != nil {
		return err
	}
	iss, err := github.View(d.X, ref.Repo, ref.N)
	if err != nil {
		return err
	}
	labels := iss.LabelNames()
	cs, err := d.issueChecks(ref, name, iss)
	if err != nil {
		return err
	}
	if err := firstFail(cs); err != nil {
		return err
	}
	s.done(fmt.Sprintf("%s \"%s\": open, no blocking labels, blockers closed, not running", ref, iss.Title))

	s.begin("quota")
	ok, detail := d.quota(o.ForceQuota)
	if !ok {
		return errors.New(detail)
	}
	s.done(detail)

	s.begin("settings")
	st, err := settings.Load(d.X, d.Cfg, ref.Name)
	if err != nil {
		return err
	}
	model := or(o.Model, or(LabelModel(labels), st.Model))
	effort := or(o.Effort, st.Effort)
	s.done(fmt.Sprintf("origin/HEAD:%s: model %s, effort %s", settings.File, model, effort))

	s.begin("concurrency")
	cs = d.concurrency(ref, name, iss, st.Locks)
	if err := firstFail(cs); err != nil {
		return err
	}
	s.done(fmt.Sprintf("slots %s; locks %s; owns %s", cs[0].Detail, cs[1].Detail, cs[2].Detail))

	s.begin("worktree")
	branch := worktree.Branch(ref.N, iss.Title, labels)
	root := d.Cfg.Worktrees(ref.Name)
	var wt string
	if o.DryRun {
		wt = worktree.Path(root, ref.N)
		s.done(fmt.Sprintf("would create or reuse %s on %s from origin/%s (dry run)", wt, branch, st.BaseBranch))
	} else {
		var what string
		if wt, branch, what, err = worktree.Ensure(d.X, st.Checkout, root, st.BaseBranch, ref.N, branch); err != nil {
			return err
		}
		s.done(what)
	}

	s.begin("brief and rules")
	dir := d.Cfg.Runs
	// A dry run renders the real paths (what start would launch) but writes the files elsewhere.
	out := dir
	if o.DryRun {
		if out = o.Out; out == "" {
			if out, err = os.MkdirTemp("", "caduceus-"+name+"-"); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var prior string
	if !o.DryRun {
		if prior, err = runs.Archive(dir, name); err != nil {
			return err
		}
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	unit := runs.Unit(name)
	meta := runs.Run{ID: "run_" + name, Repo: ref.Repo, Issue: iss.Number, Title: iss.Title, Model: model,
		Effort: effort, Worktree: wt, Branch: branch, StartedAt: now().UTC().Format("2006-01-02T15:04:05-07:00"),
		Unit: unit}
	files := [][2]string{
		{"-brief.md", Brief(ref.Repo, iss, wt, branch, dir, name, notes)},
		{"-rules.md", Rules(st, ref.N, wt, dir, name)},
		{"-launch.sh", Launch(st, name, wt, dir, model, effort)},
		{".run.json", RunJSON(meta)},
	}
	path := func(suffix string) string { return filepath.Join(out, name+suffix) }
	for _, f := range files {
		if err := os.WriteFile(path(f[0]), []byte(f[1]), 0o644); err != nil {
			return err
		}
	}
	detail = path("-brief.md") + ", " + path("-rules.md")
	if prior != "" {
		detail += fmt.Sprintf(" (earlier run archived as %s)", prior)
	}
	s.done(detail)

	s.begin("launch")
	if o.DryRun {
		s.done(fmt.Sprintf("%s, %s written; not launched (dry run)", path("-launch.sh"), path(".run.json")))
		s.begin("github")
		s.done("skipped (dry run)")
		return nil
	}
	if _, err := d.X.Run([]string{"systemd-run", "--user", "--unit=" + strings.TrimSuffix(unit, ".service"),
		"--collect", "-p", "WorkingDirectory=" + wt, "--setenv=PATH=" + os.Getenv("PATH"), "bash",
		path("-launch.sh")}); err != nil {
		return err
	}
	s.done(fmt.Sprintf("%s started (run_%s, %s/%s)", unit, name, model, effort))

	s.begin("github")
	hhmm, err := d.X.Run([]string{"date", "+%H:%M"})
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Started %s (%s, run_%s)", strings.TrimSpace(hhmm), model, name)
	if err := github.Comment(d.X, ref.Repo, ref.N, msg); err != nil {
		return err
	}
	if err := github.SetStatus(d.X, d.Cfg.Board, ref.Repo, ref.N, "In progress"); err != nil {
		return err
	}
	s.done(fmt.Sprintf("commented \"%s\", Status In progress", msg))
	return nil
}
