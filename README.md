# Caduceus

The engine of Martin's development workflow: what a project is, where work lives, and how an issue becomes merged,
tested and reviewed code across several repos at once. Hermes runs it; Trismegistus (through hermes-relay) is its
dashboard and remote. Martin's initiatives, rules and feature specs live in the workspace repo,
[hermetica](https://github.com/xMuramasa/hermetica).

Status: spec approved 2026-11-21 ([SPEC.md](SPEC.md), decisions in [docs/decisions](docs/decisions)); building
from phase 1 (issues #2-#8). Interim gitflow guards in `hooks/` and `scripts/` (#11).

## Credit

Caduceus started fresh, but its ideas come from [unclebob/swarm-forge](https://github.com/unclebob/swarm-forge):
agents working in roles (specify, code, architect, QA) in their own worktrees, durable handoffs of committed work,
rules kept as data that every agent reads, and the platoon and project-board brainstorms (a lieutenant over squads, a
dashboard of projects). No code or prompt text from it is used here.
