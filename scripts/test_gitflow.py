"""Tests for hooks/pre-push and scripts/gitflow.py against throwaway repos (no network, no GitHub)."""
import datetime as dt
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

HERE = Path(__file__).resolve().parent
HOOK = HERE.parent / "hooks" / "pre-push"
sys.path.insert(0, str(HERE))
import gitflow  # noqa: E402


class Base(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp(prefix="gitflow-test-"))
        self.addCleanup(subprocess.run, ["rm", "-rf", str(self.tmp)])
        env = {"HOME": str(self.tmp), "GIT_CONFIG_NOSYSTEM": "1", "CADUCEUS_PROJECTS": str(self.tmp / "projects"),
               "CADUCEUS_SKIP_GITHUB": "1", "CADUCEUS_GH": "false",
               "GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.invalid",
               "GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@example.invalid"}
        self.env = {k: v for k, v in os.environ.items() if not k.startswith(("GIT_", "CADUCEUS_"))} | env
        patcher = mock.patch.dict(os.environ, env)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.origin = self.tmp / "proj.git"
        self.co = self.tmp / "projects" / "proj"
        self.git("init", "-q", "--bare", "-b", "main", str(self.origin))
        self.git("clone", "-q", str(self.origin), str(self.co))
        self.commit("README.md", "chore(repo): init (#1)")
        self.git("-C", self.co, "push", "-q", "origin", "HEAD:main")
        self.git("-C", self.co, "switch", "-q", "-c", "develop")
        self.git("-C", self.co, "push", "-q", "-u", "origin", "develop")
        (self.co / ".git/hooks/pre-push").symlink_to(HOOK)

    def git(self, *args: object, check: bool = True, extra: dict | None = None) -> subprocess.CompletedProcess:
        p = subprocess.run(["git", *map(str, args)], capture_output=True, text=True, env=self.env | (extra or {}))
        if check and p.returncode:
            self.fail(f"git {args}: {p.stderr}")
        return p

    def commit(self, name: str, subject: str, cwd: Path | None = None) -> None:
        cwd = cwd or self.co
        (cwd / name).write_text(subject + "\n")
        self.git("-C", cwd, "add", name)
        self.git("-C", cwd, "commit", "-q", "-m", subject)

    def push(self, *refspec: str, extra: dict | None = None) -> subprocess.CompletedProcess:
        return self.git("-C", self.co, "push", "-q", "origin", *refspec, check=False, extra=extra)

    def cli(self, *args: str) -> tuple[int, str]:
        out = StringIO()
        with redirect_stdout(out):
            code = gitflow.main(list(args))
        return code, out.getvalue()


class HookTest(Base):
    def test_main_needs_a_confirmed_release(self) -> None:
        self.commit("a", "feat(x): a (#2)")
        p = self.push("HEAD:main")
        self.assertNotEqual(0, p.returncode)
        self.assertIn("main moves only when Martin confirms a release", p.stderr)
        self.assertEqual(0, self.push("HEAD:main", extra={"CADUCEUS_RELEASE": "10"}).returncode)

    def test_branch_names_and_commit_subjects(self) -> None:
        self.git("-C", self.co, "switch", "-q", "-c", "issue-9")
        self.commit("b", "feat(x): b (#9)")
        p = self.push("issue-9")
        self.assertNotEqual(0, p.returncode)
        self.assertIn("branch 'issue-9' should be <type>/<N>-<slug>", p.stderr)
        self.assertEqual(0, self.push("HEAD:refs/heads/feat/9-thing").returncode)
        self.commit("c", "Fix the thing")
        p = self.push("HEAD:refs/heads/feat/9-thing")
        self.assertIn("\"Fix the thing\" should be <type>(<scope>): <what> (#N)", p.stderr)

    def test_develop_only_fast_forwards_and_is_never_deleted(self) -> None:
        self.commit("d", "feat(x): d (#4)")
        self.assertEqual(0, self.push("develop").returncode)
        self.git("-C", self.co, "reset", "-q", "--hard", "HEAD~1")
        self.commit("e", "feat(x): e (#5)")
        p = self.push("+develop")
        self.assertIn("develop: not a fast-forward", p.stderr)
        self.assertIn("never delete develop", self.push(":develop").stderr)

    def test_merge_commits_pass_and_repos_without_develop_are_ignored(self) -> None:
        self.git("-C", self.co, "switch", "-q", "-c", "side")
        self.commit("f", "fix(y): f (#6)")
        self.git("-C", self.co, "switch", "-q", "develop")
        self.commit("g", "feat(y): g (#7)")
        self.git("-C", self.co, "merge", "-q", "--no-edit", "side")
        self.assertEqual(0, self.push("develop").returncode)
        self.git("--git-dir", self.origin, "branch", "-D", "develop")
        self.git("-C", self.co, "fetch", "-q", "--prune", "origin")
        self.commit("h", "anything goes")
        self.assertEqual(0, self.push("HEAD:main").returncode)


