"""The OpenAPI break check: which tag is the baseline, which changes fail, what waives them."""

import importlib.util
import os
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("openapi_diff", ROOT / "scripts/openapi_diff.py")
openapi_diff = importlib.util.module_from_spec(spec)
spec.loader.exec_module(openapi_diff)


class BaselineTests(unittest.TestCase):
    def test_latest_stable_tag_wins_numerically(self):
        tags = ["v0.9.0", "v0.10.0", "v0.8.16", "v1.0.0-rc.1", "nightly"]
        self.assertEqual(openapi_diff.latest_tag(tags), "v0.10.0")

    def test_no_stable_tag_means_no_baseline(self):
        self.assertIsNone(openapi_diff.latest_tag(["v1.0.0-rc.1", "other"]))


class ChangeTests(unittest.TestCase):
    def test_only_error_level_changes_fail(self):
        changes = [
            {"id": "response-required-property-removed", "level": 3},
            {"id": "response-optional-property-removed", "level": 2},
            {"id": "new-optional-request-property", "level": 1},
        ]
        self.assertEqual([c["id"] for c in openapi_diff.errors(changes)], ["response-required-property-removed"])

    def test_ignored_rule_ids_are_filtered(self):
        original = dict(openapi_diff.IGNORED_RULES)
        openapi_diff.IGNORED_RULES["noisy-rule"] = "test only"
        try:
            self.assertEqual(openapi_diff.errors([{"id": "noisy-rule", "level": 3}]), [])
        finally:
            openapi_diff.IGNORED_RULES.clear()
            openapi_diff.IGNORED_RULES.update(original)

    def test_describe_names_rule_operation_and_text(self):
        text = openapi_diff.describe({"id": "rule", "operation": "GET", "path": "/api/x", "text": "removed"})
        self.assertEqual(text, "rule (GET /api/x): removed")


class WaiverTests(unittest.TestCase):
    def test_footer_waives(self):
        self.assertTrue(openapi_diff.has_breaking_footer(["feat: x\n\nBREAKING CHANGE: drops the field\n"]))
        self.assertTrue(openapi_diff.has_breaking_footer(["fix: x", "feat: y\n\nBREAKING-CHANGE: drops it"]))

    def test_mentions_and_empty_footers_do_not_waive(self):
        self.assertFalse(openapi_diff.has_breaking_footer(["feat: x\n\nThis is not a BREAKING CHANGE: really"]))
        self.assertFalse(openapi_diff.has_breaking_footer(["feat: x\n\nBREAKING CHANGE:\n"]))
        self.assertFalse(openapi_diff.has_breaking_footer(["feat!: x"]))
        self.assertFalse(openapi_diff.has_breaking_footer([]))

    def test_commit_messages_come_from_the_range(self):
        with tempfile.TemporaryDirectory(prefix="mnema-openapi-diff-") as directory:
            def git(*args):
                subprocess.run(["git", "-C", directory, "-c", "user.name=t", "-c", "user.email=t@example.invalid", *args],
                               check=True, capture_output=True)
            git("init", "-q")
            git("commit", "-q", "--allow-empty", "-m", "chore: base")
            git("commit", "-q", "--allow-empty", "-m", "feat: x", "-m", "BREAKING CHANGE: drops the field")
            previous = pathlib.Path.cwd()
            try:
                os.chdir(directory)
                messages = openapi_diff.commit_messages("HEAD~1..HEAD")
            finally:
                os.chdir(previous)
        self.assertEqual(len(messages), 1)
        self.assertTrue(openapi_diff.has_breaking_footer(messages))


if __name__ == "__main__":
    unittest.main()
