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
def note(*words):
 with open(os.environ['FAKE_LOG'],'a') as f: f.write(json.dumps(list(words))+'\n')
if args[:2]==['compose','pause']:
 if os.environ.get('FAIL_AT')=='pause': done(1)
 state['paused']=True; done()
if args[:2]==['compose','unpause']: state['paused']=False; done()
if args[:2]==['compose','exec']:
 text=args[args.index('-c')+1]; extra=args[args.index('-c')+2:]
 if '-AtqX' in text and not extra:
  # The online backup's session: answers until its input ends, like psql.
  for line in sys.stdin:
   if 'pg_advisory_lock' in line:
    if os.environ.get('FAIL_AT')=='lock': sys.exit(1)
    note('session','locked'); print('locked',flush=True)
   if 'pg_export_snapshot' in line: note('session','snapshot'); print('00000003-0000001B-1',flush=True)
  note('session','closed'); sys.exit(0)
 if 'SET TRANSACTION SNAPSHOT' in ' '.join(extra): print('mnema-media/uploads/fixture.bin'); done()
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
if args[0]=='run' and any(a.split('@')[0]=='postgres:18-alpine' for a in args): print('isolated-check'); done()
if args[0]=='run' and '--entrypoint' in args: print('media-check'); done()
if args[0]=='image': done(1 if os.environ.get('FAIL_AT')=='no-image' else 0)
if args[0]=='exec' and 'media-check' in args:
 if '-i' in args: sys.stdin.buffer.read()
 done(1 if os.environ.get('FAIL_AT')=='missing-object' and 'wget' in args[-1] else 0)
if args[0]=='run': done(1 if os.environ.get('FAIL_AT')=='extract' else 0)
if args[0]=='exec':
 if 'psql' in args: sys.stdin.read()
 done(1 if os.environ.get('FAIL_AT')=='verify-import' and 'psql' in args else 0)
if args[0]=='rm': done()
done(99)
'''

# Stands in for `age`: the output is unreadable without the identity file.
FAKE_AGE = r'''#!/usr/bin/env python3
import sys
from pathlib import Path
args=sys.argv[1:]; flip=lambda b: bytes(x^0x5a for x in b)
if '-d' in args:
 if Path(args[args.index('-i')+1]).read_text()!='fixture-identity\n': sys.exit(1)
 data=Path(args[-1]).read_bytes()
 if not data.startswith(b'FAKE-AGE\n'): sys.exit(1)
 sys.stdout.buffer.write(flip(data[9:])); sys.exit(0)
