# Copyright 2026 Pocket Grimoire Guild contributors
# SPDX-License-Identifier: Apache-2.0

"""Hermetic guards for the v0.3.0 preview release helper."""

from __future__ import annotations

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


PROJECT_ROOT = Path(__file__).resolve().parents[2]
RELEASE_SCRIPT = PROJECT_ROOT / "scripts" / "release-preview.sh"
RELEASE_NOTES = PROJECT_ROOT / "docs" / "releases" / "v0.3.0.md"
RELEASE_WORKFLOW = PROJECT_ROOT / ".github" / "workflows" / "release.yml"
EXPECTED_REPOSITORY = "pocket-grimoire-guild/otelcol-exporter-servicenow-event-management"
EXPECTED_TAG = "v0.3.0"
RELEASE_SHA = "a" * 40
OTHER_SHA = "b" * 40


GIT_MOCK = r'''#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path

args = sys.argv[1:]
calls_path = Path(os.environ["MOCK_CALLS"])
with calls_path.open("a", encoding="utf-8") as calls:
    calls.write(json.dumps({"tool": "git", "args": args}) + "\n")

def fail(message, code=1):
    print(message, file=sys.stderr)
    raise SystemExit(code)

if args == ["config", "--get", "remote.origin.url"]:
    print(os.environ.get("MOCK_ORIGIN_URL", "https://github.com/" + os.environ["GH_REPO"] + ".git"))
elif args == ["rev-parse", "--verify", "HEAD^{commit}"]:
    print(os.environ.get("MOCK_HEAD_SHA", os.environ["RELEASE_SHA"]))
elif args == ["rev-parse", "--verify", "refs/tags/" + os.environ.get("MOCK_EXPECTED_TAG", "v0.3.0") + "^{commit}"]:
    print(os.environ.get("MOCK_LOCAL_TAG_SHA", os.environ["RELEASE_SHA"]))
elif args == ["status", "--porcelain"]:
    if os.environ.get("MOCK_DIRTY_TREE") == "1":
        print(" M README.md")
elif args and args[0] == "ls-remote":
    tag = os.environ.get("MOCK_EXPECTED_TAG", "v0.3.0")
    expected_args = ["ls-remote", "--tags", "origin", "refs/tags/" + tag, "refs/tags/" + tag + "^{}"]
    if args != expected_args:
        fail("unexpected git ls-remote arguments: " + repr(args), 97)
    exit_code = int(os.environ.get("MOCK_LS_REMOTE_EXIT", "0"))
    if exit_code:
        fail("mock git ls-remote failure", exit_code)
    count_path = Path(os.environ["MOCK_LS_REMOTE_COUNT"])
    count = int(count_path.read_text(encoding="utf-8") or "0") if count_path.exists() else 0
    count += 1
    count_path.write_text(str(count), encoding="utf-8")
    if os.environ.get("MOCK_REMOTE_TAG_MISSING") == "1":
        raise SystemExit(0)
    remote_sha = os.environ.get("MOCK_REMOTE_TAG_SHA", os.environ["RELEASE_SHA"])
    if count > 1:
        remote_sha = os.environ.get("MOCK_REMOTE_TAG_SHA_SECOND", remote_sha)
    if os.environ.get("MOCK_ANNOTATED_TAG") == "1":
        print(os.environ.get("MOCK_REMOTE_TAG_OBJECT_SHA", "c" * 40) + "\trefs/tags/" + tag)
        print(remote_sha + "\trefs/tags/" + tag + "^{}")
    else:
        print(remote_sha + "\trefs/tags/" + tag)
elif args and args[0] == "fetch":
    expected_args = ["fetch", "--no-tags", "origin", "+refs/heads/main:refs/remotes/origin/main"]
    if args != expected_args:
        fail("unexpected git fetch arguments: " + repr(args), 97)
    exit_code = int(os.environ.get("MOCK_FETCH_EXIT", "0"))
    if exit_code:
        fail("mock git fetch failure", exit_code)
elif len(args) == 4 and args[0] == "merge-base" and args[1] == "--is-ancestor":
    if args[2] != os.environ["RELEASE_SHA"] or args[3] != "refs/remotes/origin/main":
        fail("unexpected ancestry arguments", 97)
    raise SystemExit(int(os.environ.get("MOCK_ANCESTRY_EXIT", "0")))
else:
    fail("unexpected git command: " + repr(args), 97)
'''