class GitflowTest(Base):
    def button_merge(self, into: str, branch: str, pr: int, subject: str) -> None:
        """What GitHub's merge button leaves: a merge commit pushed past the hook."""
        self.git("-C", self.co, "switch", "-q", "-c", branch, f"origin/{into}")
        self.commit(f"{branch.replace('/', '_')}.txt", subject)
        self.git("-C", self.co, "switch", "-q", "--detach", f"origin/{into}")
        self.git("-C", self.co, "merge", "-q", "--no-ff", "-m", f"Merge pull request #{pr} from xMuramasa/{branch}",
                 branch)
        self.git("-C", self.co, "push", "-q", "--no-verify", "origin", f"HEAD:{into}")
        self.git("-C", self.co, "switch", "-q", "develop")
        self.git("-C", self.co, "merge", "-q", "--ff-only", f"origin/develop")

    def test_audit_reports_a_button_merge_and_its_commit(self) -> None:
        self.button_merge("develop", "issue-4", 5, "Build environment: newer JDK")
        code, out = self.cli("audit")
        self.assertEqual(1, code)
        self.assertIn("proj: PR #5 (issue-4) went into develop through GitHub's merge button", out)
        self.assertIn("\"Build environment: newer JDK\" isn't <type>(<scope>): <what> (#N)", out)

    def test_clean_repo_audits_clean(self) -> None:
        code, out = self.cli("audit")
        self.assertEqual((0, ""), (code, out))

    def test_settle_after_release_merge_then_hotfix(self) -> None:
        self.commit("i", "feat(z): i (#8)")
        self.assertEqual(0, self.push("develop").returncode)
        code, out = self.cli("candidates")
        self.assertIn("local/proj: 1 unreleased commit(s)", out)
        # Martin merges the release PR with the button.
        self.git("-C", self.co, "switch", "-q", "--detach", "origin/main")
        self.git("-C", self.co, "merge", "-q", "--no-ff", "-m", "Merge pull request #10 from xMuramasa/develop",
                 "origin/develop")
        self.git("-C", self.co, "push", "-q", "--no-verify", "origin", "HEAD:main")
        self.git("-C", self.co, "switch", "-q", "develop")
        code, out = self.cli("audit")
        self.assertIn("develop is behind main's release merge", out)
        code, out = self.cli("settle")
        day = dt.date.today().strftime("%Y.%m.%d")
        self.assertEqual(0, code)
        self.assertIn("proj: develop fast-forwarded to main", out)
        self.assertIn(f"proj: tagged v{day}", out)
        self.assertEqual(self.git("--git-dir", self.origin, "rev-parse", "main").stdout,
                         self.git("--git-dir", self.origin, "rev-parse", "develop").stdout)
        self.assertEqual(f"v{day}\n", self.git("--git-dir", self.origin, "tag", "--points-at", "main").stdout)
        self.assertEqual(self.git("-C", self.co, "rev-parse", "HEAD").stdout,
                         self.git("--git-dir", self.origin, "rev-parse", "develop").stdout)  # checkout pulled
        self.assertEqual((0, ""), self.cli("settle"))
        self.assertEqual((0, ""), self.cli("candidates"))
        self.commit("j", "feat(z): j (#14)")  # develop moves on before the hotfix
        self.assertEqual(0, self.push("develop").returncode)
        # A hotfix PR merged into main.
        self.button_merge("main", "hotfix/12-crash", 13, "fix(app): crash (#12)")
        code, out = self.cli("settle")
        self.assertIn("proj: main merged into develop (hotfix)", out)
        self.assertIn(f"proj: tagged v{day}.2", out)
        self.git("--git-dir", self.origin, "merge-base", "--is-ancestor", "main", "develop")
        self.assertEqual((0, ""), self.cli("settle"))


if __name__ == "__main__":
    unittest.main()
