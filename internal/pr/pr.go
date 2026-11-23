// Package pr is `caduceus pr open|merge` (SPEC section 9, ADR 0010): a task branch's pull request into develop, and
// the merge queue that rebases each branch onto origin/develop, re-runs its gates and fast-forwards develop.
package pr

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xMuramasa/caduceus/internal/config"
	"github.com/xMuramasa/caduceus/internal/dispatch"
	"github.com/xMuramasa/caduceus/internal/ext"
	"github.com/xMuramasa/caduceus/internal/settings"
	"github.com/xMuramasa/caduceus/internal/worktree"
)

// Develop is the only branch a PR merges into.
const Develop = "develop"

var (
	// The same forms hooks/pre-push enforces; hotfix branches go into main, so they are not pr's business.
	branchRE   = regexp.MustCompile(`^(feat|fix|chore|docs|refactor|test)/([0-9]+)-[a-z0-9]+(-[a-z0-9]+)*$`)
	subjectRE  = regexp.MustCompile(`^(feat|fix|chore|docs|refactor|test|hotfix|revert)(\([a-z0-9._/-]+\))?!?: .+ \(#[0-9]+(, #[0-9]+)*\)$`)
	priorities = map[string]int{"P0": 0, "P1": 1, "P2": 2}
)

// How long merge waits for GitHub to mark a PR merged after develop moved.
const (
	pollEvery = 2 * time.Second
	pollTimes = 15
)

// PR runs the commands.
type PR struct {
	X     ext.Ext
	Cfg   *config.Config
	W     io.Writer
	Sleep func(time.Duration) // nil: time.Sleep
}

// task is a ref's worktree, checked to be clean and on a task branch of the issue.
type task struct {
	ref        dispatch.Ref
	st         *settings.Settings
	wt, branch string
	pr         int
	createdAt  string
	priority   string
}

func (p *PR) git(dir string, args ...string) (string, error) {
	out, err := p.X.Run(append([]string{"git", "-C", dir}, args...))
	return strings.TrimSpace(out), err
}

func (p *PR) gh(args ...string) (string, error) { return p.X.Run(append([]string{"gh"}, args...)) }

func (p *PR) load(ref dispatch.Ref) (*task, error) {
	st, err := settings.Load(p.X, p.Cfg, ref.Name)
	if err != nil {
		return nil, err
	}
	if st.BaseBranch != Develop {
		return nil, fmt.Errorf("%s has base_branch %q: pr works only for repos whose task branches start from %s",
			ref.Name, st.BaseBranch, Develop)
	}
	t := &task{ref: ref, st: st, wt: worktree.Path(p.Cfg.Worktrees(ref.Name), ref.N)}
	if fi, err := os.Stat(t.wt); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("no worktree at %s", t.wt)
	}
	if t.branch, err = p.git(t.wt, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		return nil, err
	}
	if m := branchRE.FindStringSubmatch(t.branch); m == nil || m[2] != strconv.Itoa(ref.N) {
		return nil, fmt.Errorf("%s is on %s, not a task branch <type>/%d-<slug>; refusing", t.wt, t.branch, ref.N)
	}
	st2, err := p.git(t.wt, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	if st2 != "" {
		return nil, fmt.Errorf("%s has uncommitted changes", t.wt)
	}
	if _, err := p.git(t.wt, "fetch", "--quiet", "origin"); err != nil {
		return nil, err
	}
	return t, nil
}

// push is the only write to origin: the task's own branch, or develop as a fast-forward of origin/develop.
func (p *PR) push(t *task, dest string) error {
	switch {
	case dest == Develop:
		if _, err := p.git(t.wt, "merge-base", "--is-ancestor", "origin/"+Develop, "HEAD"); err != nil {
			return fmt.Errorf("%s is not a fast-forward of origin/%s; refusing to push %s", t.branch, Develop, Develop)
		}
		_, err := p.git(t.wt, "push", "--quiet", "origin", "HEAD:refs/heads/"+Develop)
		return err
	case dest == t.branch && branchRE.MatchString(dest):
		// The branch was rebased, so its PR head has to follow for GitHub to see the PR land.
		_, err := p.git(t.wt, "push", "--quiet", "--force-with-lease", "origin", "HEAD:refs/heads/"+dest)
		return err
	}
	return fmt.Errorf("refusing to push to %s", dest)
}

