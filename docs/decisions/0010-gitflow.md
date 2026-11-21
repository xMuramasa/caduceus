---
id: 0010
date: 2026-11-12
status: accepted
supersedes: 0007
issue: xMuramasa/caduceus#1
decided-by: Martin
---

# 0010. Gitflow: main stable, develop for work, Martin confirms every release

## Context

With up to three agents in parallel and a night run, Hermes merging straight to main (no pull requests, Martin
checking afterwards) leaves no stable branch and no point where Martin says "this ships". Martin asked for basic
gitflow: main as the stable branch, develop for unstable changes, task-type branch prefixes, commits that say the
same more precisely, rebases to fix diverged branches in priority order, merges always task to develop to main
through pull requests, confirmation before anything reaches main, and a worktree for every task.

Already in place before this: a worktree per delegate run (on `issue-<N>` branches), and rebasing or cherry-picking
onto a moved main with the gates re-run. New: develop, typed branch names and commits, pull requests, priority-ordered
merges, and Martin's confirmation for main.

## Options

- G1, where a cross-repo feature's tasks come together: A. each repo's develop, server side first; B. a feature
  branch between the tasks and develop.
- G2, what Martin runs day to day: A. develop builds, main as the rollback point; B. main builds plus separate
  develop installs.
- G3, when main moves: A. a release per repo (or initiative) that Martin confirms, listing what's in it; B. a
  confirmation per task.
- G4, repos outside the flow: A. hermetica and the vault commit straight to main; B. everything through the flow.
- G5, when it starts: A. now, by hand, until Caduceus automates it; B. with Caduceus' core (P2).

## Decision

A for all five (Martin, 2026-11-12), with tagged releases ("we can configure tagged releases if it helps").

- `main` stable; `develop` the integration branch and GitHub's default branch.
- Task branches `<type>/<N>-<slug>` from develop, each in its own worktree `~/Projects/<repo>-wt/issue-<N>`; types
  feat, fix, chore, docs, refactor, test. `hotfix/<N>-<slug>` from main for urgent fixes.
- Commits `<type>(<scope>): <what> (#N)` with `Fixes #N` in the body.
- A PR per task into develop; Hermes reviews (as a PR comment), rebases onto develop, re-runs the gates and
  fast-forwards develop. Ready PRs merge in priority order; the rest rebase after each merge. Only task branches are
  rebased.
- Releases: a PR develop to main per repo; on Martin's confirmation main fast-forwards, gets an annotated tag
  `vYYYY.MM.DD` (`.2` for a second that day) and a GitHub release with the notes.
- Amended 2026-11-21 after the first releases, which Martin merged with GitHub's button: a release is a merge commit
  on main (the button, or `gh pr merge --merge`), and develop then fast-forwards to main. Releases are proposed
  weekly (W2). Interim guards: caduceus#11.

## Consequences

- Supersedes 0007's `feat/<slug>` branches: a feature lands when Martin confirms its repos' release together, and
  `LOG.md` records the tags. Amends 0009: the night run merges into develop only.
- Issues close when their PR lands on develop (the default branch), not at release.
- GitHub's free plan can't protect branches in private repos, so the rule is enforced by Caduceus and Hermes, not by
  GitHub. GitHub can't record an approval by the same account, so Martin's confirmation is a chat message or an app
  button, recorded as a comment on the release PR.
- Fast-forwarding keeps main and develop on identical commits; GitHub's "rebase and merge" button would rewrite them,
  so releases don't use it. After a hotfix, main is merged into develop so the next release fast-forwards again.
- Done the same day: develop created and made default in the ten active repos; checkouts on optiplex and the Mac
  switched to it; hermes-dispatch names branches the new way.

## Links

- SPEC.md section 9
- Martin's rules: hermetica `rules/git.md`
