#!/usr/bin/env python3
"""A gate whose tool does not run must fail, not pass as "no findings".

scripts/staticcheck.sh and scripts/complexity-check.sh both treat a line that
is neither a finding nor its detail as a failure of the tool itself, so a tool
version that cannot be fetched or built, or a source file the tool cannot
parse, fails the run instead of reading as "nothing to report".
"""
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
MISSING = 'v0.0.0-does-not-exist'
GOCOGNIT = re.search(r'^GOCOGNIT_VERSION \?= (\S+)$', (ROOT / 'Makefile').read_text(), re.M).group(1)

# Six nested ifs score 1+2+3+4+5+6 = 21, above the limit of 15.
COMPLEX = ('package bad\n\nfunc Deep(a, b int) int {\n'
           + ''.join('if a > %d {\n' % i for i in range(6))
           + 'b++\n' + '}\n' * 6 + 'return b\n}\n')


def run_gate(script, version=MISSING, root=ROOT):
    return subprocess.run(['bash', str(root / 'scripts' / script), version], cwd=root,
                          capture_output=True, text=True, timeout=300)


def tree_with(source):
    """A copy of the complexity gate over a tree that holds one Go file."""
    tmp = tempfile.TemporaryDirectory()
    root = Path(tmp.name)
    (root / 'scripts').mkdir()
    for name in ('complexity-check.sh', 'complexity-allowlist.txt'):
        shutil.copy(ROOT / 'scripts' / name, root / 'scripts' / name)
    for d in ('cmd', 'internal/bad', 'loadtest/viewer'):
        (root / d).mkdir(parents=True)
    (root / 'internal' / 'bad' / 'bad.go').write_text(source)
    return tmp, root


class GateToolFailure(unittest.TestCase):
    def test_staticcheck_fails_when_the_tool_does_not_run(self):
        r = run_gate('staticcheck.sh')
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('did not run cleanly', r.stdout)

    def test_complexity_check_fails_when_the_tool_does_not_run(self):
        r = run_gate('complexity-check.sh')
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('did not run cleanly', r.stdout)

    def test_complexity_check_fails_on_a_file_that_does_not_parse(self):
        tmp, root = tree_with('package bad\n\nfunc Broken( {\n')
        with tmp:
            r = run_gate('complexity-check.sh', GOCOGNIT, root)
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn('did not run cleanly', r.stdout)

    def test_complexity_check_still_reports_a_function_over_the_limit(self):
        tmp, root = tree_with(COMPLEX)
        with tmp:
            r = run_gate('complexity-check.sh', GOCOGNIT, root)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn('internal/bad Deep', r.stdout)
        self.assertNotIn('did not run cleanly', r.stdout)

    def test_complexity_check_passes_a_clean_tree(self):
        tmp, root = tree_with('package bad\n\nfunc Flat(a int) int { return a + 1 }\n')
        with tmp:
            r = run_gate('complexity-check.sh', GOCOGNIT, root)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)


if __name__ == '__main__':
    unittest.main()
