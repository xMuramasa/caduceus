#!/usr/bin/env python3
"""Interim gitflow tooling for Martin's repos (caduceus#11, ADR 0010), until Caduceus P2/P3 replace it.

  gitflow.py audit       [--repos a,b] [--days 7]   findings, one line each; exit 1 when there are any
  gitflow.py settle      [--repos a,b]              after a release merge on main: fast-forward develop, tag, release
  gitflow.py candidates  [--repos a,b] [--json]     repos with unreleased commits on develop

Repos are the git checkouts in $CADUCEUS_PROJECTS (default ~/Projects) whose origin has a develop branch.
GitHub calls go through $CADUCEUS_GH (default `gh`). Nothing here edits files in a checkout.
"""
import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

TYPES = "feat|fix|chore|docs|refactor|test|hotfix"
BRANCH_RE = re.compile(rf"^({TYPES})/[0-9]+-[a-z0-9]+(-[a-z0-9]+)*$")
SUBJECT_RE = re.compile(rf"^({TYPES}|revert)(\([a-z0-9._/-]+\))?!?: .+ \(#[0-9]+(, #[0-9]+)*\)$")
PR_MERGE_RE = re.compile(r"^Merge pull request #(\d+) from [^/\s]+/(\S+)$")
HOOK_MARK = "Caduceus gitflow guard"


class GitError(RuntimeError):
    pass


def run(args: list[str], cwd: Path | None = None, check: bool = True, env: dict | None = None) -> str:
    p = subprocess.run(args, cwd=cwd, capture_output=True, text=True, env=env)
    if check and p.returncode:
        raise GitError(f"{' '.join(args[:4])}: {(p.stderr or p.stdout).strip()[:300]}")
    return p.stdout.strip()


def gh(*args: str) -> str:
    return run([os.environ.get("CADUCEUS_GH", "gh"), *args])


class Repo:
    def __init__(self, path: Path):
        self.path = path
        self.name = path.name
        url = self.git("remote", "get-url", "origin", check=False)
        m = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(\.git)?$", url)
        self.slug = f"{m[1]}/{m[2]}" if m else f"local/{self.name}"

    def git(self, *args: str, check: bool = True) -> str:
        return run(["git", "-C", str(self.path), *args], check=check)

    def has(self, ref: str) -> bool:
        return bool(self.git("rev-parse", "-q", "--verify", ref, check=False))

    def ancestor(self, a: str, b: str) -> bool:
        return subprocess.run(["git", "-C", str(self.path), "merge-base", "--is-ancestor", a, b]).returncode == 0

    def log(self, rng: list[str], merges: bool | None = None) -> list[dict]:
        """Commits as {sha, parents, date, subject}; merges=True only merges, False none, None both."""
        flags = {True: ["--merges"], False: ["--no-merges"], None: []}[merges]
        out = self.git("log", *flags, "--format=%H%x09%P%x09%cs%x09%s", *rng)
        rows = []
        for line in filter(None, out.splitlines()):
            sha, parents, date, subject = line.split("\t", 3)
            rows.append({"sha": sha, "parents": parents.split(), "date": date, "subject": subject})
        return rows


def repos(names: str | None) -> list[Repo]:
    root = Path(os.environ.get("CADUCEUS_PROJECTS", "~/Projects")).expanduser()
    wanted = set(names.split(",")) if names else None
    out = []
    for d in sorted(root.iterdir()):
        if not (d / ".git").is_dir() or (wanted and d.name not in wanted):
            continue
        r = Repo(d)
        r.git("fetch", "-q", "--prune", "--tags", "origin", check=False)
        if r.has("refs/remotes/origin/develop") and r.has("refs/remotes/origin/main"):
            out.append(r)
    return out


def is_release_merge(c: dict) -> bool:
    m = PR_MERGE_RE.match(c["subject"])
    return bool(m) and (m[2] == "develop" or m[2].startswith("hotfix/"))


# ---------------------------------------------------------------- audit

