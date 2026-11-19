#!/usr/bin/env python3
"""scripts/retry.sh: bounded retry with backoff for external CI calls."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(os.environ.get('DRILL_TEST_ROOT', Path(__file__).resolve().parents[2]))
RETRY = ROOT / 'scripts' / 'retry.sh'

# Fails the first FAILS calls with status 7, then succeeds; counts calls in a file.
FLAKY = r'''#!/bin/sh
f="$COUNTER"; n=$(cat "$f" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$f"
[ "$n" -gt "${FAILS:-0}" ] || exit 7
'''


class Retry(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.flaky = Path(self.tmp.name) / 'flaky'
        self.flaky.write_text(FLAKY)
        self.flaky.chmod(0o755)
        self.counter = Path(self.tmp.name) / 'count'

    def run_retry(self, fails, *opts):
        env = dict(os.environ, COUNTER=str(self.counter), FAILS=str(fails))
        res = subprocess.run([str(RETRY), '-d', '0', *opts, '--', str(self.flaky)],
                             capture_output=True, text=True, env=env, timeout=30)
        calls = int(self.counter.read_text()) if self.counter.exists() else 0
        return res, calls

    def test_success_runs_once(self):
        res, calls = self.run_retry(0)
        self.assertEqual((res.returncode, calls), (0, 1))

    def test_transient_failure_is_retried(self):
        res, calls = self.run_retry(2)
        self.assertEqual((res.returncode, calls), (0, 3))
        self.assertIn('attempt 2 of 4', res.stderr)

    def test_gives_up_after_the_bound_with_the_last_status(self):
        res, calls = self.run_retry(99, '-n', '3')
        self.assertEqual((res.returncode, calls), (7, 3))
        self.assertIn('failed after 3 attempts', res.stderr)

    def test_one_attempt_means_no_retry(self):
        res, calls = self.run_retry(1, '-n', '1')
        self.assertEqual((res.returncode, calls), (7, 1))

    def test_bad_arguments_are_rejected(self):
        for args in (['-n', '0', '--', 'true'], ['-n', 'x', '--', 'true'], ['-d', '-1', '--', 'true'], ['--']):
            res = subprocess.run([str(RETRY), *args], capture_output=True, text=True, timeout=10)
            self.assertEqual(res.returncode, 2, args)


if __name__ == '__main__':
    unittest.main()
