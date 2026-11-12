#!/usr/bin/env python3
"""TestChatCoreStaysFree must not pass when it could not look.

internal/repo/boundaries_test.go skips "internal/chat/core does not exist yet"
whenever `go list -e -deps ./internal/chat/core` fails. The package exists, and
`go list -e` exits 0 for a missing package anyway, so the skip now fires only
when the go tool itself fails, and the guard then passes without checking.
The case is recorded as an expected failure: it turns red the day a failing
go tool fails the test instead of skipping it.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class RepoGuardSkip(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.binary = Path(cls.tmp.name) / 'repo.test'
        subprocess.run(['go', 'test', '-c', '-o', str(cls.binary), './internal/repo/'], cwd=ROOT,
                       check=True, capture_output=True, text=True, timeout=300)
        fake = Path(cls.tmp.name) / 'bin'
        fake.mkdir()
        (fake / 'go').write_text('#!/bin/sh\necho "go: broken toolchain" >&2\nexit 2\n')
        (fake / 'go').chmod(0o755)
        cls.fake_path = f'{fake}:/usr/bin:/bin'

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def run_guard(self, path):
        env = dict(os.environ, PATH=path)
        return subprocess.run([str(self.binary), '-test.run', '^TestChatCoreStaysFree$', '-test.v'],
                              cwd=ROOT / 'internal' / 'repo', env=env, capture_output=True, text=True,
                              timeout=120)

    def test_guard_runs_with_a_working_go_tool(self):
        r = self.run_guard(os.environ['PATH'])
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('--- PASS: TestChatCoreStaysFree', r.stdout)

    @unittest.expectedFailure
    def test_guard_fails_when_the_go_tool_fails(self):
        r = self.run_guard(self.fake_path)
        self.assertNotIn('--- SKIP', r.stdout)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == '__main__':
    unittest.main()