def audit(r: Repo, days: int) -> list[str]:
    f = []
    behind = r.log(["origin/develop..origin/main"])
    if behind:
        if r.ancestor("origin/develop", "origin/main") and all(
                len(c["parents"]) > 1 and is_release_merge(c) for c in behind):
            f.append("develop is behind main's release merge (settle fixes it)")
        else:
            subj = "; ".join(c["subject"] for c in behind[:3])
            f.append(f"main has {len(behind)} commit(s) not on develop ({subj}): merge main into develop")
    since = f"--since={days}.days"
    plain: list[dict] = []
    for c in r.log([since, "--first-parent", "origin/develop"]):
        c["parents"] = r.git("log", "-1", "--format=%P", c["sha"]).split()  # --first-parent hides the others
        if len(c["parents"]) < 2:
            plain.append(c)
            continue
        m = PR_MERGE_RE.match(c["subject"])
        if m and not is_release_merge(c) and m[2] != "main":
            f.append(f"PR #{m[1]} ({m[2]}) went into develop through GitHub's merge button, not a fast-forward "
                     f"({c['sha'][:7]}, {c['date']})")
        plain += r.log([since, f"{c['parents'][0]}..{c['parents'][1]}"], merges=False)  # what the merge brought
    seen = set()
    for c in plain:
        if c["sha"] not in seen and not SUBJECT_RE.match(c["subject"]):
            f.append(f"develop commit {c['sha'][:7]} \"{c['subject']}\" isn't <type>(<scope>): <what> (#N)")
        seen.add(c["sha"])
    for ref in r.git("for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/").split():
        b = ref.removeprefix("origin/")
        if b in ("HEAD", "main", "develop", "origin"):
            continue
        if not BRANCH_RE.match(b):
            f.append(f"branch {b} on GitHub isn't <type>/<N>-<slug>")
        elif r.ancestor(ref, "origin/develop"):
            f.append(f"branch {b} is merged into develop but still on GitHub")
    wts = r.git("worktree", "list", "--porcelain").split("\n\n")
    for block in wts[1:]:
        fields = dict(line.split(" ", 1) for line in block.splitlines() if " " in line)
        path, br = fields.get("worktree", "?"), fields.get("branch", "").removeprefix("refs/heads/")
        if br and not BRANCH_RE.match(br):
            f.append(f"worktree {path} is on {br}, not <type>/<N>-<slug>")
        elif br and r.ancestor(br, "origin/develop") and not run(["git", "-C", path, "status", "--porcelain"],
                                                                  check=False):
            f.append(f"worktree {path} ({br}) is merged and clean: remove it")
    hook = r.path / ".git" / "hooks" / "pre-push"
    if not hook.is_file() or HOOK_MARK not in hook.read_text(errors="replace"):
        f.append("pre-push hook missing (scripts/install-hooks.sh)")
    if (cur := r.git("branch", "--show-current", check=False)) != "develop":
        f.append(f"main checkout is on {cur or 'a detached HEAD'}, not develop")
    if os.environ.get("CADUCEUS_SKIP_GITHUB") != "1" and not r.slug.startswith("local/"):
        try:
            default = json.loads(gh("repo", "view", r.slug, "--json", "defaultBranchRef"))["defaultBranchRef"]["name"]
            if default != "develop":
                f.append(f"GitHub default branch is {default}, not develop")
        except (GitError, KeyError, ValueError) as e:
            print(f"{r.name}: default branch unknown: {e}", file=sys.stderr)
    return f


# ---------------------------------------------------------------- settle

def tag_name(r: Repo, day: str) -> str:
    base = "v" + day.replace("-", ".")
    existing = set(r.git("tag", "--list", f"{base}*").split())
    if base not in existing:
        return base
    n = 2
    while f"{base}.{n}" in existing:
        n += 1
    return f"{base}.{n}"


