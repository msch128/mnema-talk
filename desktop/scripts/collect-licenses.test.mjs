import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, copyFileSync, rmSync, unlinkSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';

const original = join(dirname(fileURLToPath(import.meta.url)), 'collect-licenses.mjs');
const source = 'registry+https://github.com/rust-lang/crates.io-index';

function fixture() {
  const root = mkdtempSync(join(tmpdir(), 'mnema-license-check-'));
  const desktop = join(root, 'desktop');
  for (const dir of ['scripts', 'licenses', 'ui/node_modules/example', 'crate']) {
    mkdirSync(join(desktop, dir), { recursive: true });
  }
  copyFileSync(original, join(desktop, 'scripts', 'collect-licenses.mjs'));
  const put = (path, value) => writeFileSync(join(desktop, path), typeof value === 'string' ? value : JSON.stringify(value));
  put('Cargo.lock', `version = 4\n[[package]]\nname = "example"\nversion = "1.0.0"\nsource = "${source}"\n`);
  put('rust-toolchain.toml', 'channel = "1.99.0"\n');
  put('licenses/upstream-overrides.json', {});
  put('licenses/RUST-COPYRIGHT-library.html', 'fixture runtime notice');
  put('licenses/supplemental.json', { rust_standard_library: {
    version: '1.99.0', file: 'RUST-COPYRIGHT-library.html',
    sha256: createHash('sha256').update('fixture runtime notice').digest('hex'),
  } });
  put('crate/LICENSE', 'Fixture crate permission and copyright notice.');
  put('crate/Cargo.toml', '[package]\nname="example"\nversion="1.0.0"\n');
  put('metadata.json', { packages: [{ name: 'example', version: '1.0.0', source,
    license: 'MIT', manifest_path: join(desktop, 'crate', 'Cargo.toml') }] });
  put('ui/package-lock.json', { packages: {
    '': { name: 'probe', version: '0.0.0' },
    'node_modules/example': { version: '1.0.0', license: 'MIT' },
  } });
  put('ui/node_modules/example/package.json', { name: 'example', version: '1.0.0' });
  put('ui/node_modules/example/LICENSE', 'Fixture npm permission and copyright notice.');
  const run = (...args) => spawnSync(process.execPath, [join(desktop, 'scripts', 'collect-licenses.mjs'),
    join(desktop, 'metadata.json'), ...args], { encoding: 'utf8' });
  return { root, desktop, put, run, close: () => rmSync(root, { recursive: true, force: true }) };
}

test('license collection is reproducible and omits local paths', () => {
  const f = fixture();
  try {
    assert.equal(f.run().status, 0);
    const manifest = readFileSync(join(f.desktop, 'licenses', 'manifest.json'), 'utf8');
    assert.equal(manifest.includes(f.root), false);
    assert.equal(JSON.parse(manifest).packages.length, 2);
    assert.equal(f.run('--check').status, 0);
    assert.equal(f.run().status, 0);
    assert.equal(readFileSync(join(f.desktop, 'licenses', 'manifest.json'), 'utf8'), manifest);
    f.put('crate/LICENSE', 'changed upstream text');
    assert.notEqual(f.run('--check').status, 0);
  } finally { f.close(); }
});

test('missing license text and stale metadata fail instead of yielding an incomplete inventory', () => {
  const f = fixture();
  try {
    unlinkSync(join(f.desktop, 'crate', 'LICENSE'));
    assert.notEqual(f.run().status, 0);
    f.put('crate/LICENSE', 'Fixture crate notice.');
    f.put('Cargo.lock', `version = 4\n[[package]]\nname = "example"\nversion = "2.0.0"\nsource = "${source}"\n`);
    assert.notEqual(f.run().status, 0);
  } finally { f.close(); }
});

test('runtime notice integrity and installed npm versions are checked', () => {
  const f = fixture();
  try {
    f.put('licenses/RUST-COPYRIGHT-library.html', 'tampered runtime notice');
    assert.notEqual(f.run().status, 0);
    f.put('licenses/RUST-COPYRIGHT-library.html', 'fixture runtime notice');
    f.put('ui/node_modules/example/package.json', { name: 'example', version: '2.0.0' });
    assert.notEqual(f.run().status, 0);
  } finally { f.close(); }
});

test('vendored fonts must match the reviewed original bytes and attribution', () => {
  const f = fixture();
  try {
    mkdirSync(join(f.desktop, 'ui', 'src', 'assets'), { recursive: true });
    f.put('ui/src/assets/font.woff2', 'font fixture');
    const supplemental = JSON.parse(readFileSync(join(f.desktop, 'licenses', 'supplemental.json'), 'utf8'));
    supplemental.rust_standard_library.source_assets = [{
      path: 'ui/src/assets/font.woff2', sha256: createHash('sha256').update('font fixture').digest('hex'),
    }];
    f.put('licenses/supplemental.json', supplemental);
    assert.equal(f.run().status, 0);
    f.put('ui/src/assets/font.woff2', 'replaced font fixture');
    assert.notEqual(f.run('--check').status, 0);
  } finally { f.close(); }
});
