package dispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/xMuramasa/caduceus/internal/github"
	"github.com/xMuramasa/caduceus/internal/runs"
	"github.com/xMuramasa/caduceus/internal/settings"
)

func code(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = "`" + x + "`"
	}
	return strings.Join(out, ", ")
}

// NotesComment is the latest comment whose body starts with "Notes from Hermes" (a leading heading or bold mark
// allowed), or "".
func NotesComment(iss *github.Issue) string {
	for i := len(iss.Comments) - 1; i >= 0; i-- {
		body := iss.Comments[i].Body
		if strings.HasPrefix(strings.TrimLeft(body, "#* \t\r\n"), "Notes from Hermes") {
			return strings.TrimSpace(body)
		}
	}
	return ""
}

// Brief is the run's prompt: where it works, where it reports, the issue, Hermes' notes and the --notes file.
func Brief(repo string, iss *github.Issue, worktree, branch, dir, name, notes string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s#%d: %s\n\n", repo, iss.Number, iss.Title)
	fmt.Fprintf(&b, "You work in the git worktree `%s` (branch `%s`), your current directory. Read `SPEC.md` first if "+
		"the repo has one, then the files the issue names.\n\n", worktree, branch)
	fmt.Fprintf(&b, "Append one line to `%s` after each checklist item (time, item, test count). Write the final "+
		"report to `%s`: what you built per checklist item, files, test counts, decisions the issue didn't cover, and "+
		"known gaps.\n\n", filepath.Join(dir, name+"-progress.md"), filepath.Join(dir, name+"-report.md"))
	fmt.Fprintf(&b, "## Issue #%d: %s\n\n%s\n", iss.Number, iss.Title, strings.TrimSpace(iss.Body))
	for _, x := range []string{NotesComment(iss), notes} {
		if x = strings.TrimSpace(x); x != "" {
			fmt.Fprintf(&b, "\n---\n\n%s\n", x)
		}
	}
	return b.String()
}

// Gates are the settings' gates in meaning order (test, build, lint, ...).
func Gates(s *settings.Settings) []string {
	var out []string
	for _, m := range settings.Meanings {
		out = append(out, s.Gates[m]...)
	}
	return out
}

// Rules is the run's rules file (--append-system-prompt-file).
func Rules(s *settings.Settings, n int, worktree, dir, name string) string {
	refs := []string{fmt.Sprintf("`%s` (your brief; other runs' files are not yours)", dir)}
	for _, d := range s.ReadOnlyDirs {
		refs = append(refs, "`"+d+"`")
	}
	lines := []string{
		"You write code; Hermes (the orchestrator) reviews, commits and merges. Never run git commit, add, push, " +
			"checkout, switch, branch, reset, stash or worktree. Read-only git (status, diff, log) is fine.",
		fmt.Sprintf("Work only inside `%s` and only on the paths the issue lists under \"Owns\". Never touch `%s` "+
			"(the main checkout) or another worktree.", worktree, s.Checkout),
		fmt.Sprintf("Read-only references: %s. Never edit them, except your two files in `%s`: `%s-progress.md` and "+
			"`%s-report.md`.", strings.Join(refs, ", "), dir, name, name),
	}
	protected := s.Access.Protected
	if !slices.Contains(protected, ".caduceus/**") { // Tools protects it in every run; say so
		protected = append(slices.Clip(protected), ".caduceus/**")
	}
	lines = append(lines, fmt.Sprintf("Do not edit %s: they belong to Hermes.", code(protected)))
	if len(s.Access.Secret) > 0 {
		lines = append(lines, fmt.Sprintf("Never read, print or copy %s.", code(s.Access.Secret)))
	}
	lines = append(lines, "Do not create CLAUDE.md or AGENTS.md anywhere.", s.Network)
	if g := Gates(s); len(g) > 0 {
		lines = append(lines, fmt.Sprintf("Gates, from the worktree root: %s. Run them at checklist-item ends, not "+
			"after every edit (single test files in between).", code(g)))
	}
	if len(s.Review) > 0 {
		lines = append(lines, fmt.Sprintf("Hermes runs %s at review; don't run them yourself.", code(s.Review)))
	}
	progress, report := filepath.Join(dir, name+"-progress.md"), filepath.Join(dir, name+"-report.md")
	lines = append(lines, s.Rules...)
	lines = append(lines,
		fmt.Sprintf("Budget is shared with Martin's chat and cron jobs: read each file once, take the checklist in "+
			"order, append one line to `%s` after each item (time, item, test count), and stop when \"done when\" "+
			"is met.", progress),
		"Never print or log tokens or credentials; fixture placeholders for secrets are obviously fake (`FAKEFAKE...`).",
		fmt.Sprintf("Finish with the report `%s`: what you built per checklist item, files changed, test counts, "+
			"decisions the issue didn't cover, anything you needed in a file you don't own (as exact requested "+
			"changes), and known gaps.", report),
		"If something you need is impossible under these rules, write it in the report instead of working around "+
			"the rule.")
	var b strings.Builder
	fmt.Fprintf(&b, "# Rules for this run (%s #%d, parallel worktree)\n\n", s.Repo, n)
	for _, l := range lines {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9@%+=:,./_-]+$`)

// quote is Python's shlex.quote.
func quote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Launch is the one shell line hermes-dispatch verified. Issue text never appears here: the brief is read with
// $(cat).
func Launch(s *settings.Settings, name, worktree, dir, model, effort string) string {
	f := func(suffix string) string { return quote(filepath.Join(dir, name+suffix)) }
	allow, deny := s.Tools(worktree)
	var env strings.Builder
	for _, k := range slices.Sorted(maps.Keys(s.Env)) { // shortcut: Env is a map, so keys go sorted, not in TOML order; keep the order if an env var ever depends on another
		fmt.Fprintf(&env, "%s=%s ", k, quote(s.Env[k]))
	}
	var dirs []string
	for _, d := range append([]string{dir}, s.ReadOnlyDirs...) {
		dirs = append(dirs, "--add-dir "+quote(d))
	}
	return fmt.Sprintf("cd %s && %sclaude -p \"$(cat %s)\" --append-system-prompt-file %s %s --model %s --effort %s "+
		"--permission-mode acceptEdits --allowedTools %s --disallowedTools %s --output-format stream-json --verbose "+
		"> %s 2> %s; echo \"%s exit $?\"\n", quote(worktree), env.String(), f("-brief.md"), f("-rules.md"),
		strings.Join(dirs, " "), quote(model), quote(effort), quote(strings.Join(allow, ",")),
		quote(strings.Join(deny, ",")), f(".stream.jsonl"), f(".stderr"), name)
}

// RunJSON is <name>.run.json.
func RunJSON(r runs.Run) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r) // a struct of strings and an int always encodes
	return b.String()
}
