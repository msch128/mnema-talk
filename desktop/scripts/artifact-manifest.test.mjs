import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, readFileSync, rmSync, symlinkSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';

const script = join(dirname(fileURLToPath(import.meta.url)), 'artifact-manifest.mjs');
const target = 'x86_64-pc-windows-msvc';
const invoke = (...args) => spawnSync(process.execPath, [script, ...args], { encoding: 'utf8' });

test('checksum receipts are deterministic, contain no machine path, and cannot authorize an update', () => {
  const dir = mkdtempSync(join(tmpdir(), 'mnema-dev-receipt-'));
  try {
    const artifact = join(dir, 'probe.exe');
    const bytes = Buffer.from('development-test-artifact');
    writeFileSync(artifact, bytes);
    const first = join(dir, 'first.json');
    const second = join(dir, 'second.json');
    assert.equal(invoke(artifact, first, target).status, 0);
    assert.equal(invoke(artifact, second, target).status, 0);
    const raw = readFileSync(first, 'utf8');
    assert.equal(raw, readFileSync(second, 'utf8'));
    assert.equal(raw.includes(dir), false);
    const parsed = JSON.parse(raw);
    assert.equal(parsed.artifact.sha256, createHash('sha256').update(bytes).digest('hex'));
    assert.equal(parsed.artifact.bytes, bytes.length);
    assert.equal(parsed.updater_eligible, false);
    assert.equal(parsed.trust, 'unverified-development-artifact');
    writeFileSync(artifact, 'changed');
    const changed = join(dir, 'changed.json');
    assert.equal(invoke(artifact, changed, target).status, 0);
    assert.notEqual(JSON.parse(readFileSync(changed, 'utf8')).artifact.sha256, parsed.artifact.sha256);
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test('receipt generation refuses overwrites, symlinks, directories, and unrecognized targets', () => {
  const dir = mkdtempSync(join(tmpdir(), 'mnema-dev-receipt-'));
  try {
    const artifact = join(dir, 'probe.exe');
    const manifest = join(dir, 'manifest.json');
    writeFileSync(artifact, 'probe');
    writeFileSync(manifest, 'must survive');
    assert.notEqual(invoke(artifact, manifest, target).status, 0);
    assert.equal(readFileSync(manifest, 'utf8'), 'must survive');
    assert.notEqual(invoke(artifact, artifact, target).status, 0);
    assert.equal(readFileSync(artifact, 'utf8'), 'probe');
    assert.notEqual(invoke(artifact, join(dir, 'invalid.json'), 'unrecognized-platform').status, 0);
    assert.notEqual(invoke(dir, join(dir, 'directory.json'), target).status, 0);
    // Windows creation of symlinks may require an OS privilege not granted
    // to the development runner; the lstat guard remains identical there.
    if (process.platform !== 'win32') {
      const link = join(dir, 'link.exe');
      symlinkSync(artifact, link);
      assert.notEqual(invoke(link, join(dir, 'link.json'), target).status, 0);
    }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
