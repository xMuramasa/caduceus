---
id: 0006
date: 2026-11-11
status: accepted
supersedes: null
issue: null
decided-by: Martin
---

# 0006. A `swarm` label; squads in any language; a squad takes a whole feature

## Context

swarm-forge runs a pipeline of agents (specifier, coder, architect, QA) whose quality tools cover only some languages
(no Swift or Kotlin) and whose acceptance pipeline needs Babashka.

## Options

- Scope A: a single run stays the default; squads only on labelled work, limited to supported languages.
- Scope B: squads by default for features. Scope C: no squads.
- Size: a squad per whole feature across its repos, or per task in one repo.

## Decision

A `swarm` label marks squad work, **in any language**. Martin: "shouldnt constraint us to set of languages, just
think about the meta-rules and languages can be picked for the project each time, right tool for the right job". A
squad takes a **whole feature** across its repos.

## Consequences

- Rules define roles by responsibility and gates by meaning; each component's settings name its tool per gate.
- Still open: who specifies for a squad, approval of its acceptance spec, slot accounting.

## Links

- Interview in chat with Hermes, 2026-11-11; draft in Martin's vault, `Plans/control-plane-draft.md` (v0, v1).
- SPEC.md section 10
