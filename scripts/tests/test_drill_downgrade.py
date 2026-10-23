#!/usr/bin/env python3
"""Whether the restore drill expects the old image to refuse the migrated data.

Sources scripts/backup-drill-downgrade.sh and runs build_has_newer_migrations
against a fake migrations directory; no Docker is used.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(os.environ.get('DRILL_TEST_ROOT', Path(__file__).resolve().parents[2]))
SCRIPT = ROOT / 'scripts' / 'backup-drill-downgrade.sh'


def newer(files, source_latest):
    with tempfile.TemporaryDirectory() as tmp:
        d = Path(tmp) / 'internal' / 'db' / 'migrations'
        d.mkdir(parents=True)
        for name in files:
            (d / name).write_text('')
        return subprocess.run(
            ['bash', '-c', f'set -uo pipefail; source "{SCRIPT}"; build_has_newer_migrations "{tmp}" "{source_latest}"'],
            capture_output=True, text=True, timeout=30).returncode == 0


class BuildHasNewerMigrations(unittest.TestCase):
    def test_equal_schema_is_not_newer(self):
        self.assertFalse(newer(['0047_a.sql', '0048_polls.sql'], '0048_polls.sql'))

    def test_extra_migration_is_newer(self):
        self.assertTrue(newer(['0048_polls.sql', '0049_x.sql'], '0048_polls.sql'))

    def test_build_behind_source_is_not_newer(self):
        self.assertFalse(newer(['0047_a.sql'], '0048_polls.sql'))


if __name__ == '__main__':
    unittest.main()
