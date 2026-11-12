#!/usr/bin/env python3
"""scripts/security-floor.sh raises the floor only on an explicit trailer, and
every later release keeps the highest floor raised so far."""
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

    def run_script(self, *args, stdin=None):
        return subprocess.run(['sh', str(SCRIPT), *args], cwd=self.repo, input=stdin,
                              capture_output=True, text=True)

    def refused(self, *args):
        r = self.run_script(*(args or ('HEAD', '0.2.0')))
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

    def test_marker_in_another_commit_of_the_release_fails_too(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.commit('fix: y\n\n  security-floor : RAISE\n\nprose')
        self.refused()

    def test_marker_as_a_trailer_and_quoted_in_prose_is_read(self):
        self.commit('docs: the rule\n\nA fix carries a Security-Floor: raise trailer.\n\nSecurity-Floor: raise')
        self.assertEqual(self.decide(), ('raise', ''))

    def test_a_misplaced_marker_of_an_earlier_release_is_not_checked_again(self):
        self.commit('feat: old\n\nSecurity-Floor: raise\n\nBREAKING CHANGE: y.')
        self.git('tag', 'v0.2.0')
        self.refused('v0.2.0', '0.2.0')
        self.commit('fix: later')
        self.assertEqual(self.floor('HEAD', '0.3.0'), ('', ''))

    # The pull request check (ci.yml): the branch's commits and its
    # description, before the squash commit reaches main.
    def test_check_commits_mode(self):
        self.commit('fix: x\n\nSecurity-Floor: raise')
        self.assertEqual(self.run_script('--check-commits', 'v0.1.0..HEAD').returncode, 0)
        self.commit('fix: y\n\nSecurity-Floor: raise\n\nRefs: none')
        r = self.run_script('--check-commits', 'v0.1.0..HEAD')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('not a git trailer', r.stderr)

    def test_check_message_mode(self):
        cases = {'Body.\r\n\r\nSecurity-Floor: raise\r\n': True, 'Body only.': True, '': True,
                 'Text.\n\nRelease-As: 1.2.0\nSecurity-Floor: raise\n': True,
                 'Text.\r\n\r\nSecurity-Floor: raise\r\n\r\nBREAKING CHANGE: y\r\n': False,
                 'Security-Floor: raise\n\nmore': False}
        for message, ok in cases.items():
            r = self.run_script('--check-message', stdin=message)
            self.assertEqual(r.returncode == 0, ok, (message, r.stderr))

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
        # Commits on top of v3.0.9, made in a throwaway clone (no checkout:
        # commit-tree reuses 3.0.9's tree). The history holds the misplaced
        # 1.2.0 marker (c50b6886e), which must not fail a later release; a
        # new misplaced marker in the release's own range must.
        root = SCRIPT.parents[1]
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            clone = os.path.join(tmp, 'c')
            subprocess.run(['git', 'clone', '-q', '--shared', '--no-checkout', str(root), clone], check=True,
                           capture_output=True)
            env = dict(os.environ, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@example.invalid',
                       GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@example.invalid')

            def commit(parent, message):
                return subprocess.run(['git', 'commit-tree', 'v3.0.9^{tree}', '-p', parent, '-m', message],
                                      cwd=clone, env=env, check=True, capture_output=True, text=True).stdout.strip()

            plain = commit('v3.0.9', 'fix: no trailer')
            r = subprocess.run(['sh', str(SCRIPT), plain, '3.0.10'], cwd=clone, capture_output=True, text=True)
            self.assertEqual((r.returncode, r.stdout.strip()), (0, '3.0.9'), r.stderr)
            bad = commit(plain, 'feat: y\n\nSecurity-Floor: raise\n\nBREAKING CHANGE: z.')
            r = subprocess.run(['sh', str(SCRIPT), bad, '3.0.10'], cwd=clone, capture_output=True, text=True)
            self.assertNotEqual(r.returncode, 0)
            self.assertIn(bad[:8], r.stderr)

class DocumentedRaises(unittest.TestCase):
    """Every release the documents say raised the floor really raised it.

    The floor a signed manifest declares is decided by this script, so a
    sentence such as "the security floor is raised to X.Y.Z" must name a
    release that the script reads as raising the floor to X.Y.Z (hunt
    finding H-L11-01: SECURITY.md claimed 1.2.0, the signed 1.2.0 manifest
    declares 0.7.23)."""

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
            r = subprocess.run(['sh', str(SCRIPT), 'v' + version, version], cwd=root,
                               capture_output=True, text=True)
            self.assertEqual((r.returncode, r.stdout.strip()), (0, version),
                             f'{name} says the floor was raised to {version}; the release did not raise it')


if __name__ == '__main__':
    unittest.main()
