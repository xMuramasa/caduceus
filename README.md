# Caduceus

[![CI](https://github.com/xMuramasa/caduceus/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/xMuramasa/caduceus/actions/workflows/ci.yml)
![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![Claude Code](https://img.shields.io/badge/Claude_Code-headless_runs-D97757?logo=claude&logoColor=white)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

The engine of Martin's development workflow: how an issue becomes merged, tested and reviewed code, across several
repos at once. [Hermes Agent](https://hermes-agent.nousresearch.com) on a home server runs it: Caduceus checks that an
issue is ready, starts a headless Claude Code run for it in its own worktree, watches the run, and lines the result up
for Hermes' review and merge. The Trismegistus apps
([trismegistus-mac](https://github.com/xMuramasa/trismegistus-mac),
[trismegistus-android](https://github.com/xMuramasa/trismegistus-android)) show its runs live, through
[hermes-relay](https://github.com/xMuramasa/hermes-relay).

```text
intake -> triage -> ready -> dispatch -> run -> review + merge into develop -> check -> done
                                         ... done work on develop -> release (Martin confirms) -> main
```

## What's built

- **`caduceus component init|check`**: each repo carries its own settings in
  [`.caduceus/project.toml`](.caduceus/project.toml): the model, the rules a run gets, the allowed commands, the
  network policy, and the gates (test, build, lint, format, plus heavier review gates) that a change must pass.
- **`caduceus dispatch check|start`**: the eligibility report (open, blockers closed, not already running, settings
  present, usage budget, free slots, file-ownership overlap), then the start: a worktree on a `<type>/<N>-<slug>`
  branch, a brief and a rules file, and one `claude -p` run in a transient systemd user service, with stream-json
  output and allow and deny lists from the settings. Runs never commit, push, deploy or touch credentials.
- **`caduceus run list|status|stop|prune`**: one folder per run with its status, transcript and outcome; the relay's
  agents helper reads the same folders for the apps.
- **`caduceus pr open|merge`**: the task's pull request into `develop`, merged in queue order only after the gates
  pass on the rebased branch.
- **The gitflow guard** ([`hooks/pre-push`](hooks/pre-push), [`scripts/gitflow.py`](scripts/gitflow.py)): refuses
  pushes to `main` without a confirmed release, history rewrites on `develop` or `main`, and branch names or commit
  subjects outside `<type>(<scope>): <what> (#N)`.

Spec only so far ([SPEC.md](SPEC.md)): initiatives and features that span repos, releases, the night run (a batch of
ready issues while Martin sleeps, with a morning report), squads (specify, code, architect and QA roles on one
feature), and the daemon. The decisions behind it are in [docs/decisions](docs/decisions).

## How it's built

Go, the standard library plus two modules (TOML and YAML). Every external command a test would touch (`gh`,
`systemd-run`, `systemctl`, `docker`, the usage API) is replaced by a fake shaped like a real answer, and every path
lives in a temporary directory, so the tests never reach GitHub, systemd or a real repo. CI runs gofmt, vet, build,
the tests with and without the race detector, and the guard's Python tests.

The Go core was written by Claude Code runs (#16 to #18 started by its predecessor, #19 by Caduceus itself), each
reviewed and merged by Hermes; Martin sets the direction, approves the specs and confirms releases. Martin's own initiatives,
rules and feature specs live in a private workspace repo, hermetica; Caduceus stays generic.

## Credit

Caduceus started fresh, but its ideas come from [unclebob/swarm-forge](https://github.com/unclebob/swarm-forge):
agents working in roles (specify, code, architect, QA) in their own worktrees, durable handoffs of committed work,
rules kept as data that every agent reads, and the platoon and project-board brainstorms (a lieutenant over squads, a
dashboard of projects). No code or prompt text from it is used here.