// withFile writes text to a temporary file for a --body-file, so it never reaches a command line.
func withFile(text string, fn func(name string) error) error {
	f, err := os.CreateTemp("", "caduceus-pr-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return fn(f.Name())
}

// Open pushes the task branch, opens its PR into develop (title the commit subject, body with Fixes #N) and posts
// the review from reviewFile as a PR comment.
func (p *PR) Open(ref dispatch.Ref, reviewFile string) error {
	review, err := os.ReadFile(reviewFile)
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}
	if strings.TrimSpace(string(review)) == "" {
		return fmt.Errorf("review: %s is empty", reviewFile)
	}
	t, err := p.load(ref)
	if err != nil {
		return err
	}
	subjects, err := p.git(t.wt, "log", "--format=%s", "origin/"+Develop+"..HEAD")
	if err != nil {
		return err
	}
	if subjects == "" {
		return fmt.Errorf("%s has no commits beyond origin/%s", t.branch, Develop)
	}
	for _, s := range strings.Split(subjects, "\n") {
		if !subjectRE.MatchString(s) {
			return fmt.Errorf("commit subject %q should be <type>(<scope>): <what> (#N)", s)
		}
	}
	title, _, _ := strings.Cut(subjects, "\n")
	body, err := p.git(t.wt, "log", "-1", "--format=%b", "HEAD")
	if err != nil {
		return err
	}
	if fixes := fmt.Sprintf("Fixes #%d", ref.N); !strings.Contains(body, fixes) {
		body = strings.TrimSpace(body + "\n\n" + fixes)
	}
	if err := p.push(t, t.branch); err != nil {
		return err
	}
	fmt.Fprintf(p.W, "%s: pushed %s\n", ref, t.branch)
	var url string
	err = withFile(body+"\n", func(name string) error {
		out, err := p.gh("pr", "create", "-R", ref.Repo, "--base", Develop, "--head", t.branch, "--title", title,
			"--body-file", name)
		url = lastLine(out)
		return err
	})
	if err != nil {
		return err
	}
	if _, err := strconv.Atoi(path.Base(url)); err != nil {
		return fmt.Errorf("gh pr create: no PR url in %q", url)
	}
	fmt.Fprintf(p.W, "%s: opened %s \"%s\"\n", ref, url, title)
	if _, err := p.gh("pr", "comment", path.Base(url), "-R", ref.Repo, "--body-file", reviewFile); err != nil {
		return fmt.Errorf("review comment: %w", err)
	}
	fmt.Fprintf(p.W, "%s: posted the review from %s\n", ref, reviewFile)
	return nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

type prView struct {
	Number      int       `json:"number"`
	State       string    `json:"state"`
	BaseRefName string    `json:"baseRefName"`
	CreatedAt   string    `json:"createdAt"`
	Comments    []comment `json:"comments"`
}

type comment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// ready loads a ref for merging: its worktree, an open PR into develop with a review comment by login, and its
// priority.
func (p *PR) ready(ref dispatch.Ref, login string) (*task, error) {
	t, err := p.load(ref)
	if err != nil {
		return nil, err
	}
	out, err := p.gh("pr", "view", t.branch, "-R", ref.Repo, "--json", "number,state,baseRefName,createdAt,comments")
	if err != nil {
		return nil, err
	}
	var v prView
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, fmt.Errorf("gh pr view %s: %w", t.branch, err)
	}
	switch {
	case v.State != "OPEN":
		return nil, fmt.Errorf("PR #%d is %s", v.Number, strings.ToLower(v.State))
	case v.BaseRefName != Develop:
		return nil, fmt.Errorf("PR #%d is into %s, not %s; refusing", v.Number, v.BaseRefName, Develop)
	case !slices.ContainsFunc(v.Comments, func(c comment) bool { return c.Author.Login == login }):
		return nil, fmt.Errorf("PR #%d has no review comment by %s (pr open --review FILE)", v.Number, login)
	}
	t.pr, t.createdAt = v.Number, v.CreatedAt
	if t.priority, err = p.priority(ref); err != nil {
		return nil, err
	}
	return t, nil
}

