---
id: 0001
date: 2026-11-11
status: accepted
supersedes: null
issue: null
decided-by: Martin
---

# 0001. Name Caduceus; a new repo, started fresh

## Context

Martin wanted to rebrand his fork of unclebob/swarm-forge (`mini-forge` pack) into the engine for this workflow.
The fork is public, and GitHub reports no license for upstream, so its code can't be republished under another name.

## Options

- A. A new private repo with both histories (swarm-forge fork, hermes-dispatch) imported.
- B. Rename the public fork: stays public and a fork.
- C. Keep both repos and add a third for rules and the project list.

## Decision

Name **Caduceus** (Hermes' staff: the tool Hermes works with). A **new private repo, started fresh**: Martin's words,
"just take the ideas with us and give the credit where its due since we were inspired". No code or prompt text from
swarm-forge is copied. hermes-dispatch is our own code and its logic and tests may be ported.

## Consequences

- The README credits swarm-forge for the ideas taken: role pipelines, durable handoffs between agents, rules kept as
  data, and its platoon (a lieutenant over squads) and project-board brainstorms.
- The public fork stays as it is until Martin decides to archive it.

## Links

- Interview in chat with Hermes, 2026-11-11; draft in Martin's vault, `Plans/control-plane-draft.md` (v0, v1).
- Upstream: https://github.com/unclebob/swarm-forge
