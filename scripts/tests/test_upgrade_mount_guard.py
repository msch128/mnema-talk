#!/usr/bin/env python3
"""Exercise the actual upgrade script with a stop/list replica-lag boundary.

No Docker daemon, database, real secrets, ports, or community data are used.
The fake records every invocation; only container enumeration is stale.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT_ROOT = Path(os.environ.get('UPGRADE_TEST_ROOT', Path(__file__).resolve().parents[2]))
DOCKER = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as f:
    f.write(json.dumps(args) + '\n')
scenario = os.environ['SCENARIO']
if args[:2] == ['compose', 'config']:
    print('name: upgrade-fixture')
elif args[:2] == ['volume', 'ls']:
    if any(x.endswith('volume=postgres-data') for x in args): print('fixture-old')
elif args[:2] == ['compose', 'stop']:
    if scenario == 'stop-failure': sys.exit(42)
elif args[0] == 'ps':
    if scenario == 'list-failure': sys.exit(42)
    if scenario == 'no-mounts': pass
    elif scenario == 'multiple': print('stopped-first\nrunning-second')
    else: print('fixture-postgres')
elif args[0] == 'inspect':
    if scenario == 'inspect-failure': sys.exit(42)
    if scenario == 'removed': sys.exit(1)
    if scenario in ('running', 'paused', 'restarting') or args[-1] == 'running-second': print('true')
    elif scenario == 'malformed': print('unknown')
    elif scenario == 'empty-state': pass
    else: print('false')
elif args[:2] == ['volume', 'create']: print('fixture-copy')
elif args[0] == 'run' and '-d' in args: print('fixture-dump-server')
elif args[0] == 'exec' and 'pg_dump' in args: print('dummy SQL fixture only')
elif args[:2] == ['compose', 'exec']:
    if 'psql' in args: sys.stdin.read()
elif args[0] not in ('run', 'exec', 'stop', 'rm', 'volume', 'compose'):
    sys.exit(99)
'''

class UpgradeGuardTests(unittest.TestCase):
    def run_case(self, scenario):
        with tempfile.TemporaryDirectory(prefix='mnema-upgrade-guard-') as directory:
            root = Path(directory)
            deploy = root / 'deploy'; deploy.mkdir()
            (deploy / '.env').write_text('DUMMY_ONLY=placeholder\n')
            binary = root / 'bin'; binary.mkdir()
            docker = binary / 'docker'; docker.write_text(DOCKER); docker.chmod(0o755)
            log = root / 'calls.jsonl'
            env = dict(os.environ, PATH=str(binary) + os.pathsep + os.environ['PATH'],
                       BACKUP_DIR=str(root / 'backups'), FAKE_LOG=str(log), SCENARIO=scenario)
            result = subprocess.run(['bash', str(SCRIPT_ROOT / 'scripts/upgrade-postgres.sh')],
                                    cwd=deploy, env=env, text=True, capture_output=True, timeout=10)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            self.assertFalse((deploy / '.mnema-maintenance.lock').exists())
            return result, calls

    def assert_no_copy_or_database_start(self, calls):
        self.assertFalse(any(c[:2] == ['volume', 'create'] for c in calls))
        self.assertFalse(any(c[0] == 'run' and '-d' in c for c in calls))
        self.assertFalse(any(c[:2] == ['compose', 'up'] for c in calls))
        self.assertFalse(any('pg_dump' in c for c in calls))

    def test_stale_running_list_requires_live_stopped_inspection_before_copy(self):
        result, calls = self.run_case('stale-stopped')
        self.assertEqual(result.returncode, 0, result.stderr)
        stop = next(i for i,c in enumerate(calls) if c[:2] == ['compose', 'stop'])
        listing = next(i for i,c in enumerate(calls) if c[0] == 'ps')
        inspection = next(i for i,c in enumerate(calls) if c[0] == 'inspect')
        copy = next(i for i,c in enumerate(calls) if c[:2] == ['volume', 'create'])
        self.assertLess(stop, listing); self.assertLess(listing, inspection); self.assertLess(inspection, copy)
        self.assertEqual(calls[listing], ['ps', '-aq', '--filter', 'volume=fixture-old'])
        self.assertEqual(calls[inspection], ['inspect', '-f', '{{.State.Running}}', 'fixture-postgres'])
        original_mounts = [arg for c in calls for arg in c if arg.startswith('fixture-old:')]
        self.assertTrue(original_mounts)
        self.assertTrue(all(arg.endswith(':ro') for arg in original_mounts))

    def test_running_paused_and_restarting_mounts_remain_blocked(self):
        for scenario in ('running', 'paused', 'restarting'):
            with self.subTest(scenario=scenario):
                result, calls = self.run_case(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('still mounted by a running container', result.stderr)
                self.assert_no_copy_or_database_start(calls)

    def test_later_live_mount_is_not_hidden_by_first_stopped_container(self):
        result, calls = self.run_case('multiple')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('running-second', result.stderr)
        self.assertEqual([c[-1] for c in calls if c[0] == 'inspect'], ['stopped-first', 'running-second'])
        self.assert_no_copy_or_database_start(calls)

    def test_list_failure_never_authorizes_copy(self):
        result, calls = self.run_case('list-failure')
        self.assertNotEqual(result.returncode, 0)
        self.assert_no_copy_or_database_start(calls)

    def test_failed_missing_empty_or_malformed_inspection_never_authorizes_copy(self):
        for scenario in ('inspect-failure', 'removed', 'empty-state', 'malformed'):
            with self.subTest(scenario=scenario):
                result, calls = self.run_case(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assert_no_copy_or_database_start(calls)

    def test_failed_compose_stop_never_reaches_mount_guard_or_copy(self):
        result, calls = self.run_case('stop-failure')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[0] in ('ps', 'inspect') for c in calls))
        self.assert_no_copy_or_database_start(calls)

    def test_empty_successful_mount_list_can_complete_upgrade(self):
        result, calls = self.run_case('no-mounts')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(c[0] == 'inspect' for c in calls))
        self.assertTrue(any(c[:2] == ['volume', 'create'] for c in calls))

if __name__ == '__main__': unittest.main(verbosity=2)