GH_MOCK = r'''#!/usr/bin/env python3
import json
import os
import sys
from pathlib import Path

args = sys.argv[1:]
calls_path = Path(os.environ["MOCK_CALLS"])
event = {"tool": "gh", "args": args}
if args == ["api", "--paginate", "--slurp", "/repos/" + os.environ["GH_REPO"] + "/releases?per_page=100"]:
    exit_code = int(os.environ.get("MOCK_GH_API_EXIT", "0"))
    if exit_code:
        event["exit"] = exit_code
        with calls_path.open("a", encoding="utf-8") as calls:
            calls.write(json.dumps(event) + "\n")
        print("mock gh api failure", file=sys.stderr)
        raise SystemExit(exit_code)
    with calls_path.open("a", encoding="utf-8") as calls:
        calls.write(json.dumps(event) + "\n")
    print(os.environ.get("MOCK_RELEASE_LIST", "[[]]"))
elif len(args) >= 2 and args[:2] == ["release", "create"]:
    event["notes"] = ""
    if "--notes-file" in args:
        event["notes"] = Path(args[args.index("--notes-file") + 1]).read_text(encoding="utf-8")
    with calls_path.open("a", encoding="utf-8") as calls:
        calls.write(json.dumps(event) + "\n")
    exit_code = int(os.environ.get("MOCK_GH_CREATE_EXIT", "0"))
    if exit_code:
        print("mock gh release create failure", file=sys.stderr)
        raise SystemExit(exit_code)
    print("mock release created")
else:
    print("unexpected gh command: " + repr(args), file=sys.stderr)
    raise SystemExit(97)
'''


class ReleasePreviewGuardsTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="release-preview-test-")
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        (self.repo / "scripts").mkdir()
        (self.repo / "docs" / "releases").mkdir(parents=True)
        shutil.copyfile(RELEASE_SCRIPT, self.repo / "scripts" / "release-preview.sh")
        shutil.copyfile(RELEASE_NOTES, self.repo / "docs" / "releases" / "v0.3.0.md")
        self.bin = self.repo / "mock-bin"
        self.bin.mkdir()
        self._write_mock("git", GIT_MOCK)
        self._write_mock("gh", GH_MOCK)
        self.calls_path = self.repo / "mock-calls.jsonl"

    def _write_mock(self, name: str, contents: str) -> None:
        path = self.bin / name
        path.write_text(contents, encoding="utf-8")
        path.chmod(0o755)

    def _env(self, **overrides: str) -> dict[str, str]:
        env = os.environ.copy()
        env.update(
            {
                "PATH": str(self.bin) + os.pathsep + env["PATH"],
                "GH_TOKEN": "mock-token",
                "GH_REPO": EXPECTED_REPOSITORY,
                "RELEASE_TAG": EXPECTED_TAG,
                "RELEASE_SHA": RELEASE_SHA,
                "MOCK_CALLS": str(self.calls_path),
                "MOCK_LS_REMOTE_COUNT": str(self.repo / "ls-remote-count"),
            }
        )
        env.update(overrides)
        return env

    def _run(self, **overrides: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(self.repo / "scripts" / "release-preview.sh")],
            cwd=self.repo,
            env=self._env(**overrides),
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

    def _calls(self) -> list[dict[str, object]]:
        if not self.calls_path.exists():
            return []
        return [json.loads(line) for line in self.calls_path.read_text(encoding="utf-8").splitlines()]

    def _gh_calls(self) -> list[dict[str, object]]:
        return [event for event in self._calls() if event["tool"] == "gh"]

    def _release(self, *, sha: str = RELEASE_SHA, draft: bool = False, prerelease: bool = True) -> list[list[dict[str, object]]]:
        return [[
            {
                "tag_name": EXPECTED_TAG,
                "draft": draft,
                "prerelease": prerelease,
                "body": f"release notes\n\nSource commit: {sha}\n",
            }
        ]]

    def test_creates_prerelease_with_notes_and_full_source_sha(self) -> None:
        result = self._run(MOCK_ANNOTATED_TAG="1")

        self.assertEqual(result.returncode, 0, result.stderr)
        gh_calls = self._gh_calls()
        self.assertEqual([event["args"][0] for event in gh_calls], ["api", "release"])
        create = gh_calls[1]
        args = create["args"]
        self.assertEqual(args[:3], ["release", "create", EXPECTED_TAG])
        self.assertEqual(args[args.index("--repo") + 1], EXPECTED_REPOSITORY)
        self.assertIn("--verify-tag", args)
        self.assertIn("--prerelease", args)
        self.assertIn("--latest=false", args)
        self.assertIn("--title", args)
        self.assertEqual(args[args.index("--title") + 1], "v0.3.0 — development/public preview")
        expected_notes = (self.repo / "docs" / "releases" / "v0.3.0.md").read_text(encoding="utf-8")
        self.assertEqual(create["notes"], expected_notes + "\nSource commit: " + RELEASE_SHA + "\n")

    def test_matching_published_preview_is_idempotent(self) -> None:
        result = self._run(MOCK_RELEASE_LIST=json.dumps(self._release()))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("already exists", result.stdout)
        self.assertEqual([event["args"][0] for event in self._gh_calls()], ["api"])

    def test_rejects_wrong_requested_tag_before_git_or_github(self) -> None:
        result = self._run(RELEASE_TAG="v0.2.0")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RELEASE_TAG must be v0.3.0", result.stderr)
        self.assertEqual(self._calls(), [])

    def test_rejects_mismatched_local_and_remote_shas(self) -> None:
        scenarios = (
            ({"MOCK_HEAD_SHA": OTHER_SHA}, "checked-out commit does not match RELEASE_SHA"),
            ({"MOCK_LOCAL_TAG_SHA": OTHER_SHA}, "local tag does not peel to RELEASE_SHA"),
            ({"MOCK_REMOTE_TAG_SHA": OTHER_SHA}, "public remote tag does not peel to RELEASE_SHA"),
        )
        for overrides, message in scenarios:
            with self.subTest(message=message):
                self.calls_path.unlink(missing_ok=True)
                (self.repo / "ls-remote-count").unlink(missing_ok=True)
                result = self._run(**overrides)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertFalse(any(event.get("args", [None])[0] == "release" for event in self._gh_calls()))

    def test_rejects_remote_tag_change_before_release_creation(self) -> None:
        result = self._run(MOCK_REMOTE_TAG_SHA_SECOND=OTHER_SHA)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("public remote tag does not peel to RELEASE_SHA", result.stderr)
        self.assertEqual([event["args"][0] for event in self._gh_calls()], ["api"])

    def test_rejects_tag_not_merged_to_origin_main(self) -> None:
        result = self._run(MOCK_ANCESTRY_EXIT="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("tagged commit is not merged into origin/main", result.stderr)
        self.assertEqual(self._gh_calls(), [])

    def test_rejects_dirty_working_tree(self) -> None:
        result = self._run(MOCK_DIRTY_TREE="1")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("working tree must be clean", result.stderr)
        self.assertEqual(self._gh_calls(), [])

    def test_rejects_missing_empty_or_marked_release_notes(self) -> None:
        notes = self.repo / "docs" / "releases" / "v0.3.0.md"
        scenarios = (
            (None, "maintained notes file docs/releases/v0.3.0.md is missing or empty"),
            ("   \n\t\n", "maintained notes contain no non-whitespace content"),
            ("Release\n\nSource commit: " + RELEASE_SHA + "\n", "maintained notes must not contain a Source commit marker"),
        )
        original = notes.read_text(encoding="utf-8")
        try:
            for contents, message in scenarios:
                with self.subTest(message=message):
                    self.calls_path.unlink(missing_ok=True)
                    (self.repo / "ls-remote-count").unlink(missing_ok=True)
                    if contents is None:
                        notes.unlink(missing_ok=True)
                    else:
                        notes.write_text(contents, encoding="utf-8")
                    result = self._run()
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(message, result.stderr)
                    self.assertEqual(self._gh_calls(), [])
        finally:
            notes.write_text(original, encoding="utf-8")

    def test_refuses_mismatched_draft_full_and_duplicate_releases(self) -> None:
        scenarios = (
            (self._release(sha=OTHER_SHA), "not the matching published preview"),
            (self._release(draft=True), "not the matching published preview"),
            (self._release(prerelease=False), "not the matching published preview"),
            (self._release() + self._release(), "multiple releases exist for this tag"),
        )
        for release_list, message in scenarios:
            with self.subTest(message=message):
                self.calls_path.unlink(missing_ok=True)
                (self.repo / "ls-remote-count").unlink(missing_ok=True)
                result = self._run(MOCK_RELEASE_LIST=json.dumps(release_list))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertEqual([event["args"][0] for event in self._gh_calls()], ["api"])

    def test_stops_when_release_api_listing_fails_or_has_wrong_shape(self) -> None:
        scenarios = (
            ({"MOCK_GH_API_EXIT": "17"}, "could not list releases; refusing to publish"),
            ({"MOCK_RELEASE_LIST": "{}"}, "unexpected shape; refusing to publish"),
        )
        for overrides, message in scenarios:
            with self.subTest(message=message):
                self.calls_path.unlink(missing_ok=True)
                (self.repo / "ls-remote-count").unlink(missing_ok=True)
                result = self._run(**overrides)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertFalse(any(event.get("args", [None])[0] == "release" for event in self._gh_calls()))

    def test_propagates_release_creation_api_failure(self) -> None:
        result = self._run(MOCK_GH_CREATE_EXIT="23")

        self.assertEqual(result.returncode, 23)
        self.assertEqual([event["args"][0] for event in self._gh_calls()], ["api", "release"])

    def test_workflow_uses_reusable_ci_and_only_the_v030_tag(self) -> None:
        workflow = RELEASE_WORKFLOW.read_text(encoding="utf-8")

        self.assertIn("tags: [v0.3.0]", workflow)
        self.assertNotIn("v0.2.0", workflow)
        self.assertIn("uses: ./.github/workflows/ci.yml", workflow)
        self.assertIn("permissions:\n      contents: write", workflow)
        self.assertIn("persist-credentials: false", workflow)
        self.assertIn("RELEASE_TAG: ${{ github.ref_name }}", workflow)
        self.assertIn("run: bash scripts/release-preview.sh", workflow)


if __name__ == "__main__":
    unittest.main()
