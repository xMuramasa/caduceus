package settings

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xMuramasa/caduceus/internal/config"
)

// The base lists of hermes-dispatch's profiles.py, byte for byte: Claude Code matches rule strings literally.
var (
	BaseAllow = append([]string{"Read", "Edit", "Write", "Glob", "Grep"}, wrap("Bash(%s*)",
		"ls", "cat", "find", "mkdir", "git status", "git diff", "git log", "git ls-files", "jq", "wc", "head", "tail",
		"grep", "diff", "sort", "tree", "date")...)
	GitWrites = []string{"commit", "push", "add", "checkout", "switch", "branch", "reset", "stash", "worktree"}
	BaseDeny  = slices.Concat([]string{"WebFetch", "WebSearch"}, wrap("Bash(git %s*)", GitWrites...), wrap("Bash(%s*)",
		"docker", "sudo", "ssh", "scp", "rsync", "curl", "wget", "claude", "codex", "hermes"))
	AgentFiles = []string{"Write(CLAUDE.md)", "Edit(CLAUDE.md)", "Write(AGENTS.md)", "Edit(AGENTS.md)",
		"Write(**/CLAUDE.md)", "Edit(**/CLAUDE.md)", "Write(**/AGENTS.md)", "Edit(**/AGENTS.md)"}
)

func wrap(format string, items ...string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = fmt.Sprintf(format, it)
	}
	return out
}

// rule writes an absolute path the way Claude Code does: //abs/path.
func rule(tool, path string) string { return tool + "(/" + path + ")" }

// join is Python's os.path.join for one glob: absolute (or ~) globs stay as they are, nothing is cleaned.
func join(worktree, glob string) string {
	if strings.HasPrefix(glob, "~") {
		if p, err := config.Expand(glob); err == nil {
			return p
		}
	}
	if strings.HasPrefix(glob, "/") {
		return glob
	}
	return strings.TrimSuffix(worktree, "/") + "/" + glob
}

// Tools gives the (allowed, disallowed) lists for a run in worktree: the base lists plus the settings' entries,
// protected and secret globs, every checkout but the run's own worktree unwritable, and CLAUDE.md/AGENTS.md denied.
func (s *Settings) Tools(worktree string) (allow, deny []string) {
	allow = slices.Concat(BaseAllow, s.Access.Allow)
	deny = slices.Concat(BaseDeny, s.Access.Deny)
	// .caduceus/** is protected in every run (ADR 0004), so a run can't loosen its own settings.
	for _, g := range append([]string{".caduceus/**"}, s.Access.Protected...) {
		deny = append(deny, rule("Edit", join(worktree, g)), rule("Write", join(worktree, g)))
	}
	for _, g := range s.Access.Secret {
		p := join(worktree, g)
		deny = append(deny, rule("Read", p), rule("Edit", p), rule("Write", p))
	}
	for _, d := range slices.Concat([]string{s.Checkout}, s.ReadOnlyDirs, s.NoTouchDirs, s.Others) {
		deny = append(deny, rule("Edit", d+"/**"), rule("Write", d+"/**"))
	}
	deny = append(deny, AgentFiles...)
	return unique(allow), unique(deny)
}

func unique(list []string) []string {
	seen := map[string]bool{}
	out := list[:0]
	for _, x := range list {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
