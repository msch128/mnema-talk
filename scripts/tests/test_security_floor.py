#!/usr/bin/env python3
"""scripts/security-floor.sh raises the floor only on an explicit trailer, and
every later release keeps the highest floor raised so far."""
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
        self.git('tag', 'v0.1.0')

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args):
        env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@example.invalid',
                   GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid')
        return subprocess.run(['git', '-c', 'gc.auto=0', '-c', 'maintenance.auto=false', *args], cwd=self.repo, env=env, check=True,
                              capture_output=True, text=True).stdout

    def commit(self, message):
        self.git('commit', '-q', '--allow-empty', '-m', message)

    def floor(self, rev='HEAD', version='0.2.0', cwd=None):
        r = subprocess.run(['sh', str(SCRIPT), rev, version], cwd=cwd or self.repo, check=True,
                           capture_output=True, text=True)
        return r.stdout.strip(), r.stderr

    def decide(self):
        # The decision for the untagged release 0.2.0 on top of v0.1.0.
        floor, warnings = self.floor()
        return {'0.2.0': 'raise', '': 'keep'}.get(floor, floor), warnings

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

    # Known failure (hunt finding H-L11-01): the 1.2.0 release commit carried
    # "Security-Floor: raise" followed by a "BREAKING CHANGE:" paragraph, so
    # git did not read it as a trailer, the floor stayed 0.7.23 and nothing
    # warned. A marker that is not a trailer must at least warn; this test
    # turns red (unexpected success) the day it does.
    @unittest.expectedFailure
    def test_marker_that_is_not_a_trailer_warns(self):
        self.commit('feat: a release\n\nRelease-As: 1.2.0\nSecurity-Floor: raise\n\n'
                    'BREAKING CHANGE: an optional field.')
        decision, warnings = self.decide()
        self.assertEqual(decision, 'keep')
        self.assertIn('Security-Floor', warnings)

    def test_other_value_keeps(self):
        self.commit('fix: x\n\nSecurity-Floor: lower')
        self.assertEqual(self.decide()[0], 'keep')

    def test_no_marker_keeps(self):
        self.commit('feat: something')
        self.assertEqual(self.decide(), ('keep', ''))


    # Hunt finding H-L11-02: a raise used to live in the raising release's
    # manifest only; the next release declared the compat file's floor again.
    def test_a_raise_is_carried_into_every_later_release(self):
        self.commit('fix(security): x\n\nSecurity-Floor: raise')
        self.git('tag', 'v0.2.0')
        self.commit('fix: y')
        self.git('tag', 'v0.3.0')
        self.commit('fix: z')
        self.assertEqual(self.floor('v0.2.0', '0.2.0')[0], '0.2.0')
        self.assertEqual(self.floor('v0.3.0', '0.3.0')[0], '0.2.0')
        self.assertEqual(self.floor('HEAD', '0.4.0'), ('0.2.0', ''))

    def test_a_later_raise_wins_and_an_older_one_cannot_lower_it(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.git('tag', 'v0.2.0')
        self.commit('fix: y\n\nSecurity-Floor: raise')
        self.git('tag', 'v0.10.0')
        self.commit('fix: z')
        self.assertEqual(self.floor('HEAD', '0.11.0')[0], '0.10.0')
        self.commit('fix: w\n\nSecurity-Floor: raise')
        self.assertEqual(self.floor('HEAD', '0.11.0')[0], '0.11.0')

    def test_a_missing_tag_moves_the_raise_up_never_down(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.commit('fix: y')
        self.git('tag', 'v0.3.0')
        self.commit('fix: z')
        self.assertEqual(self.floor('HEAD', '0.4.0')[0], '0.3.0')

    def test_a_shallow_clone_is_refused(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.git('tag', 'v0.2.0')
        self.commit('fix: y')
        clone = os.path.join(self.repo, 'shallow')
        subprocess.run(['git', 'clone', '-q', '--depth', '1', 'file://' + self.repo, clone], check=True,
                       capture_output=True)
        r = subprocess.run(['sh', str(SCRIPT), 'HEAD', '0.3.0'], cwd=clone, capture_output=True, text=True)
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('shallow', r.stderr)

    def test_a_version_that_is_not_semver_is_refused(self):
        r = subprocess.run(['sh', str(SCRIPT), 'HEAD', 'v0.2.0'], cwd=self.repo, capture_output=True, text=True)
        self.assertNotEqual(r.returncode, 0)


class RealHistory(unittest.TestCase):
    """The floors the script yields for this repository's own releases."""

    # Published manifests declared 3.0.4, 0.7.23, 3.0.6, 0.7.23, 0.7.23, 3.0.9
    # for these releases before the floor was carried forward (H-L11-02).
    WANT = {'3.0.4': '3.0.4', '3.0.5': '3.0.4', '3.0.6': '3.0.6', '3.0.7': '3.0.6',
            '3.0.8': '3.0.6', '3.0.9': '3.0.9'}

    def run_floor(self, rev, version):
        root = SCRIPT.parents[1]
        return subprocess.run(['sh', str(SCRIPT), rev, version], cwd=root, check=True,
                              capture_output=True, text=True).stdout.strip()

    def test_releases_from_3_0_4_on(self):
        root = SCRIPT.parents[1]
        tags = subprocess.run(['git', 'tag', '--list', 'v3.0.*'], cwd=root, check=True,
                              capture_output=True, text=True).stdout.split()
        missing = [v for v in self.WANT if 'v' + v not in tags]
        self.assertEqual(missing, [], 'the release tags are missing: fetch the full history with tags')
        for version, want in self.WANT.items():
            self.assertEqual(self.run_floor('v' + version, version), want, version)

    def test_a_later_release_without_a_trailer_keeps_3_0_9(self):
        # A commit without the trailer on top of v3.0.9, made in a throwaway
        # clone (no checkout: commit-tree reuses 3.0.9's tree).
        root = SCRIPT.parents[1]
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            clone = os.path.join(tmp, 'c')
            subprocess.run(['git', 'clone', '-q', '--shared', '--no-checkout', str(root), clone], check=True,
                           capture_output=True)
            env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@example.invalid',
                       GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid')
            sha = subprocess.run(['git', 'commit-tree', 'v3.0.9^{tree}', '-p', 'v3.0.9', '-m', 'fix: no trailer'],
                                 cwd=clone, env=env, check=True, capture_output=True, text=True).stdout.strip()
            r = subprocess.run(['sh', str(SCRIPT), sha, '3.0.10'], cwd=clone, check=True,
                               capture_output=True, text=True)
            self.assertEqual(r.stdout.strip(), '3.0.9')

if __name__ == '__main__':
    unittest.main()
