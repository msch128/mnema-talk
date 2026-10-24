#!/usr/bin/env python3
"""Version selection and the bounded registry retry of the release drills.

Sources scripts/drill-common.sh and runs its functions; docker is a fake on
PATH, so no daemon, registry or network is used.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(os.environ.get('DRILL_TEST_ROOT', Path(__file__).resolve().parents[2]))
COMMON = ROOT / 'scripts' / 'drill-common.sh'

# Pulls fail the first FAILS times with a transient error (or always with the
# message in MODE), then succeed. The call count lives in a file.
FAKE_DOCKER = r'''#!/usr/bin/env python3
import os, sys
counter = os.environ['PULL_COUNTER']
n = int(open(counter).read() or 0) + 1 if os.path.exists(counter) else 1
open(counter, 'w').write(str(n))
mode = os.environ.get('PULL_MODE', 'ok')
if mode == 'missing':
    print('Error response from daemon: manifest unknown', file=sys.stderr); sys.exit(1)
if mode == 'down' or (mode == 'flaky' and n <= int(os.environ.get('PULL_FAILS', '2'))):
    print('Error response from daemon: Get "https://registry/v2/": 503 Service Unavailable', file=sys.stderr); sys.exit(1)
'''


def run_shell(snippet, env=None, path_extra=None):
    full = dict(os.environ)
    full.update(env or {})
    if path_extra:
        full['PATH'] = f'{path_extra}:{full["PATH"]}'
    return subprocess.run(['bash', '-c', f'set -uo pipefail; source "{COMMON}"; {snippet}'],
                          capture_output=True, text=True, env=full, timeout=60)


class ResolveVersions(unittest.TestCase):
    def resolve(self, value):
        return run_shell(f'drill_resolve_versions "{value}"')

    def test_default_matrix_is_the_supported_sources(self):
        r = self.resolve('')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.split(), ['0.8.16', '0.9.0', '1.0.0'])

    def test_commas_spaces_prefix_and_duplicates(self):
        r = self.resolve('v0.9.0, 0.8.16 0.9.0')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stdout.split(), ['0.9.0', '0.8.16'])

    def test_malformed_entries_fail_loudly(self):
        for bad in ('latest', '0.9', '0.9.0-rc1', '1.0.x', '0.8.16 oops'):
            with self.subTest(bad=bad):
                r = self.resolve(bad)
                self.assertNotEqual(r.returncode, 0)
                self.assertIn('FROM_VERSIONS', r.stderr)

    def test_versions_older_than_the_seed_supports_are_refused(self):
        r = self.resolve('0.7.25')
        self.assertNotEqual(r.returncode, 0)
        self.assertIn('older than 0.8.0', r.stderr)

    def test_version_ge_compares_numerically(self):
        for a, b, want in (('0.9.0', '0.9.0', 0), ('0.10.0', '0.9.0', 0), ('0.8.16', '0.9.0', 1),
                           ('0.8.16', '0.8.2', 0), ('1.0.0', '0.9.0', 0)):
            with self.subTest(a=a, b=b):
                self.assertEqual(run_shell(f'drill_version_ge {a} {b}').returncode, want)


class PullRetry(unittest.TestCase):
    def pull(self, mode, attempts='3', fails='2'):
        with tempfile.TemporaryDirectory() as tmp:
            fake = Path(tmp) / 'docker'
            fake.write_text(FAKE_DOCKER)
            fake.chmod(0o755)
            counter = Path(tmp) / 'count'
            r = run_shell('drill_pull_image ghcr.io/example/app:0.9.0',
                          env={'PULL_MODE': mode, 'PULL_COUNTER': str(counter), 'PULL_FAILS': fails,
                               'DRILL_PULL_ATTEMPTS': attempts, 'DRILL_PULL_DELAY': '0'},
                          path_extra=tmp)
            calls = int(counter.read_text()) if counter.exists() else 0
            return r, calls

    def test_transient_failures_are_retried(self):
        r, calls = self.pull('flaky', attempts='3', fails='2')
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(calls, 3)

    def test_outage_fails_after_the_bound_with_a_clear_message(self):
        r, calls = self.pull('down', attempts='3')
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(calls, 3)
        self.assertIn('registry unavailable', r.stderr)
        self.assertIn('not skipped', r.stderr)

    def test_missing_tag_fails_at_once_without_retries(self):
        r, calls = self.pull('missing', attempts='5')
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(calls, 1)
        self.assertIn('does not exist', r.stderr)


class HealthFeatures(unittest.TestCase):
    def test_reports_ok_when_all_features_present(self):
        json_str = '{"features": ["blocks", "dm", "saved"]}'
        r = run_shell(f'drill_health_features_ok \'{json_str}\' dm blocks')
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_fails_when_feature_missing(self):
        json_str = '{"features": ["blocks"]}'
        r = run_shell(f'drill_health_features_ok \'{json_str}\' dm blocks')
        self.assertNotEqual(r.returncode, 0)

    def test_fails_on_empty_or_missing_features(self):
        for bad in ('{}', '{"features": []}', '', '{"features": null}'):
            with self.subTest(bad=bad):
                r = run_shell(f'drill_health_features_ok \'{bad}\' dm')
                self.assertNotEqual(r.returncode, 0)


if __name__ == '__main__':
    unittest.main()
