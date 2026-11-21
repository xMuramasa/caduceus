---
id: 0009
date: 2026-11-02
status: accepted
supersedes: null
issue: xMuramasa/hermes-dispatch#4
decided-by: Martin
---

# 0009. The night run's rules move to Caduceus

> Amended by [0010](0010-gitflow.md) (2026-11-12): night merges go into develop; main never moves at night.

## Context

Martin agreed the night run's rules for hermes-dispatch in two interview rounds on 2026-11-02, then decided it waits
for Caduceus (2026-11-11).

## Options

See hermes-dispatch SPEC.md, "Decisions", rounds 1 and 2.

## Decision

The rules carry over unchanged: 03:30 dispatch and 06:30 merge and report; `hermes` label or Ready; budget weekly
under 80 % and window under 30 %, at most 3 runs; one retry except usage-limit cut-offs; homelab prepare-only; UI
merges then Needs check; up to 2 design drafts a night; report to Telegram and the Jobs view.

## Consequences

- hermes-dispatch#4 and #5 are on hold until Caduceus' P4.

## Links

- https://github.com/xMuramasa/hermes-dispatch/blob/main/SPEC.md
- SPEC.md section 12
