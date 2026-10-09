// The reviewed GTK3-compatible backport retains its real upstream version.
// RustSec skips path packages: scan a temporary registry-identity lock as well.
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { lstatSync, readFileSync, readdirSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const GLIB_VERSION = '0.18.5';
export const GLIB_ARCHIVE_SHA256 = '233daaf6e83ae6a12a52055f568f9d7cf4671dabb78ff9560ab6da230ce00ee5';
const manifestSha256 = '7d039c58a6e0f95f4d882a88f397b1615666c513271edbce0f2ed610d36e3dde';
const registry = 'registry+https://github.com/rust-lang/crates.io-index';
const hash = bytes => createHash('sha256').update(bytes).digest('hex');
export const GLIB_ROOTS = [
  ['Cargo.toml', 'experimental/vendor/glib'],
  ['experimental/app/Cargo.toml', '../vendor/glib'],
  ['experimental/crates/trust-dialog/Cargo.toml', '../../vendor/glib'],
];
// Include independent native/research roots and retained upstream workspaces.
export const OTHER_DESKTOP_LOCKS = [
  'experimental/crates/broker/Cargo.lock',
  'experimental/crates/broker/research-vault-facade/Cargo.lock',
  'experimental/crates/crypto-owner/Cargo.lock',
  'experimental/crates/crypto-owner/qualified-sdk/Cargo.lock',
  'experimental/crates/crypto-seeds/Cargo.lock',
  'experimental/crates/media/rust-actor/Cargo.lock',
  'experimental/crates/media/rust/Cargo.lock',
  'experimental/vendor/openmls/Cargo.lock',
  'experimental/vendor/openmls/compat_tests/Cargo.lock',
];

export function verifyGlibSource(base = desktop) {
  const vendor = join(base, 'experimental/vendor/glib');
  const bytes = readFileSync(join(base, 'experimental/vendor/glib-source.json'));
  if (hash(bytes) !== manifestSha256) throw new Error('GLib provenance differs from the reviewed archive/backport');
  const manifest = JSON.parse(bytes);
  const actual = [];
  function walk(dir, prefix = '') {
    if (!lstatSync(dir).isDirectory() || lstatSync(dir).isSymbolicLink()) throw new Error('Non-directory GLib source');
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const name = prefix + entry.name;
      const path = join(dir, entry.name);
      if (entry.isDirectory()) walk(path, name + '/');
      else if (entry.isFile()) {
        actual.push(name);
        if (hash(readFileSync(path)) !== manifest.files[name]) throw new Error('Unreviewed GLib source bytes');
      } else throw new Error('Non-file GLib source');
    }
  }
  walk(vendor);
  if (JSON.stringify(actual.sort()) !== JSON.stringify(Object.keys(manifest.files).sort())) throw new Error('Incomplete GLib source inventory');
  return manifest;
}

function glibSection(lock) {
  const sections = lock.split('[[package]]');
  const matches = sections.filter(s => /^name = "glib"$/m.test(s));
  if (matches.length !== 1 || !/^version = "0.18.5"$/m.test(matches[0]) || /^source = /m.test(matches[0]) || /^checksum = /m.test(matches[0])) throw new Error('GLib lock must resolve the single reviewed path package');
  return matches[0];
}

export function verifyGlibRoots(base = desktop) {
  verifyGlibSource(base);
  for (const [manifest, path] of GLIB_ROOTS) {
    const text = readFileSync(join(base, manifest), 'utf8');
    const section = text.match(/^\[patch\.crates-io\]\s*\n([\s\S]*?)(?=^\[|$(?![\s\S]))/m)?.[1];
    if (!section || !section.split('\n').some(line => line.trim() === `glib = { path = "${path}" }`)) throw new Error('Cargo root lacks the canonical GLib backport');
    glibSection(readFileSync(join(base, dirname(manifest), 'Cargo.lock'), 'utf8'));
  }
}

// Explicit local-dependency license inventory; never silently omit this crate.
export function reviewedGlibPackage(pkg) {
  if (pkg.name !== 'glib') return null;
  verifyGlibRoots();
  if (pkg.source !== null || pkg.version !== GLIB_VERSION || resolve(pkg.manifest_path) !== join(desktop, 'experimental/vendor/glib/Cargo.toml')) throw new Error('Cargo metadata resolved an unreviewed GLib package');
  return { source: `https://crates.io/crates/glib/${GLIB_VERSION}`,
    local_patch: 'https://github.com/gtk-rs/gtk-rs-core/commit/b5a4071e439bef2b5eea76c3aa25e5ae84839e34' };
}

export function registryIdentityLock(lock) {
  const section = glibSection(lock);
  const replacement = section.replace(/^version = "0.18.5"$/m,
    `version = "0.18.5"\nsource = "${registry}"\nchecksum = "${GLIB_ARCHIVE_SHA256}"`);
  return lock.replace(section, replacement);
}

export function auditGlibRegistryIdentities() {
  verifyGlibRoots();
  const dir = mkdtempSync(join(tmpdir(), 'mnema-glib-advisory-'));
  try {
    for (const [manifest] of GLIB_ROOTS) {
      const source = readFileSync(join(desktop, dirname(manifest), 'Cargo.lock'), 'utf8');
      const lock = join(dir, 'Cargo.lock');
      writeFileSync(lock, registryIdentityLock(source));
      console.log(`Auditing original GLib registry identity: desktop/${manifest}`);
      const result = spawnSync('cargo', ['audit', '--file', lock], { stdio: 'inherit' });
      if (result.error || result.status !== 0) throw new Error('Registry-identity advisory scan failed');
    }
    for (const path of OTHER_DESKTOP_LOCKS) {
      console.log(`Auditing independent desktop lock: desktop/${path}`);
      const result = spawnSync('cargo', ['audit', '--file', join(desktop, path)], { stdio: 'inherit' });
      if (result.error || result.status !== 0) throw new Error('Desktop dependency advisory scan failed');
    }
    console.log('The upstream 0.18.5 advisory remains reported; the exact local backport is verified separately.');
  } finally { rmSync(dir, { recursive: true, force: true }); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.argv.slice(2).some(arg => arg !== '--audit')) throw new Error('Usage: checked-glib.mjs [--audit]');
  verifyGlibRoots();
  if (process.argv.includes('--audit')) auditGlibRegistryIdentities();
  console.log('Reviewed GLib source and all three Cargo roots verified.');
}
