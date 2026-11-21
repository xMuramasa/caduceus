# Caduceus: SPEC v1 (approved by Martin, 2026-11-21)

Caduceus is the engine of Martin's development workflow. It decides what a project is, where work lives, and how an
issue becomes merged, tested and reviewed code, across several repos at once. Hermes runs it. Trismegistus (the Mac
and Android apps, through hermes-relay) is its dashboard and remote. Martin's own content (initiatives, rules, feature
specs) lives in the workspace repo, [hermetica](https://github.com/xMuramasa/hermetica); this repo stays generic.

How to read this: decisions already taken are ADRs in [docs/decisions](docs/decisions) (one line each in section 2).
Section 3 holds facts checked on the box. Open questions come last. Martin approved this file on 2026-11-21; changes to it go through ADRs.

## 1. What it replaces

| Today | Becomes |
| --- | --- |
| hermes-dispatch (Python, 876 lines + 25 tests): one Claude Code run per issue | `caduceus dispatch` (Go); its tests are the port's spec; the repo is archived after a side-by-side check |
| The delegate-run half of the agents helper in hermes-relay (list, transcript, stop) | The Caduceus daemon, which starts the runs and so owns them |
| About 1,700 lines of workflow rules in Hermes skills on optiplex, not versioned | `rules/`, `patterns/` and `templates/` here, plus Martin's layer in hermetica |
| Cross-repo work held together only by tracking issues | Initiatives, features and feature workspaces |
| The planned night run (hermes-dispatch#4, #5) | `caduceus night` + two Hermes cron jobs, same agreed rules |

## 2. Decisions

| ADR | Decision |
| --- | --- |
| [0001](docs/decisions/0001-name-and-fresh-start.md) | Name Caduceus; new private repo, started fresh; ideas credited to swarm-forge, no code taken |
| [0002](docs/decisions/0002-initiatives-components-features.md) | Model: components (repos), initiatives (mega projects), features, tasks, runs |
| [0003](docs/decisions/0003-go.md) | Written in Go |
| [0004](docs/decisions/0004-settings-in-each-component.md) | Settings live in each component repo (`.caduceus/project.toml`), protected from runs |
| [0005](docs/decisions/0005-decision-records.md) | Decisions asked in an issue, recorded as ADR files plus an index line |
| [0006](docs/decisions/0006-swarm.md) | A `swarm` label; squads in any language; a squad takes a whole feature |
| [0007](docs/decisions/0007-feature-workspaces.md) | Feature workspaces on `feat/<slug>` branches (superseded by 0010) |
| [0008](docs/decisions/0008-workspace-repo.md) | Initiatives live in one private workspace repo, hermetica |
| [0009](docs/decisions/0009-night-run.md) | The night run's rules (agreed for hermes-dispatch), moved to Caduceus (merge target amended by 0010) |
| [0010](docs/decisions/0010-gitflow.md) | Gitflow: main stable, develop for work, typed task branches in worktrees, PRs, Martin confirms every release |

## 3. Facts checked (2026-11-11, optiplex)

- Go 1.27.3 on the host, `CGO_ENABLED=0`, no C compiler: race tests run in `golang:1.27.3-bookworm` (image present).
  The module cache has no TOML or YAML library; Hermes adds `github.com/BurntSushi/toml` and `gopkg.in/yaml.v3`
  before the first delegate, because delegates run with `GOPROXY=off`.
- gh 2.102.0 (scopes include `project`), Claude Code 2.1.311, systemd 259 (`systemd-run --user` transient services,
  which hermes-dispatch already uses per run).
- herdr 0.9.5: agent panes (`agent list|get|read|prompt|wait|start`), worktree workspaces, and a socket API with a
  bundled schema (`herdr api schema`, `herdr api snapshot`).
- Hermes cron: scripts must be regular files in `~/.hermes/scripts` (`.py` under Hermes' interpreter, `.sh` allowed).
  A last stdout line `{"wakeAgent": false}` skips the agent for that tick.
- horus-eye `GET /api/summary` gives session and weekly usage, and answers `n/a` when its upstream rate-limits.
- Work board (`PVT_kwHOAkoXDc4Bo1_m`): Status Todo, Ready, In progress, Needs check, Done; Priority P0-P2. Sub-issues
  and blocked-by work across repos (hermes-dispatch#1 has sub-issues in five repos).
- The Go host helpers that run today use 4.5 MB (term) and 13.5 MB (agents) of memory.
- GitHub's free plan answers 403 "Upgrade to GitHub Pro" for branch protection and rulesets on private repos, and an
  account can't approve its own pull request.
- 2026-11-12: `develop` created from main and made the default branch (rebase and merge-commit merges on, head
  branches deleted on merge) in hermes-relay, trismegistus-mac, trismegistus-android, homelab, news-reader, mage-sys,
  tablet-kiosk, hermes-dispatch, caduceus and endfield-dotfiles; the checkouts on optiplex and the Mac track it.
- The relay repo is `xMuramasa/hermes-relay` (renamed from `trismegistus` on 2026-11-11). Its Go module paths still
  read `github.com/xMuramasa/trismegistus/...`, which is harmless for local modules.

## 4. The model

| Thing | What it is | Identified by |
| --- | --- | --- |
| Workspace | Everything Martin builds; the hermetica repo | — |
| Initiative | A lasting goal across components (the mega project) | slug, e.g. `hermopolis` |
| Component | One git repo, self-contained: history, settings, tests, releases | `owner/repo` |
| Feature | A slice of value in one initiative; may touch several components | `<initiative>/<slug>` + its tracking issue |
| Task | One issue in one repo, sized for one run | `owner/repo#N` |
| Run | One execution of a task (or of a squad role) | `<repo>-issue-<N>`, `<initiative>-<slug>-<role>` |

- A component doesn't know which initiatives use it. Membership is declared once, in the initiative's
  `INITIATIVE.md`.
- A single-repo project is an initiative with one component, so nothing is a special case.
- Status of initiatives and components: idea, spec, active, maintenance, archived. Repos in no initiative (dotfiles,
  old experiments) are outside Caduceus.

## 5. Where things live

### This repo (the engine)

```text
README.md  SPEC.md
cmd/caduceus/          the one binary: CLI and daemon (`caduceus serve`)
internal/              packages (settings, workspace, github, quota, runs, dispatch, features, night, squad, api)
rules/                 default rules: engineering, workflow, handoffs, review, decisions, documentation, design
patterns/              per component kind: macos-app, android-app, web-app, service, homelab, cli, library
packs/                 executor definitions: solo, squad
templates/             SPEC, README, ADR, INITIATIVE, feature, issue bodies (task, phase, decision, tracking, design)
docs/decisions/        Caduceus' own ADRs
testdata/
```

### The workspace repo (hermetica)

```text
README.md
rules/                       Martin's layer on top of rules/ here (style, git, review, design, homelab, budget)
initiatives/<slug>/
  INITIATIVE.md              front matter (name, status, components, penpot) + vision, architecture, contracts, roadmap
  decisions/NNNN-slug.md     cross-repo ADRs
  features/<slug>.md         behavior, contract changes, split per component, design link, acceptance
  LOG.md                     features landed: date, feature, commit per repo, deployed or not
```

`INITIATIVE.md` front matter (YAML, so Obsidian shows it as properties):

```yaml
name: Hermopolis
slug: hermopolis
status: active
components:
  - repo: xMuramasa/hermes-relay
    role: server        # owns protocol/schema.json
  - repo: xMuramasa/trismegistus-mac
    role: client
penpot: { file: <penpot file id> }
```

### A component repo

```text
README.md  SPEC.md (this component only)
.caduceus/project.toml       settings (section 6)
docs/decisions/  docs/plans/  docs/probes/  docs/runbooks/
design/                      DESIGN.md, tokens.json, <draft>/ PNGs + README with Penpot links
scripts/                     build, test and guard scripts: runs use them, never edit them
```

### Elsewhere

| What | Where |
| --- | --- |
| Main checkouts | `~/Projects/<repo>` on optiplex; Mac builds over `ssh mac` |
| Task worktrees | `~/Projects/<repo>-wt/issue-<N>`, one per task (Hermes' own edits too), on `<type>/<N>-<slug>` |
| Run state | `~/.local/state/caduceus/runs/` (flat; same file set as hermes-dispatch; 30 days) |
| Daemon socket | `~/.local/state/caduceus/caduceus.sock` (0660, like the term helper) |
| Work items | GitHub issues + the Work board |
| Designs | Penpot: one file per initiative, one page per feature draft |
| Notes, reports | Obsidian vault: generated initiative and component pages; night report in the daily note |
| Secrets | `.env` / `.credentials` on optiplex only |

## 6. Component settings: `.caduceus/project.toml`

Grown from hermes-dispatch's profiles. Caduceus reads it from the component's default branch in the main checkout,
never from a run's worktree. `.caduceus/**` is a protected path in every run, and Hermes' review flags any change to
it.

```toml
kind = "service"                 # macos-app | android-app | web-app | service | homelab | cli | library | config
language = "go"                  # informational; the tools below are what count
model = "opus"                   # default model for solo runs; the issue's `agent:` label wins
effort = "high"
base_branch = "develop"          # task branches start here; "main" only for repos outside gitflow
hosts = ["optiplex"]             # "mac": skipped at night when `ssh mac` doesn't answer
locks = []                       # shared resources a run holds: "mac-build", "gradle"
night = true                     # may the night run take this component's issues

[env]                            # set on the run's command line
GOPROXY = "off"
GOFLAGS = "-mod=readonly"

[gates]                          # by meaning; each a list of commands the run itself executes
test = ["go -C relay test ./..."]
build = ["go -C relay build ./..."]
lint = ["go -C relay vet ./..."]
format = ["gofmt -l relay"]
coverage = []
complexity = []
mutation = []
e2e = []                         # the public surface QA drives (squads)

[review]                         # gates only Hermes runs (containers, other hosts)
gates = ["docker run ... golang:1.27.3-bookworm go test -race ./..."]

[access]
allow = ["Bash(go -C relay test*)"]      # tools beyond the base set
deny = ["Bash(go get*)", "Bash(go mod*)"]
protected = ["scripts/*.sh", "compose*.yml"]
read_only = ["hermes-relay"]             # other components by name; resolved to their checkouts
no_touch = []
secret = []

rules = ["Never log tokens, pairing codes or message text."]
network = "No network. Standard library only unless the issue says otherwise."
```

The rules define what each gate means. A project's language choice includes choosing its tool for each gate,
checked by one probe run at setup (`caduceus component init`).

## 7. How work gets done

```text
intake -> triage -> (design) -> ready -> dispatch -> execute -> review + merge into develop -> check -> done
                                                                   ... done work on develop -> release (Martin confirms) -> main
```

- **Intake**: chat, the app, phone capture, Hermes' audits, night-report follow-ups. Everything becomes an issue: a
  feature's tracking issue in hermetica, or a task in a component.
- **Triage** (Hermes): initiative, feature, repo, priority, blocked-by, labels (`hermes` by its rule,
  `needs-decision`, `needs-design`, `swarm`, `agent: opus|sonnet`), and Owns (paths from `git ls-files`).
- **Design**: UI work gets a Penpot proposal; Martin's pick becomes "Agreed design" in the issue.
- **Ready**: Martin's go (Ready status, the app's Dispatch Tonight, "go" in chat) or the `hermes` label.
- **Dispatch**: `caduceus dispatch check` (open, blockers closed, not running, settings present, budget, slots, locks,
  Owns overlap), then start: worktree, brief, rules, launch, an issue comment "Started HH:MM", Status In progress.
- **Review + merge**: always Hermes, through a pull request into develop (section 9). The diff must stay inside Owns;
  Hermes re-runs the gates and review gates, runs a secrets scan, reads the risky code, commits as
  `<type>(<scope>): <what> (#N)` with `Fixes #N`, opens the PR with its review as a comment, rebases, fast-forwards
  develop and checks the remote.
- **Check**: UI changes go to Needs check with what to look at (and a Penpot-vs-app pair once snapshots exist), on
  the develop build Martin runs; everything else goes to Done.
- **Release**: done work on develop reaches main only when Martin confirms a release (section 9).

### Executors

| Executor | When | How |
| --- | --- | --- |
| `hermes` | A few files whose tests Hermes knows | Hermes edits directly; same review |
| `solo` | The default for a task | One `claude -p` run in a transient user service `caduceus-<run>`; brief + rules file; allow/deny from settings |
| `squad` | Issues or features labelled `swarm` | Roles in herdr panes, section 10 |

The solo launch keeps hermes-dispatch's verified shape: `--append-system-prompt-file <rules>`, `--add-dir` for the
run folder and read-only components, `--permission-mode acceptEdits`, allow and deny lists, and stream-json to
`<run>.stream.jsonl`. Runs never commit, push, deploy, install or touch credentials.

## 8. Rules and briefs

- Three layers, later ones adding to earlier ones: `rules/` here (defaults), then hermetica `rules/` (Martin's), then
  the component (`patterns/<kind>.md` plus the `rules` lines in its settings).
- A run's brief is the issue body + Hermes' "Notes from Hermes" comment + the feature spec when there is one. Its rules
  file is the three layers + the run's paths, Owns, gates and report paths.
- Caduceus doesn't write `AGENTS.md` or `CLAUDE.md` into component repos: Hermes guards those files behind an approval
  prompt, and the rules reach every run at launch anyway (squad panes included).
- Tests check behavior, never the wording of rules or prompts.

## 9. Branches, pull requests and releases (ADR 0010)

- **main** is stable and moves only when Martin confirms a release. **develop** is where work lands, and GitHub's
  default branch, so pull requests target it and `Fixes #N` closes the issue when the PR lands there. Martin runs
  develop builds (the relay, the apps); main is the confirmed snapshot to roll back to.
- **Every task gets its own worktree** `~/Projects/<repo>-wt/issue-<N>`, branched from develop as
  `<type>/<N>-<slug>` (`feat/28-ntfy-push`). Types: feat, fix, chore, docs, refactor, test; taken from the issue's
  labels (`bug` is fix, `documentation` is docs, `chore`, `refactor`, `test`), feat otherwise. Hermes' own small edits
  get a worktree too.
- **Commits** say the same, more precisely: `<type>(<scope>): <what> (#N)`, `Fixes #N` in the body.
- **Into develop**: one PR per task. Hermes reviews it (GitHub doesn't let the account approve its own PR, so the
  review is a PR comment with the gates and findings), rebases the branch onto develop, re-runs the gates and
  fast-forwards develop, so what was tested is exactly what lands. PRs ready together merge in priority order (P0,
  P1, P2, then oldest); after each merge the others rebase and re-run their gates. Only task branches are ever
  rebased, never develop or main.
- **Releases** (weekly, Martin's W2, 2026-11-21): every Sunday, or when Martin asks, Hermes opens a release PR
  (develop to main) per repo with unreleased work, listing what's in it. Martin confirms by merging it on GitHub
  ("Create a merge commit"; squash and rebase merges are disabled) or by telling Hermes, who merges it the same way,
  contract owners first. The settle step then fast-forwards develop to main, adds the annotated tag `vYYYY.MM.DD`
  (`.2` for a second that day) and publishes a GitHub release with the notes. Deploys and installs from main stay
  Martin's steps.
- **Hotfixes**: `hotfix/<N>-<slug>` from main, a PR into main through the same confirmation; settle then merges main
  into develop.
- **Cross-repo features**: their tasks are ordinary tasks merging into each repo's develop in dependency order (the
  contract owner first), so develop keeps working mid-feature.
  1. `caduceus feature new <initiative>/<slug>` creates the tracking issue in hermetica, `features/<slug>.md` from
     the template, and the sub-issues per component (server before client, `blocked by` edges, the binding contract
     pasted into both ends when they run in parallel).
  2. `caduceus feature workspace <initiative>/<slug>` lists the feature's task worktrees side by side; each run writes
     only its own and reads its siblings.
  3. The feature is ready when its tasks are on develop and its cross-repo acceptance passes there (from
     `features/<slug>.md`: e.g. the relay container, the apps' contract tests and one end-to-end check through the
     public surface).
  4. It lands when Martin confirms the release of its repos together; `LOG.md` records the tags that shipped
     together. Features should land within about a week; a longer one gets split.
- **Outside this flow**: hermetica and the Obsidian vault commit straight to main (decision records there are written
  after Martin has answered).
- **Enforcement**: GitHub's free plan can't protect branches in private repos, so Caduceus holds the line. Until P2/P3
  this is interim tooling (caduceus#11): `hooks/pre-push`, linked into every gitflow checkout, refuses pushes to main
  without `CADUCEUS_RELEASE=<release PR>`, non-fast-forward develop or main, untyped branch names and
  non-conventional subjects; `scripts/gitflow.py` audits drift (`audit`), settles releases hourly (`settle`) and lists
  candidates for the Sunday proposal (`candidates`). `caduceus pr` and `caduceus release` replace the script.

## 10. Squads (label `swarm`)

- A squad takes a whole feature across its components (ADR 0006): roles for code, architecture and QA, each an
  interactive Claude pane in a herdr workspace named after the feature. The architect sees the boundaries between
  repos; QA drives the public surfaces (the relay's WebSocket and HTTP, the apps' UI where it can).
- Handoffs are committed work plus a message file in the run folder. The daemon delivers them through herdr
  (`agent prompt`, `agent wait`) and records them on the feature issue. This takes the idea of durable handoffs from
  swarm-forge, written fresh.
- Gates by meaning (section 6). The architect runs complexity and mutation where a component names a tool; QA runs
  `e2e`.
- Open (round 4): who specifies (Hermes or a squad role), whether Martin approves the squad's acceptance spec, and how
  a squad counts against the slots.

## 11. Concurrency and budget

- Slots: 3 agents at once (a squad counts as its active roles). Locks from settings (`mac-build`, `gradle`, one Gradle
  build at a time). One writer per worktree. Two active tasks or features whose Owns overlap in one repo run one after
  the other.
- Budget from horus-eye before every launch: the 5-hour window and weekly limits are configurable (daytime defaults as
  in hermes-dispatch). An `n/a` reading is retried 3 times over about 10 minutes, then nothing launches (fail closed).

## 12. The night run (ADR 0009)

- Two Hermes cron jobs: **03:30 Night dispatch** (pre-run script `caduceus night select`; when nothing is eligible the
  agent never wakes) and **06:30 Night merge and report**.
- Eligible: open, on the board, label `hermes` or Status Ready, none of `needs-decision`, `needs-design`,
  `needs-phone-check`, `on-hold`, `phase`; blockers closed; settings with `night = true`; not running. P0, P1, P2,
  then oldest.
- Budget: weekly under 80 %, 5-hour window under 30 %, at most 3 runs, re-read before each launch.
- One immediate retry for failures other than the usage limit. A limit cut-off is reported with "relaunch?" (the
  03:00 window lasts until about 08:00). Runs still going at 06:30 are reported and merged when Martin next chats.
- Everything merges into develop only; main never moves at night. Homelab: prepare only, never deployed. UI changes
  go to Needs check. Mac work is skipped when `ssh mac` fails.
- Up to 2 `needs-design` issues a night get 2-3 Penpot alternatives on a new page in the initiative's Penpot file,
  through the headless Penpot session (homelab#7).
- The report goes to Telegram at 06:30 (and into the Jobs view): merged, Needs check, skipped and why, retried, still
  running, usage before and after, issues newly labelled `hermes`, morning steps.

## 13. Decisions and documentation

- ADR files `NNNN-slug.md` with YAML front matter (`id`, `date`, `status`: accepted or superseded, `supersedes`,
  `issue`, `decided-by`), then Context, Options (lettered, one-line trade-off each), Decision, Consequences, Links.
- Homes: component ADRs in the component, cross-repo ones in the initiative, harness ones here. Each gets one line in
  the matching SPEC.md or INITIATIVE.md index.
- Asking: a `needs-decision` issue (or a comment on the issue it changes) with evidence, lettered options and a
  recommendation. Answering: a letter, in chat or in the app's Decisions inbox. Hermes then writes the ADR, the index
  line and the Hindsight entry, and closes the issue with a link.
- Style for every document: Markdown, short plain sentences, ISO dates, `owner/repo#N` for cross-repo references.

## 14. CLI

```text
caduceus component init|check <repo>            settings scaffold + probe; validate
caduceus initiative list|show <slug>
caduceus feature new|show|workspace <initiative>/<slug>
caduceus pr open|merge <owner/repo#N>          task PR into develop; merge after gates, in queue order
caduceus release propose|confirm [<repo>...|<initiative>]   release PRs; fast-forward main + tag on confirm
caduceus dispatch check|start <owner/repo#N> [--model] [--effort] [--notes FILE] [--dry-run] [--force-quota]
caduceus run list|status|stop|prune [<run>]
caduceus night select|report
caduceus squad start|status|stop <initiative>/<slug>
caduceus serve                                    the daemon (section 15)
```

A `caduceus` symlink in `~/.local/bin`. Hermes' cron reaches it through small `.sh` wrappers in `~/.hermes/scripts`.

## 15. Daemon and Trismegistus

- `caduceus serve` is a user service listening on the socket in section 5. hermes-relay proxies it the way it proxies
  the agents helper today, with the relay's roles: the Mac writes, the phone reads.
- It takes over delegate runs (list, transcript, stop) from the agents helper. The helper keeps what serves chat: Hermes'
  own background processes, media and rewind.
- Routes (v1): `GET /initiatives`, `GET /initiatives/{slug}` (components, features, LOG), `GET /features/{id}`
  (status per component, workspace, runs), `GET /runs`, `GET /runs/{id}/transcript?after=N`,
  `POST /runs/{id}/stop`, `POST /dispatch` (now or tonight), `GET /decisions` (open `needs-decision` issues with their
  options), `POST /decisions/{issue}` (a letter + note: a comment Hermes acts on), `GET /night` (tonight's queue, last
  report), `POST /night/pause`, `GET /squads/{feature}`.
- The app's home becomes Initiatives, then Features, then Tasks and runs; plus Decisions, Night, Designs and Squads.
  The screens get drawn in Penpot first (a Hermopolis feature).

## 16. Security

- Runs never see credentials. Settings, scripts and guards are protected paths. `secret` globs are denied for reading.
- The daemon socket is 0660 in a persistent directory (not `/run/user`), mounted into the relay read-only like the
  agents socket.
- GitHub writes come from Hermes (gh) or the daemon (gh, Martin's token), never from runs.
- Nothing pushes to main without Martin's confirmation on the release PR; at night main never moves.
- Code, rules and templates here stay generic: no hosts, tokens or personal paths in them (those go in hermetica
  and in component settings). This SPEC describes the setup Caduceus is built for, so it names them.

## 17. Phases

| Phase | Work | Done when |
| --- | --- | --- |
| P0 | This SPEC, the ADRs, the two repos, the Hermopolis initiative | Martin's go on SPEC.md |
| P1 | Rules: move the law out of the Hermes skills into `rules/`, `patterns/`, `templates/` and hermetica `rules/`; skills shrink to pointers | A run's rules file is built from these files only, and a real run with it passes review |
| P2 | Core: settings, runs folder, `dispatch` and `run` commands ported from hermes-dispatch (its tests ported), typed task branches, `pr open/merge` and the merge queue, `component init/check`; settings files in every active component | `caduceus dispatch` replaces `dispatch` for a week of real runs; hermes-dispatch archived |
| P3 | Initiatives, features and releases: hermetica reader, `feature new/workspace`, `release propose/confirm` with tags, board fields Initiative and Feature, `LOG.md` | One real cross-repo feature released through `release confirm` |
| P4 | Night run (section 12) | One real night merged an eligible issue and the report reached Telegram and the Jobs view |
| P5 | Squads (section 10, after round 4) | One `swarm` feature delivered by a squad |
| P6 | Daemon + relay routes + app screens (a Hermopolis feature) | Martin dispatches, answers a decision and reads the night report from the Mac app |
| P7 | ADR backfill for active components; generated Obsidian pages | Every active component has its ADR index |

## 18. Out of scope

Merging without Hermes' review; build hosts other than optiplex and the Mac; a public release; Hermes kanban (GitHub
issues stay the backlog); automatic account failover.

## 19. Open questions

1. Squads (round 4): the specifier (Hermes or a role), approval of a squad's acceptance spec, slot accounting.
2. The app's navigation: drawn in Penpot as alternatives first.
3. A short alias for the CLI.
