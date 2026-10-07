#!/usr/bin/env node
// A checksum receipt is useful for dev builds, but is not a signature,
// release attestation, updater feed, or proof of tests/hardware qualification.
import { createHash } from 'node:crypto';
import { readFileSync, lstatSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const [artifact, destination, target, ...extra] = process.argv.slice(2);
const allowedTargets = new Set(['x86_64-pc-windows-msvc', 'aarch64-pc-windows-msvc',
  'aarch64-apple-darwin', 'x86_64-apple-darwin', 'x86_64-unknown-linux-gnu', 'aarch64-unknown-linux-gnu']);
if (!artifact || !destination || extra.length || !allowedTargets.has(target)) {
  throw new Error('Usage: artifact-manifest.mjs <artifact> <manifest.json> <Rust target>');
}
const path = resolve(artifact);
if (!lstatSync(path).isFile()) throw new Error('Artifact must be a regular file, not a symlink or directory.');
if (path === resolve(destination)) throw new Error('Manifest cannot overwrite the artifact.');
const name = basename(path);
if (!/^[a-zA-Z0-9._-]+$/.test(name)) throw new Error('Artifact basename must contain only portable filename characters.');
const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');
const bytes = readFileSync(path);
const manifest = {
  schema: 'mnema-desktop-development-artifact-v1',
  purpose: 'Unsigned development feasibility probe; not an official release or updater feed.',
  target_supplied_by_packager: target,
  artifact: { name, bytes: bytes.length, sha256: digest(bytes) },
  packaging_inputs: {
    cargo_lock_sha256: digest(readFileSync(join(desktop, 'Cargo.lock'))),
    npm_lock_sha256: digest(readFileSync(join(desktop, 'ui', 'package-lock.json'))),
    license_manifest_sha256: digest(readFileSync(join(desktop, 'licenses', 'manifest.json'))),
  },
  updater_eligible: false,
  trust: 'unverified-development-artifact',
};
writeFileSync(resolve(destination), `${JSON.stringify(manifest, null, 2)}\n`, { flag: 'wx' });
process.stdout.write('Development checksum receipt written; it does not establish publisher trust.\n');
