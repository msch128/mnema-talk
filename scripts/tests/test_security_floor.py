#!/usr/bin/env python3
"""scripts/security-floor.sh raises the floor only on an explicit trailer."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'security-floor.sh'


class SecurityFloor(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(ignore_cleanup_errors=True)
        self.repo = self.tmp.name
        self.git('init', '-q')
        self.git('commit', '-q', '--allow-empty', '-m', 'chore: base')
        self.git('tag', 'base')

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args):
        env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@example.invalid',
                   GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid')
        return subprocess.run(['git', '-c', 'gc.auto=0', '-c', 'maintenance.auto=false', *args], cwd=self.repo, env=env, check=True,
                              capture_output=True, text=True).stdout

    def commit(self, message):
        self.git('commit', '-q', '--allow-empty', '-m', message)

    def decide(self):
        r = subprocess.run(['sh', str(SCRIPT), 'base..HEAD'], cwd=self.repo, check=True,
                           capture_output=True, text=True)
        return r.stdout.strip(), r.stderr

    def test_trailer_raises(self):
        self.commit('fix(web): escape names\n\nBody text.\n\nSecurity-Floor: raise')
        self.assertEqual(self.decide()[0], 'raise')

    def test_trailer_among_other_trailers(self):
        self.commit('fix: x\n\nSecurity-Floor: raise\nCo-authored-by: A <a@example.invalid>')
        self.assertEqual(self.decide()[0], 'raise')

    def test_security_scope_alone_does_not_raise_but_warns(self):
        self.commit('fix(security): close a hole')
        decision, warnings = self.decide()
        self.assertEqual(decision, 'keep')
        self.assertIn('security scope but no', warnings)

    def test_lookalike_scope_is_silent_and_keeps(self):
        self.commit('docs(security-notes): reword')
        decision, _ = self.decide()
        self.assertEqual(decision, 'keep')

    def test_marker_in_the_middle_of_the_body_is_not_a_trailer(self):
        self.commit('fix: x\n\nSecurity-Floor: raise\n\nMore prose after it.')
        self.assertEqual(self.decide()[0], 'keep')

    def test_other_value_keeps(self):
        self.commit('fix: x\n\nSecurity-Floor: lower')
        self.assertEqual(self.decide()[0], 'keep')

    def test_no_marker_keeps(self):
        self.commit('feat: something')
        self.assertEqual(self.decide(), ('keep', ''))


if __name__ == '__main__':
    unittest.main()
