#!/usr/bin/env python3
"""Fault-injected lifecycle tests; all data and Docker calls are disposable."""
import gzip
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
FAKE_DOCKER = r'''#!/usr/bin/env python3
import gzip,io,json,os,sys,tarfile
from pathlib import Path
p=Path(os.environ['FAKE_STATE']); state=json.loads(p.read_text()); args=sys.argv[1:]
with open(os.environ['FAKE_LOG'],'a') as f: f.write(json.dumps(args)+'\n')
def done(code=0): p.write_text(json.dumps(state)); sys.exit(code)
if args[:2]==['compose','config']: print('services: {}'); done()
if args[:2]==['compose','ps']: print(args[-1]); done()
if args[0]=='inspect' and 'Health' in args[2]: print('healthy' if state.get(args[-1],False) else 'false'); done()
if args[0]=='inspect': print(str(state.get(args[-1],False)).lower() if args[2]=='{{.State.Running}}' else 'sha256:fixture'); done()
if args[:2]==['compose','stop']:
 if os.environ.get('FAIL_AT')=='stop': done(1)
 state[args[-1]]=False; done()
if args[:2]==['compose','start']:
 if os.environ.get('FAIL_AT')=='resume-seaweed' and args[-1]=='seaweedfs': done(1)
 state[args[-1]]=True; done()
if args[:2]==['compose','exec']:
 text=args[-1]
 if 'pg_dump' in text:
  print('fixture dump')
  done(1 if os.environ.get('FAIL_AT')=='dump' else 0)
 if 'SELECT filename' in text: print('0001_initial.sql|fixture-checksum')
 if 'psql' in text and '-c' not in text:
  sys.stdin.read(); done(1 if os.environ.get('FAIL_AT')=='import' else 0)
 done()
if args[0]=='run' and '/out/seaweedfs.tar.gz' in ' '.join(args):
 if os.environ.get('FAIL_AT')=='archive': done(1)
 dest=Path(args[args.index('-v')+1].split(':/out')[0])
 with tarfile.open(dest/'seaweedfs.tar.gz','w:gz') as t:
  b=b'fixture'; i=tarfile.TarInfo('./object'); i.size=len(b); t.addfile(i,io.BytesIO(b))
 done()
if args[0]=='run' and 'postgres:18-alpine' in args: print('isolated-check'); done()
if args[0]=='run': done(1 if os.environ.get('FAIL_AT')=='extract' else 0)
if args[0]=='exec':
 if 'psql' in args: sys.stdin.read()
 done(1 if os.environ.get('FAIL_AT')=='verify-import' and 'psql' in args else 0)
if args[0]=='rm': done()
done(99)
'''

class MaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='mnema-backup-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.deploy = self.root/'deployment'; self.deploy.mkdir()
        (self.deploy/'.env').write_text('DUMMY_ONLY=placeholder\n')
        self.bin = self.root/'bin'; self.bin.mkdir()
        docker = self.bin/'docker'; docker.write_text(FAKE_DOCKER); docker.chmod(0o755)
        self.state = self.root/'state.json'; self.state.write_text(json.dumps({'app':True,'seaweedfs':True,'postgres':True}))
        self.log = self.root/'calls.jsonl'
        self.backups = self.root/'backups'
        self.env = dict(os.environ, PATH=str(self.bin)+os.pathsep+os.environ['PATH'], BACKUP_DIR=str(self.backups), FAKE_STATE=str(self.state), FAKE_LOG=str(self.log))
    def run_script(self, name, *args, fail=''):
        return subprocess.run(['bash',str(ROOT/'scripts'/name),*map(str,args)], cwd=self.deploy, env=dict(self.env,FAIL_AT=fail), capture_output=True, text=True, timeout=15)
    def calls(self): return [json.loads(x) for x in self.log.read_text().splitlines()]
    def good_backup(self):
        r=self.run_script('backup.sh'); self.assertEqual(r.returncode,0,r.stderr)
        return next(p for p in self.backups.iterdir() if p.is_dir())
    def test_snapshot_orders_writers_before_dump_and_marks_completion(self):
        backup=self.good_backup(); calls=self.calls()
        stop=next(i for i,c in enumerate(calls) if c[:2]==['compose','stop'] and c[-1]=='app')
        dump=next(i for i,c in enumerate(calls) if 'pg_dump' in ' '.join(c))
        self.assertLess(stop,dump)
        storage_stop=next(i for i,c in enumerate(calls) if c[:2]==['compose','stop'] and c[-1]=='seaweedfs')
        self.assertLess(storage_stop,dump)
        self.assertTrue((backup/'COMPLETE').is_file())
        self.assertEqual(json.loads(self.state.read_text())['app'],True)
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_backup_failures_resume_prior_state_and_never_prune(self):
        previous=self.good_backup()
        old=self.backups/'20200101-000000'; previous.rename(old)
        os.utime(old,(0,0))
        for failure in ('dump','archive'):
            r=self.run_script('backup.sh',fail=failure); self.assertNotEqual(r.returncode,0)
            self.assertTrue((old/'COMPLETE').exists())
            self.assertTrue(json.loads(self.state.read_text())['app'])
            self.assertTrue(json.loads(self.state.read_text())['seaweedfs'])
            self.assertFalse(any((p/'COMPLETE').exists() for p in self.backups.iterdir() if p!=old))
    def test_retention_only_removes_valid_old_points_after_success(self):
        previous=self.good_backup(); old=self.backups/'20200101-000000'; previous.rename(old); os.utime(old,(0,0))
        incomplete=self.backups/'20200102-000000'; incomplete.mkdir(); os.utime(incomplete,(0,0))
        self.good_backup()
        self.assertFalse(old.exists()); self.assertTrue(incomplete.exists())
    def test_failed_storage_resume_keeps_app_stopped_and_previous_backup(self):
        previous=self.good_backup(); old=self.backups/'20200101-000000'; previous.rename(old); os.utime(old,(0,0))
        self.log.write_text('')
        r=self.run_script('backup.sh',fail='resume-seaweed'); self.assertNotEqual(r.returncode,0)
        self.assertTrue(old.exists())
        self.assertFalse(any(c[:2]==['compose','start'] and c[-1]=='app' for c in self.calls()))
        self.assertFalse(json.loads(self.state.read_text())['app'])
        self.assertFalse(json.loads(self.state.read_text())['seaweedfs'])
    def test_missing_config_rejects_restore_before_mutation(self):
        backup=self.good_backup(); (backup/'env').unlink(); self.log.write_text('')
        self.assertNotEqual(self.run_script('restore.sh',backup,'--yes').returncode,0)
        self.assertEqual(self.calls(),[])
    def test_stopped_services_are_not_started_by_backup(self):
        self.state.write_text(json.dumps({'app':False,'seaweedfs':False,'postgres':True}))
        self.good_backup()
        self.assertFalse(any(c[:2]==['compose','start'] for c in self.calls()))
    def test_failed_writer_stop_never_snapshots_or_replaces_data(self):
        r=self.run_script('backup.sh',fail='stop'); self.assertNotEqual(r.returncode,0)
        self.assertFalse(any('pg_dump' in ' '.join(c) for c in self.calls()))
        self.assertTrue(json.loads(self.state.read_text())['app'])
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_existing_maintenance_lock_blocks_commands(self):
        (self.deploy/'.mnema-maintenance.lock').mkdir()
        r=self.run_script('backup.sh'); self.assertNotEqual(r.returncode,0)
        self.assertFalse(self.log.exists())
        self.assertTrue((self.deploy/'.mnema-maintenance.lock').is_dir())
    def test_corruption_is_rejected_before_live_docker_commands(self):
        backup=self.good_backup(); (backup/'env').write_text('altered')
        self.log.write_text(''); r=self.run_script('restore.sh',backup,'--yes')
        self.assertNotEqual(r.returncode,0); self.assertEqual(self.calls(),[])
    def test_restore_failure_leaves_mutated_services_stopped(self):
        backup=self.good_backup(); r=self.run_script('restore.sh',backup,'--yes',fail='import')
        self.assertNotEqual(r.returncode,0)
        self.assertFalse(json.loads(self.state.read_text())['app'])
        self.assertFalse(json.loads(self.state.read_text())['seaweedfs'])
    def test_media_extraction_failure_keeps_writers_stopped(self):
        backup=self.good_backup(); r=self.run_script('restore.sh',backup,'--yes',fail='extract')
        self.assertNotEqual(r.returncode,0)
        self.assertFalse(json.loads(self.state.read_text())['app'])
        self.assertFalse(json.loads(self.state.read_text())['seaweedfs'])
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_successful_restore_preserves_config_and_stopped_services(self):
        backup=self.good_backup()
        self.state.write_text(json.dumps({'app':False,'seaweedfs':False,'postgres':True}))
        (self.deploy/'.env').write_text('CURRENT_CONFIG=placeholder\n')
        (self.deploy/'docker-compose.yml').write_text('services: {}\n')
        self.log.write_text('')
        r=self.run_script('restore.sh',backup,'--yes'); self.assertEqual(r.returncode,0,r.stderr)
        self.assertFalse(any(c[:2]==['compose','start'] for c in self.calls()))
        self.assertEqual((self.deploy/'.env').read_text(),'CURRENT_CONFIG=placeholder\n')
        self.assertEqual((self.deploy/'docker-compose.yml').read_text(),'services: {}\n')
    def test_failed_restore_storage_resume_does_not_start_app(self):
        backup=self.good_backup(); self.log.write_text('')
        r=self.run_script('restore.sh',backup,'--yes',fail='resume-seaweed')
        self.assertNotEqual(r.returncode,0)
        self.assertFalse(any(c[:2]==['compose','start'] and c[-1]=='app' for c in self.calls()))
        self.assertFalse(json.loads(self.state.read_text())['app'])
    def test_verify_never_uses_compose_and_removes_its_container_on_failure(self):
        backup=self.good_backup(); self.log.write_text('')
        r=self.run_script('restore.sh',backup,'--verify',fail='verify-import')
        self.assertNotEqual(r.returncode,0); calls=self.calls()
        self.assertFalse(any(c[0]=='compose' for c in calls))
        self.assertIn(['rm','-f','-v','isolated-check'],calls)
        create=next(c for c in calls if c[0]=='run'); self.assertIn('none',create); self.assertNotIn('-p',create)
    def test_legacy_requires_explicit_flag_and_rejects_traversal(self):
        backup=self.good_backup()
        for name in ('COMPLETE','manifest','SHA256SUMS','schema.txt'): (backup/name).unlink()
        self.assertNotEqual(self.run_script('restore.sh',backup,'--verify').returncode,0)
        self.assertEqual(self.run_script('restore.sh',backup,'--verify','--allow-legacy').returncode,0)
        with tarfile.open(backup/'seaweedfs.tar.gz','w:gz') as t:
            i=tarfile.TarInfo('../escape'); i.size=1; t.addfile(i,io.BytesIO(b'x'))
        self.log.write_text('')
        self.assertNotEqual(self.run_script('restore.sh',backup,'--yes','--allow-legacy').returncode,0)
        self.assertEqual(self.calls(),[])

if __name__=='__main__': unittest.main()
