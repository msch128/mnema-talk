#!/usr/bin/env python3
"""scripts/security-floor.sh raises the floor only on an explicit trailer."""
import os
from pathlib import Path
import re
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

    def refused(self):
        r = subprocess.run(['sh', str(SCRIPT), 'base..HEAD'], cwd=self.repo, capture_output=True, text=True)
        self.assertNotEqual(r.returncode, 0, r.stdout)
        self.assertEqual(r.stdout, '')
        return r.stderr

    def test_marker_in_the_middle_of_the_body_fails_the_decision(self):
        self.commit('fix: x\n\nSecurity-Floor: raise\n\nMore prose after it.')
        self.assertIn('not a git trailer', self.refused())

    # Hunt finding H-L11-01: the 1.2.0 release commit carried
    # "Security-Floor: raise" followed by a "BREAKING CHANGE:" paragraph, so
    # git did not read it as a trailer, the floor stayed 0.7.23 and nothing
    # warned. Such a marker now fails the decision instead of reading as keep.
    def test_marker_that_is_not_a_trailer_fails_the_decision(self):
        self.commit('feat: a release\n\nRelease-As: 1.2.0\nSecurity-Floor: raise\n\n'
                    'BREAKING CHANGE: an optional field.')
        warnings = self.refused()
        self.assertIn('Security-Floor', warnings)
        self.assertIn(self.git('rev-parse', '--short=8', 'HEAD').strip(), warnings)

    def test_marker_in_another_commit_of_the_range_fails_too(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.commit('fix: y\n\n  security-floor : RAISE\n\nprose')
        self.refused()

    def test_marker_as_a_trailer_and_quoted_in_prose_is_read(self):
        self.commit('docs: the rule\n\nA fix carries a Security-Floor: raise trailer.\n\nSecurity-Floor: raise')
        self.assertEqual(self.decide(), ('raise', ''))

    def test_other_value_keeps(self):
        self.commit('fix: x\n\nSecurity-Floor: lower')
        self.assertEqual(self.decide()[0], 'keep')

    def test_no_marker_keeps(self):
        self.commit('feat: something')
        self.assertEqual(self.decide(), ('keep', ''))


class DocumentedRaises(unittest.TestCase):
    """Every release the documents say raised the floor really raised it.

    The floor a signed manifest declares is decided by this script from the
    release's own commit range, so a sentence such as "the security floor is
    raised to X.Y.Z" must name a release whose range the script reads as a
    raise (hunt finding H-L11-01: SECURITY.md claimed 1.2.0, the signed 1.2.0
    manifest declares 0.7.23)."""

    CLAIM = re.compile(r'(?i)\brais(?:e|es|ed|ing)\b[^.]{0,120}?\bto\s+`?(\d+\.\d+\.\d+)')

    def test_documented_raises_match_the_releases(self):
        root = SCRIPT.parents[1]
        files = [root / 'SECURITY.md', root / 'AGENTS.md', *sorted((root / 'doc').rglob('*.md'))]
        claims = set()
        for f in files:
            flat = re.sub(r'\s+', ' ', f.read_text(encoding='utf-8'))
            for m in self.CLAIM.finditer(flat):
                sentence = flat[flat.rfind('.', 0, m.start()) + 1:m.end()]
                if 'floor' in sentence.lower():
                    claims.add((f.name, m.group(1)))
        for name, version in sorted(claims):
            tag = 'v' + version
            prev = subprocess.run(['git', 'describe', '--tags', '--abbrev=0', tag + '^'], cwd=root,
                                  capture_output=True, text=True)
            self.assertEqual(prev.returncode, 0, f'{name}: no release tag before {tag}; fetch the tags')
            r = subprocess.run(['sh', str(SCRIPT), f'{prev.stdout.strip()}..{tag}'], cwd=root,
                               capture_output=True, text=True)
            self.assertEqual((r.returncode, r.stdout.strip()), (0, 'raise'),
                             f'{name} says the floor was raised to {version}; the release did not raise it')


if __name__ == '__main__':
    unittest.main()