recipients=[args[i+1] for i,a in enumerate(args) if a=='-r']
Path(args[args.index('-o')+1]).write_bytes(b'FAKE-AGE\n'+flip(sys.stdin.buffer.read()))
Path(args[args.index('-o')+1]).with_name('recipients').write_text(' '.join(recipients))
'''

class Fixture(unittest.TestCase):
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
    def run_script(self, name, *args, fail='', **extra):
        return subprocess.run(['bash',str(ROOT/'scripts'/name),*map(str,args)], cwd=self.deploy, env=dict(self.env,FAIL_AT=fail,**extra), capture_output=True, text=True, timeout=15)
    def status(self): return json.loads((self.deploy/'backup-status'/'status.json').read_text())
    def calls(self): return [json.loads(x) for x in self.log.read_text().splitlines()]
    def good_backup(self, **extra):
        r=self.run_script('backup.sh',**extra); self.assertEqual(r.returncode,0,r.stderr)
        return next(p for p in self.backups.iterdir() if p.is_dir())
    def index(self, match): return next(i for i,c in enumerate(self.calls()) if match(c))

class MaintenanceTests(Fixture):
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
    def test_backup_reports_its_time_size_and_passed_restore_test(self):
        backup=self.good_backup(); st=self.status(); calls=self.calls()
        created=next(l.split('=',1)[1] for l in (backup/'manifest').read_text().splitlines() if l.startswith('created_utc='))
        self.assertEqual(st['format'],1); self.assertEqual(st['last_backup_at'],created)
        self.assertEqual(st['size_bytes'],sum(p.stat().st_size for p in backup.iterdir()))
        self.assertEqual(st['restore_test']['result'],'passed'); self.assertRegex(st['restore_test']['at'],r'^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$')
        self.assertEqual((self.deploy/'backup-status'/'status.json').stat().st_mode&0o777,0o644)
        self.assertEqual((self.deploy/'backup-status').stat().st_mode&0o777,0o755)
        self.assertEqual(list((self.deploy/'backup-status').iterdir()),[self.deploy/'backup-status'/'status.json'])
        # The test runs after the app is back, on a throwaway container only.
        start=max(i for i,c in enumerate(calls) if c[:2]==['compose','start'] and c[-1]=='app')
        verify=next(i for i,c in enumerate(calls) if c[0]=='run' and any(a.startswith('postgres:18-alpine') for a in c))
        self.assertLess(start,verify)
        self.assertFalse(any(c[:2]==['compose','exec'] for c in calls[verify:]))
    def test_failed_restore_test_is_reported_and_keeps_older_backups(self):
        previous=self.good_backup(); old=self.backups/'20200101-000000'; previous.rename(old); os.utime(old,(0,0))
        r=self.run_script('backup.sh',fail='verify-import')
        self.assertNotEqual(r.returncode,0); self.assertIn('restore test FAILED',r.stderr)
        self.assertNotIn('incomplete directories',r.stderr)
        self.assertEqual(self.status()['restore_test']['result'],'failed')
        self.assertTrue(old.exists())
        self.assertTrue(all((p/'COMPLETE').exists() for p in self.backups.iterdir()))
        self.assertTrue(json.loads(self.state.read_text())['app'])
    def test_restore_test_can_be_skipped_and_failed_backups_keep_the_last_status(self):
        r=self.run_script('backup.sh',RESTORE_TEST='0'); self.assertEqual(r.returncode,0,r.stderr)
        self.assertEqual(self.status()['restore_test'],{'result':'skipped'})
        self.assertFalse(any(c[0]=='run' and any(a.startswith('postgres:18-alpine') for a in c) for c in self.calls()))
        before=(self.deploy/'backup-status'/'status.json').read_text()
        self.assertNotEqual(self.run_script('backup.sh',fail='dump').returncode,0)
        self.assertEqual((self.deploy/'backup-status'/'status.json').read_text(),before)
        self.assertNotEqual(self.run_script('backup.sh',RESTORE_TEST='yes').returncode,0)
    @unittest.skipIf(os.geteuid()==0,'root writes everywhere')
    def test_root_owned_status_directory_is_written_through_a_container(self):
        folder=self.deploy/'backup-status'; folder.mkdir(); folder.chmod(0o555); self.addCleanup(folder.chmod,0o755)
        r=self.run_script('backup.sh'); self.assertEqual(r.returncode,0,r.stderr)
        write=[c for c in self.calls() if c[0]=='run' and '-i' in c]
        self.assertEqual(len(write),1); self.assertIn(f'{folder.resolve()}:/out',write[0]); self.assertIn('none',write[0])
        self.assertIn('/out/status.json',write[0][-1])
    def test_unwritable_status_directory_warns_but_keeps_the_backup(self):
        (self.deploy/'backup-status').write_text('not a directory')
        r=self.run_script('backup.sh')
        self.assertNotEqual(r.returncode,0); self.assertIn('could not write',r.stderr)
        self.assertTrue(any((p/'COMPLETE').exists() for p in self.backups.iterdir()))
        self.assertTrue(json.loads(self.state.read_text())['app'])

class OnlineBackupTests(Fixture):
    def online(self, **extra): return self.good_backup(BACKUP_MODE='online',**extra)
    def test_cold_stays_the_default_and_its_layout_is_unchanged(self):
        backup=self.good_backup(); calls=self.calls()
        self.assertEqual(sorted(p.name for p in backup.iterdir()),['COMPLETE','SHA256SUMS','compose.yaml','env','manifest','postgres.sql.gz','schema.txt','seaweedfs.tar.gz'])
        self.assertEqual([l.split('=')[0] for l in (backup/'manifest').read_text().splitlines()],['format','created_utc','app_image_id','postgres_image_id','seaweedfs_image_id'])
        self.assertFalse(any(c[:2] in (['compose','pause'],['compose','unpause']) or c[0]=='session' for c in calls))
        self.assertNotEqual(self.run_script('backup.sh',BACKUP_MODE='hot').returncode,0)
    def test_the_app_keeps_running_and_storage_is_frozen_from_snapshot_to_copy(self):
        backup=self.online(); calls=self.calls()
        self.assertFalse(any(c[:2] in (['compose','stop'],['compose','start']) for c in calls))
        order=[self.index(lambda c:c==['session','locked']), self.index(lambda c:c[:2]==['compose','pause']),
               self.index(lambda c:c==['session','snapshot']), self.index(lambda c:c[0]=='run' and '/out/seaweedfs.tar.gz' in ' '.join(c)),
               self.index(lambda c:c[:2]==['compose','unpause']), self.index(lambda c:'pg_dump' in ' '.join(c)),
               self.index(lambda c:c==['session','closed'])]
        self.assertEqual(order,sorted(order))
        # The dump and the object list read the snapshot taken while storage was frozen.
        self.assertEqual(calls[order[5]][-1],'00000003-0000001B-1')
        self.assertIn("SET TRANSACTION SNAPSHOT '00000003-0000001B-1'",next(c for c in calls if 's3_key' in ' '.join(c))[-1])
        state=json.loads(self.state.read_text())
        self.assertTrue(state['app']); self.assertTrue(state['seaweedfs']); self.assertFalse(state['paused'])
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_an_online_backup_adds_to_the_layout_and_keeps_every_v1_file(self):
        backup=self.online(); manifest=(backup/'manifest').read_text().splitlines()
        self.assertEqual(sorted(p.name for p in backup.iterdir()),['COMPLETE','SHA256SUMS','compose.yaml','env','manifest','media-keys.txt','postgres.sql.gz','schema.txt','seaweedfs.tar.gz'])
        self.assertEqual(manifest[0],'format=mnema-backup-v1'); self.assertIn('mode=online',manifest)
        self.assertEqual((backup/'media-keys.txt').read_text(),'mnema-media/uploads/fixture.bin\n')
        # The checksum list is the v1 one: a restore.sh from before online mode accepts it.
        self.assertEqual([l.split()[-1] for l in (backup/'SHA256SUMS').read_text().splitlines()],['postgres.sql.gz','seaweedfs.tar.gz','env','manifest','schema.txt','compose.yaml'])
        self.assertEqual(self.run_script('restore.sh',backup,'--yes').returncode,0)
    def test_a_server_job_that_keeps_the_lock_stops_the_backup_before_anything_is_frozen(self):
        r=self.run_script('backup.sh',fail='lock',BACKUP_MODE='online',BACKUP_LOCK_WAIT='1')
        self.assertNotEqual(r.returncode,0); self.assertIn('no maintenance lock',r.stderr)
        self.assertFalse(any(c[:2]==['compose','pause'] or 'pg_dump' in ' '.join(c) for c in self.calls()))
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_a_failed_online_backup_unfreezes_storage_and_is_no_restore_point(self):
        for failure in ('pause','archive','dump'):
            self.log.write_text('')
            r=self.run_script('backup.sh',fail=failure,BACKUP_MODE='online'); self.assertNotEqual(r.returncode,0,failure)
            state=json.loads(self.state.read_text())
            self.assertFalse(state.get('paused',False),failure); self.assertTrue(state['app'],failure)
            self.assertIn(['session','closed'],self.calls(),failure)
            self.assertFalse(any((p/'COMPLETE').exists() for p in self.backups.iterdir()),failure)
    def test_the_restore_test_opens_every_object_the_snapshot_refers_to(self):
        backup=self.online(); calls=self.calls()
        check=next(c for c in calls if c[0]=='run' and '--entrypoint' in c)
        self.assertIn('none',check); self.assertNotIn('-p',check); self.assertIn('sha256:fixture',check)
        self.assertIn(['rm','-f','-v','media-check'],calls)
        self.assertEqual(self.status()['restore_test']['result'],'passed')
        r=self.run_script('restore.sh',backup,'--verify',fail='missing-object')
        self.assertNotEqual(r.returncode,0); self.assertIn('lacks objects',r.stderr)
        self.assertEqual(self.calls()[-1],['rm','-f','-v','media-check'])
    def test_a_missing_object_fails_the_backup_run_and_keeps_older_backups(self):
        previous=self.good_backup(); old=self.backups/'20200101-000000'; previous.rename(old); os.utime(old,(0,0))
        r=self.run_script('backup.sh',fail='missing-object',BACKUP_MODE='online')
        self.assertNotEqual(r.returncode,0); self.assertIn('restore test FAILED',r.stderr)
        self.assertEqual(self.status()['restore_test']['result'],'failed'); self.assertTrue(old.exists())
    def test_a_changed_object_list_is_rejected_before_any_docker_command(self):
        backup=self.online(); (backup/'media-keys.txt').write_text(''); self.log.write_text('')
        for mode in ('--verify','--yes'):
            self.assertNotEqual(self.run_script('restore.sh',backup,mode).returncode,0)
        (backup/'media-keys.txt').unlink()
        self.assertNotEqual(self.run_script('restore.sh',backup,'--yes').returncode,0)
        self.assertEqual(self.calls(),[])

class EncryptedBackupTests(Fixture):
    RECIPIENT='age1fixturerecipient'
    def setUp(self):
        super().setUp()
        age=self.bin/'age'; age.write_text(FAKE_AGE); age.chmod(0o755)
        self.identity=self.root/'identity'; self.identity.write_text('fixture-identity\n')
    def encrypted(self, **extra):
        r=self.run_script('backup.sh',BACKUP_AGE_RECIPIENT=self.RECIPIENT,**extra); self.assertEqual(r.returncode,0,r.stderr)
        return next(p for p in self.backups.iterdir() if p.name.endswith('.tar.age'))
    def test_without_a_recipient_nothing_is_encrypted(self):
        (self.bin/'age').write_text('#!/bin/sh\nexit 99\n')
        backup=self.good_backup(BACKUP_AGE_RECIPIENT='  ')
        self.assertEqual((backup/'env').read_text(),'DUMMY_ONLY=placeholder\n')
    def test_a_recipient_leaves_one_unreadable_file_and_no_plain_copy(self):
        file=self.encrypted()
        self.assertEqual([p.name for p in self.backups.iterdir() if p.name!='recipients'],[file.name])
        self.assertNotIn(b'DUMMY_ONLY',file.read_bytes()); self.assertEqual(file.stat().st_mode&0o777,0o600)
        self.assertEqual((self.backups/'recipients').read_text(),self.RECIPIENT)
        # The restore test ran on the plain files first; the status reports what is stored.
        self.assertEqual(self.status()['restore_test']['result'],'passed'); self.assertEqual(self.status()['size_bytes'],file.stat().st_size)
    def test_an_encrypted_backup_restores_with_the_identity_and_leaves_no_plain_copy(self):
        file=self.encrypted(BACKUP_MODE='online')
        for mode in ('--verify','--yes'):
            r=self.run_script('restore.sh',file,mode,BACKUP_AGE_IDENTITY=str(self.identity)); self.assertEqual(r.returncode,0,r.stderr)
            self.assertEqual([p.name for p in self.backups.iterdir() if p.name!='recipients'],[file.name])
        self.assertTrue(any('wget' in c[-1] for c in self.calls() if c[0]=='exec'))
        self.assertFalse((self.deploy/'.mnema-maintenance.lock').exists())
    def test_without_the_right_identity_nothing_is_touched(self):
        file=self.encrypted(); wrong=self.root/'wrong'; wrong.write_text('other\n'); self.log.write_text('')
        for env in ({},{'BACKUP_AGE_IDENTITY':str(wrong)}):
            self.assertNotEqual(self.run_script('restore.sh',file,'--yes',**env).returncode,0)
        self.assertEqual(self.calls(),[]); self.assertEqual([p.name for p in self.backups.iterdir() if p.name!='recipients'],[file.name])
    def test_an_encrypted_file_with_unsafe_paths_is_rejected(self):
        raw=io.BytesIO()
        with tarfile.open(fileobj=raw,mode='w') as t:
            i=tarfile.TarInfo('../escape'); i.size=1; t.addfile(i,io.BytesIO(b'x'))
        self.backups.mkdir(); file=self.backups/'20200101-000000-x.tar.age'
        file.write_bytes(b'FAKE-AGE\n'+bytes(x^0x5a for x in raw.getvalue()))
        r=self.run_script('restore.sh',file,'--yes',BACKUP_AGE_IDENTITY=str(self.identity))
        self.assertNotEqual(r.returncode,0); self.assertFalse(self.log.exists()); self.assertFalse((self.root/'escape').exists())
    def test_a_malformed_recipient_or_a_failed_encryption_keeps_what_exists(self):
        r=self.run_script('backup.sh',BACKUP_AGE_RECIPIENT='not-a-key'); self.assertNotEqual(r.returncode,0); self.assertFalse(self.log.exists())
        (self.bin/'age').write_text('#!/bin/sh\ncat >/dev/null\nexit 1\n')
        r=self.run_script('backup.sh',BACKUP_AGE_RECIPIENT=self.RECIPIENT); self.assertNotEqual(r.returncode,0)
        plain=[p for p in self.backups.iterdir() if p.is_dir()]
        self.assertEqual(len(plain),1); self.assertTrue((plain[0]/'COMPLETE').exists())
        self.assertFalse(any(p.name.endswith(('.age','.partial')) for p in self.backups.iterdir()))
    def test_retention_removes_old_encrypted_backups_after_a_success(self):
        old=self.encrypted().rename(self.backups/'20200101-000000-old.tar.age'); os.utime(old,(0,0))
        other=self.backups/'notes.tar.age'; other.write_text('not ours'); os.utime(other,(0,0))
        self.assertNotEqual(self.run_script('backup.sh',fail='dump',BACKUP_AGE_RECIPIENT=self.RECIPIENT).returncode,0)
        self.assertTrue(old.exists())
        self.encrypted()
        self.assertFalse(old.exists()); self.assertTrue(other.exists())

if __name__=='__main__': unittest.main()
