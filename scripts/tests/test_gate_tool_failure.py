#!/usr/bin/env python3
"""A gate whose tool does not run must fail, not pass as "no findings".

scripts/staticcheck.sh guards this (a line that is neither a finding nor its
detail fails the run). scripts/complexity-check.sh replaces the tool's exit
status with `|| true` and has no such guard, so a gocognit version that cannot
be fetched or built passes `make complexity-check`. That case is recorded as an
expected failure: it turns red the day the script fails on a broken tool.
"""
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
MISSING = 'v0.0.0-does-not-exist'


def run_gate(script):
    return subprocess.run(['bash', str(ROOT / 'scripts' / script), MISSING], cwd=ROOT,
                          capture_output=True, text=True, timeout=300)


class GateToolFailure(unittest.TestCase):
    def test_staticcheck_fails_when_the_tool_does_not_run(self):
        r = run_gate('staticcheck.sh')
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('did not run cleanly', r.stdout)

    @unittest.expectedFailure
    def test_complexity_check_fails_when_the_tool_does_not_run(self):
        r = run_gate('complexity-check.sh')
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == '__main__':
    unittest.main()
