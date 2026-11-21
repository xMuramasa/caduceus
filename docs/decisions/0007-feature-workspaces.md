---
id: 0007
date: 2026-11-11
status: superseded
superseded-by: 0010
supersedes: null
issue: null
decided-by: Martin
---

# 0007. Feature workspaces, dependency-order landing, a landing log

> Superseded by [0010](0010-gitflow.md) (2026-11-12): a feature's tasks merge into each repo's develop; no
> `feat/<slug>` branches. Dependency order and the landing log stay.

## Context

Cross-repo work today is separate tasks per repo, linked by a tracking issue and blocked-by edges.

## Options

- A. One worktree per touched repo on a shared branch, landing in dependency order, a cross-repo check, a log.
- B. Keep separate per-repo tasks.

## Decision

A.

## Consequences

- `feat/<slug>` per touched component; task worktrees branch from it; landing merges contract owners first.
- `LOG.md` in the initiative records the commit of each repo that shipped together (for rollbacks).

## Links

- Interview in chat with Hermes, 2026-11-11; draft in Martin's vault, `Plans/control-plane-draft.md` (v0, v1).
- SPEC.md section 9