def settle(r: Repo) -> list[str]:
    done = []
    if not r.ancestor("origin/main", "origin/develop"):
        if r.ancestor("origin/develop", "origin/main"):
            r.git("push", "-q", "origin", "refs/remotes/origin/main:refs/heads/develop")
            r.git("fetch", "-q", "origin")
            done.append(f"develop fast-forwarded to main ({r.git('rev-parse', '--short', 'origin/main')})")
        else:  # a hotfix landed on main: merge main into develop in a throwaway worktree
            with tempfile.TemporaryDirectory(prefix="gitflow-settle-") as tmp:
                wt = Path(tmp) / "wt"
                r.git("worktree", "add", "-q", "--detach", str(wt), "origin/develop")
                try:
                    m = subprocess.run(["git", "-C", str(wt), "merge", "--no-edit", "-q", "-m",
                                        "Merge branch 'main' into develop", "origin/main"], capture_output=True, text=True)
                    if m.returncode:
                        run(["git", "-C", str(wt), "merge", "--abort"], check=False)
                        raise GitError("merging main into develop conflicts: do it by hand")
                    run(["git", "-C", str(wt), "push", "-q", "origin", "HEAD:develop"])
                finally:
                    r.git("worktree", "remove", "--force", str(wt), check=False)
            r.git("fetch", "-q", "origin")
            done.append("main merged into develop (hotfix)")
        if r.git("branch", "--show-current", check=False) == "develop" and not r.git("status", "--porcelain"):
            r.git("merge", "-q", "--ff-only", "origin/develop", check=False)
    tip = r.log(["-1", "origin/main"])[0]
    tags_here = r.git("tag", "--points-at", "origin/main", "--list", "v*").split()
    prev = r.git("describe", "--tags", "--abbrev=0", "--match", "v*", "origin/main", check=False)
    if not tags_here and (is_release_merge(tip) or prev):
        day = dt.datetime.fromisoformat(r.git("log", "-1", "--format=%cI", "origin/main")).astimezone().date().isoformat()
        t = tag_name(r, day)
        rng = [f"{prev}..origin/main"] if prev else ["origin/main^1..origin/main"]
        notes = "\n".join(f"- {c['subject']}" for c in r.log(rng, merges=False)) or "- (no changes besides merges)"
        r.git("tag", "-a", t, "origin/main", "-m", f"Release {day}")
        r.git("push", "-q", "origin", f"refs/tags/{t}")
        done.append(f"tagged {t}")
        if not r.slug.startswith("local/"):
            url = gh("release", "create", t, "-R", r.slug, "--verify-tag", "--title", t, "--notes", notes)
            done.append(f"release {url.splitlines()[-1] if url else t}")
    return done


# ---------------------------------------------------------------- candidates

def candidates(r: Repo) -> dict | None:
    commits = r.log(["origin/main..origin/develop"], merges=False)
    if not commits:
        return None
    issues = sorted({int(n) for c in commits for n in re.findall(r"#(\d+)", c["subject"])})
    pr = None
    if os.environ.get("CADUCEUS_SKIP_GITHUB") != "1" and not r.slug.startswith("local/"):
        try:
            open_prs = json.loads(gh("pr", "list", "-R", r.slug, "--base", "main", "--head", "develop",
                                     "--state", "open", "--json", "number,url"))
            pr = open_prs[0]["url"] if open_prs else None
        except (GitError, ValueError) as e:
            print(f"{r.name}: release PR unknown: {e}", file=sys.stderr)
    return {"repo": r.slug, "commits": len(commits), "oldest": min(c["date"] for c in commits),
            "subjects": [c["subject"] for c in commits], "issues": issues, "release_pr": pr}


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("command", choices=["audit", "settle", "candidates"])
    ap.add_argument("--repos", help="comma-separated checkout names (default: all gitflow repos)")
    ap.add_argument("--days", type=int, default=7)
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args(argv)
    failed = False
    results: list = []
    for r in repos(a.repos):
        try:
            if a.command == "audit":
                results += [f"{r.name}: {x}" for x in audit(r, a.days)]
            elif a.command == "settle":
                results += [f"{r.name}: {x}" for x in settle(r)]
            elif c := candidates(r):
                results.append(c)
        except GitError as e:
            failed = True
            print(f"{r.name}: {a.command} failed: {e}", file=sys.stderr)
    if a.command == "candidates" and a.json:
        print(json.dumps(results, indent=2))
    elif a.command == "candidates":
        for c in results:
            print(f"{c['repo']}: {c['commits']} unreleased commit(s) since {c['oldest']}"
                  + (f", release PR {c['release_pr']}" if c["release_pr"] else ""))
            for s in c["subjects"]:
                print(f"  - {s}")
    else:
        for line in results:
            print(line)
    if failed:
        return 2
    return 1 if a.command == "audit" and results else 0


if __name__ == "__main__":
    sys.exit(main())
