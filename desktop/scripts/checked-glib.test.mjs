import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cpSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, symlinkSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { verifyGlibSource, verifyGlibRoots, registryIdentityLock, GLIB_ROOTS, OTHER_DESKTOP_LOCKS } from './checked-glib.mjs';

const desktop = join(dirname(fileURLToPath(import.meta.url)), '..');
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'mnema-glib-integrity-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'experimental/vendor'), { recursive: true });
  cpSync(join(desktop, 'experimental/vendor/glib'), join(root, 'experimental/vendor/glib'), { recursive: true });
  cpSync(join(desktop, 'experimental/vendor/glib-source.json'), join(root, 'experimental/vendor/glib-source.json'));
  for (const [path] of GLIB_ROOTS) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    cpSync(join(desktop, path), join(root, path));
    cpSync(join(desktop, dirname(path), 'Cargo.lock'), join(root, dirname(path), 'Cargo.lock'));
  }
  return root;
}

test('the complete official archive with only the reviewed backport passes', () => {
  assert.equal(verifyGlibSource().version, '0.18.5');
  verifyGlibRoots();
});
test('the advisory gate covers every tracked desktop Cargo lock', () => {
  const result = spawnSync('git', ['ls-files', 'Cargo.lock', '**/Cargo.lock'], { cwd: desktop, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  const audited = [...GLIB_ROOTS.map(([manifest]) => join(dirname(manifest), 'Cargo.lock').replaceAll('\\', '/')), ...OTHER_DESKTOP_LOCKS];
  assert.equal(new Set(audited).size, audited.length);
  assert.deepEqual(audited.sort(), result.stdout.trim().split('\n').sort());
});
test('reintroducing the upstream undefined behavior fails', t => {
  const root = fixture(t), path = join(root, 'experimental/vendor/glib/src/variant_iter.rs');
  writeFileSync(path, readFileSync(path, 'utf8').replace('let mut p: *mut libc::c_char', 'let p: *mut libc::c_char').replace('&mut p,', '&p,'));
  assert.throws(() => verifyGlibSource(root), /Unreviewed GLib source bytes/);
});
test('rewriting the manifest cannot bless changed source', t => {
  const root = fixture(t), source = join(root, 'experimental/vendor/glib/src/lib.rs');
  writeFileSync(source, 'unreviewed code');
  const path = join(root, 'experimental/vendor/glib-source.json'), manifest = JSON.parse(readFileSync(path));
  manifest.files['src/lib.rs'] = createHash('sha256').update('unreviewed code').digest('hex');
  writeFileSync(path, JSON.stringify(manifest));
  assert.throws(() => verifyGlibSource(root), /provenance differs/);
});
test('missing and extra source files are rejected', t => {
  const root = fixture(t), source = join(root, 'experimental/vendor/glib');
  rmSync(join(source, 'LICENSE'));
  assert.throws(() => verifyGlibSource(root), /Incomplete GLib source inventory/);
  writeFileSync(join(source, 'unreviewed.rs'), 'new source');
  assert.throws(() => verifyGlibSource(root), /Unreviewed GLib source bytes/);
});
test('source symlinks are rejected', { skip: process.platform === 'win32' }, t => {
  const root = fixture(t), source = join(root, 'experimental/vendor/glib/LICENSE');
  rmSync(source); symlinkSync(join(desktop, 'experimental/vendor/glib/LICENSE'), source);
  assert.throws(() => verifyGlibSource(root), /Non-file GLib source/);
});
test('each Cargo root must resolve the shared backport, not registry bytes', t => {
  for (const [path] of GLIB_ROOTS) {
    const root = fixture(t), lock = join(root, dirname(path), 'Cargo.lock');
    writeFileSync(lock, registryIdentityLock(readFileSync(lock, 'utf8')));
    assert.throws(() => verifyGlibRoots(root), /single reviewed path package/);
  }
});
test('advisory locks restore original registry identity without changing the real lock', () => {
  const lock = readFileSync(join(desktop, 'Cargo.lock'), 'utf8');
  const result = registryIdentityLock(lock);
  assert.match(result, /name = "glib"\nversion = "0.18.5"\nsource = "registry\+https:\/\/github.com\/rust-lang\/crates.io-index"\nchecksum = "233daaf6e83ae6a12a52055f568f9d7cf4671dabb78ff9560ab6da230ce00ee5"/);
  assert.equal(result.replace(/\nsource = "registry\+https:\/\/github.com\/rust-lang\/crates.io-index"\nchecksum = "233daaf6e83ae6a12a52055f568f9d7cf4671dabb78ff9560ab6da230ce00ee5"/, ''), lock);
  assert.equal(readFileSync(join(desktop, 'Cargo.lock'), 'utf8'), lock);
  assert.throws(() => registryIdentityLock(result), /single reviewed path package/);
});