// priority is the issue's Priority on the board, else a P0-P2 label, else "".
func (p *PR) priority(ref dispatch.Ref) (string, error) {
	owner, name, _ := strings.Cut(ref.Repo, "/")
	q := "query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){" +
		"labels(first:50){nodes{name}} projectItems(first:10){nodes{project{id} " +
		"priority:fieldValueByName(name:\"Priority\"){... on ProjectV2ItemFieldSingleSelectValue{name}}}}}}}"
	out, err := p.gh("api", "graphql", "-f", "query="+q, "-f", "o="+owner, "-f", "r="+name, "-F", fmt.Sprintf("n=%d", ref.N))
	if err != nil {
		return "", err
	}
	type named struct {
		Name string `json:"name"`
	}
	var d struct {
		Data struct {
			Repository struct {
				Issue struct {
					Labels struct {
						Nodes []named `json:"nodes"`
					} `json:"labels"`
					ProjectItems struct {
						Nodes []struct {
							Project struct {
								ID string `json:"id"`
							} `json:"project"`
							Priority *named `json:"priority"`
						} `json:"nodes"`
					} `json:"projectItems"`
				} `json:"issue"`
			} `json:"repository"`
		} `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return "", fmt.Errorf("%s priority: %w", ref, err)
	}
	if len(d.Errors) > 0 && string(d.Errors) != "null" {
		return "", fmt.Errorf("%s priority: %s", ref, string(d.Errors)[:min(len(d.Errors), 300)])
	}
	iss := d.Data.Repository.Issue
	for _, it := range iss.ProjectItems.Nodes {
		if it.Project.ID == p.Cfg.Board.ID && it.Priority != nil {
			return it.Priority.Name, nil
		}
	}
	for _, l := range iss.Labels.Nodes {
		if _, ok := priorities[l.Name]; ok {
			return l.Name, nil
		}
	}
	return "", nil
}

func rank(priority string) int {
	if r, ok := priorities[priority]; ok {
		return r
	}
	return len(priorities)
}

// Merge merges the refs' PRs one at a time in priority order (P0, P1, P2, none; then the oldest PR). Each one is
// rebased onto the develop the previous merge left and its gates re-run just before develop moves, so what was
// tested is what lands. A PR that isn't ready, conflicts or fails a gate is reported and skipped; the error says
// how many.
func (p *PR) Merge(refs []dispatch.Ref) error {
	out, err := p.gh("api", "user")
	if err != nil {
		return err
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(out), &user); err != nil || user.Login == "" {
		return fmt.Errorf("gh api user: no login in %q", strings.TrimSpace(out))
	}
	failed := 0
	var queue []*task
	for _, ref := range refs {
		t, err := p.ready(ref, user.Login)
		if err != nil {
			fmt.Fprintf(p.W, "%s: FAIL %s\n", ref, oneLine(err))
			failed++
			continue
		}
		queue = append(queue, t)
	}
	slices.SortStableFunc(queue, func(a, b *task) int {
		if c := rank(a.priority) - rank(b.priority); c != 0 {
			return c
		}
		return strings.Compare(a.createdAt, b.createdAt)
	})
	if len(queue) > 1 {
		names := make([]string, len(queue))
		for i, t := range queue {
			names[i] = fmt.Sprintf("%s (%s, PR #%d)", t.ref, or(t.priority, "no priority"), t.pr)
		}
		fmt.Fprintf(p.W, "queue: %s\n", strings.Join(names, ", "))
	}
	for _, t := range queue {
		if err := p.mergeOne(t); err != nil {
			fmt.Fprintf(p.W, "%s: FAIL %s\n", t.ref, oneLine(err))
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d PR(s) not merged", failed, len(refs))
	}
	return nil
}

func (p *PR) mergeOne(t *task) error {
	if _, err := p.git(t.wt, "fetch", "--quiet", "origin"); err != nil {
		return err
	}
	if _, err := p.git(t.wt, "rebase", "--quiet", "origin/"+Develop); err != nil {
		files, _ := p.git(t.wt, "diff", "--name-only", "--diff-filter=U")
		if _, aerr := p.git(t.wt, "rebase", "--abort"); aerr != nil {
			return fmt.Errorf("rebase onto origin/%s failed and could not be aborted: %w", Develop, aerr)
		}
		if files == "" {
			return fmt.Errorf("rebase onto origin/%s failed (aborted): %w", Develop, err)
		}
		return fmt.Errorf("rebase onto origin/%s conflicts in %s (aborted; %s is unchanged)", Develop,
			strings.ReplaceAll(files, "\n", ", "), t.branch)
	}
	ahead, err := p.git(t.wt, "rev-list", "--count", "origin/"+Develop+"..HEAD")
	if err != nil {
		return err
	}
	if ahead == "0" {
		return fmt.Errorf("%s has no commits beyond origin/%s", t.branch, Develop)
	}
	head, err := p.git(t.wt, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	fmt.Fprintf(p.W, "%s: rebased %s onto origin/%s (%s, %s commit(s))\n", t.ref, t.branch, Develop, head, ahead)
	n, err := p.gates(t)
	if err != nil {
		return err
	}
	fmt.Fprintf(p.W, "%s: %d gate(s) passed\n", t.ref, n)
	if err := p.push(t, t.branch); err != nil {
		return err
	}
	if err := p.push(t, Develop); err != nil {
		return err
	}
	fmt.Fprintf(p.W, "%s: develop fast-forwarded to %s\n", t.ref, head)
	if err := p.waitMerged(t); err != nil {
		return err
	}
	if _, err := p.git(t.st.Checkout, "worktree", "remove", t.wt); err != nil {
		return fmt.Errorf("PR #%d merged, but removing the worktree failed: %w", t.pr, err)
	}
	if _, err := p.git(t.st.Checkout, "branch", "-D", t.branch); err != nil {
		return fmt.Errorf("PR #%d merged, but deleting the branch failed: %w", t.pr, err)
	}
	fmt.Fprintf(p.W, "%s: PR #%d merged; removed %s and %s\n", t.ref, t.pr, t.wt, t.branch)
	return nil
}

// gates runs the settings' gates by meaning, then the review gates, in the worktree with the settings' env. A
// format gate fails when it lists files.
func (p *PR) gates(t *task) (int, error) {
	env := []string{"env"}
	for _, k := range slices.Sorted(maps.Keys(t.st.Env)) {
		env = append(env, k+"="+t.st.Env[k])
	}
	type gate struct{ meaning, cmd string }
	var all []gate
	for _, m := range settings.Meanings {
		for _, c := range t.st.Gates[m] {
			all = append(all, gate{m, c})
		}
	}
	for _, c := range t.st.Review {
		all = append(all, gate{"review", c})
	}
	for _, g := range all {
		out, err := p.X.Run(append(slices.Clone(env), "sh", "-c", `cd "$1" && eval "$2"`, "gate", t.wt, g.cmd))
		if err != nil {
			return 0, fmt.Errorf("%s gate `%s` failed: %w", g.meaning, g.cmd, err)
		}
		if g.meaning == "format" && strings.TrimSpace(out) != "" {
			return 0, fmt.Errorf("format gate `%s` lists %s", g.cmd, strings.Join(strings.Fields(out), ", "))
		}
	}
	return len(all), nil
}

// waitMerged polls until GitHub marks the PR merged, which it does a few seconds after develop moves.
func (p *PR) waitMerged(t *task) error {
	sleep := p.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for i := range pollTimes {
		out, err := p.gh("pr", "view", strconv.Itoa(t.pr), "-R", t.ref.Repo, "--json", "state")
		var v struct {
			State string `json:"state"`
		}
		if err == nil && json.Unmarshal([]byte(out), &v) == nil && v.State == "MERGED" {
			return nil
		}
		if i < pollTimes-1 {
			sleep(pollEvery)
		}
	}
	return fmt.Errorf("develop moved, but GitHub hasn't marked PR #%d merged after %s; check it, then remove %s",
		t.pr, pollEvery*pollTimes, t.wt)
}

func oneLine(err error) string { return strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ") }

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
