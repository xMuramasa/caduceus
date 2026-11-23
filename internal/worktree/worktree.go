// Package worktree is a task's git worktree: <worktree root>/issue-<N> on branch <type>/<N>-<slug>, based on the
// remote base branch (develop under gitflow, ADR 0010).
package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/xMuramasa/caduceus/internal/ext"
)

// types maps issue labels to branch types; the first label found in this order wins, feat otherwise.
var types = [][2]string{{"bug", "fix"}, {"documentation", "docs"}, {"chore", "chore"}, {"refactor", "refactor"},
	{"test", "test"}}

var filler = map[string]bool{"a": true, "an": true, "the": true, "and": true, "or": true, "of": true, "to": true,
	"for": true, "in": true, "on": true, "with": true, "from": true, "by": true, "at": true, "is": true}

// SlugMax is the longest slug.
const SlugMax = 32

var word = regexp.MustCompile(`[a-z0-9]+`)

// Type is the branch type for an issue's labels.
func Type(labels []string) string {
	for _, t := range types {
		if slices.Contains(labels, t[0]) {
			return t[1]
		}
	}
	return "feat"
}

// Slug is the title's lowercase words without filler words, joined by '-', at most SlugMax characters.
func Slug(title string) string {
	words := word.FindAllString(strings.ToLower(title), -1)
	out := ""
	for _, w := range words {
		if filler[w] {
			continue
		}
		next := w
		if out != "" {
			next = out + "-" + w
		}
		if len(next) > SlugMax {
			break
		}
		out = next
	}
	if out == "" { // only filler words, or a first word longer than the limit
		if len(words) == 0 {
			return "task"
		}
		out = words[0][:min(len(words[0]), SlugMax)]
	}
	return out
}

// Branch is <type>/<n>-<slug>.
func Branch(n int, title string, labels []string) string {
	return fmt.Sprintf("%s/%d-%s", Type(labels), n, Slug(title))
}

func isIssueBranch(name string, n int) bool {
	return regexp.MustCompile(fmt.Sprintf(`^[a-z]+/%d-[a-z0-9-]+$`, n)).MatchString(name)
}

// Path is the worktree of issue n under root.
func Path(root string, n int) string { return filepath.Join(root, fmt.Sprintf("issue-%d", n)) }

// Ensure gives (worktree, branch, what was done). New branches start from origin/<base> so a dirty main checkout
// doesn't matter. An existing worktree is reused only when it is clean and on a branch of this issue, and an
// existing branch of this issue (<type>/<n>-..., even under an older title) before creating branch.
func Ensure(x ext.Ext, checkout, root, base string, n int, branch string) (string, string, string, error) {
	git := func(dir string, args ...string) (string, error) {
		return x.Run(append([]string{"git", "-C", dir}, args...))
	}
	wt := Path(root, n)
	if fi, err := os.Stat(checkout); err != nil || !fi.IsDir() {
		return "", "", "", fmt.Errorf("no main checkout at %s", checkout)
	}
	if _, err := git(checkout, "fetch", "--quiet", "origin"); err != nil {
		return "", "", "", err
	}
	if _, err := os.Stat(wt); err == nil {
		out, err := git(wt, "rev-parse", "--show-toplevel", "--abbrev-ref", "HEAD")
		f := strings.Fields(out)
		if err != nil || len(f) != 2 {
			return "", "", "", fmt.Errorf("%s exists and is not a git worktree", wt)
		}
		top, head := f[0], f[1]
		if !samePath(top, wt) || !isIssueBranch(head, n) {
			return "", "", "", fmt.Errorf("%s exists but is not a worktree on a branch of issue %d (it is on %s)", wt, n,
				head)
		}
		st, err := git(wt, "status", "--porcelain")
		if err != nil {
			return "", "", "", err
		}
		if strings.TrimSpace(st) != "" {
			return "", "", "", fmt.Errorf("%s has uncommitted changes", wt)
		}
		return wt, head, fmt.Sprintf("reusing clean worktree %s (%s)", wt, head), nil
	}
	refs, err := git(checkout, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return "", "", "", err
	}
	var existing []string
	for _, r := range strings.Fields(refs) {
		if isIssueBranch(r, n) {
			existing = append(existing, r)
		}
	}
	slices.Sort(existing)
	if len(existing) > 1 {
		return "", "", "", fmt.Errorf("several branches for issue %d: %s; delete the stale ones", n,
			strings.Join(existing, ", "))
	}
	if len(existing) == 1 {
		if _, err := git(checkout, "worktree", "add", wt, existing[0]); err != nil {
			return "", "", "", err
		}
		return wt, existing[0], fmt.Sprintf("reusing existing branch %s in new worktree %s", existing[0], wt), nil
	}
	if _, err := git(checkout, "worktree", "add", "--no-track", "-b", branch, wt, "origin/"+base); err != nil {
		return "", "", "", err
	}
	return wt, branch, fmt.Sprintf("created %s on %s from origin/%s", wt, branch, base), nil
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
