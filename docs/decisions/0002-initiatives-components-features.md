---
id: 0002
date: 2026-11-11
status: accepted
supersedes: null
issue: null
decided-by: Martin
---

# 0002. Initiatives, components, features, tasks, runs

## Context

v0 of the draft said one repo is one project. Martin: "the relation 1 repo 1 project does not cover enough ground ...
we are defining something akin to a meta harness which allows us to work on multiple projects - features - repos at
the same time. the git tracking of a single tool can be so its contained its own code but part of a greater set".

## Options

- A. One repo per project, with an optional product grouping (v0).
- B. Components (repos) kept self-contained, initiatives spanning them, and work as features, tasks and runs (v1).

## Decision

B. A **component** is one repo with its own history, settings, tests and releases, and it doesn't know which
initiatives use it. An **initiative** is a lasting goal across components and owns the cross-repo picture. A
**feature** is a slice of an initiative that may touch several components. A **task** is one issue in one repo, sized
for one run. A **run** is one execution. A single-repo project is an initiative with one component.

## Consequences

- Initiative membership is declared once, in the initiative's `INITIATIVE.md`.
- GitHub sub-issues and blocked-by already work across repos, so features need no new tracking mechanism.

## Links

- Interview in chat with Hermes, 2026-11-11; draft in Martin's vault, `Plans/control-plane-draft.md` (v0, v1).
- SPEC.md section 4
