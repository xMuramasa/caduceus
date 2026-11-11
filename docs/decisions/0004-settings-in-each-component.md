---
id: 0004
date: 2026-11-11
status: accepted
supersedes: null
issue: null
decided-by: Martin
---

# 0004. Settings live in each component repo

## Context

hermes-dispatch keeps one profile per repo in its own `profiles/`. The draft recommended a central registry so that
agents can't loosen their own gates.

## Options

- A. Central settings in the engine or workspace.
- B. A settings file inside each component repo.

## Decision

B (Martin). Each component has `.caduceus/project.toml`.

## Consequences

- Caduceus reads it from the default branch of the main checkout, never from a run's worktree.
- `.caduceus/**` is a protected path in every run, and Hermes' review flags any change to it, so a run can't loosen
  its own gates.

## Links

- Interview in chat with Hermes, 2026-11-11; draft in Martin's vault, `Plans/control-plane-draft.md` (v0, v1).
- SPEC.md section 6
