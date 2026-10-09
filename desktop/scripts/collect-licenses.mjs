#!/usr/bin/env node
// Collect verbatim dependency license notices. No downloads and no signing.
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { readFileSync, readdirSync, mkdirSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { reviewedGlibPackage } from './checked-glib.mjs';

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const output = join(desktop, 'licenses');
const check = process.argv.includes('--check');
const args = process.argv.slice(2).filter((arg) => arg !== '--check');
if (args.length > 1) throw new Error('Usage: collect-licenses.mjs [metadata.json] [--check]');
const metadata = args.length === 1
  ? JSON.parse(readFileSync(resolve(args[0]), 'utf8'))
  : (() => {
      const cargo = spawnSync('cargo', ['metadata', '--locked', '--features', 'shell', '--format-version', '1'], {
        cwd: desktop, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024,
      });
      if (cargo.error || cargo.status !== 0) throw new Error('Locked cargo metadata failed; fetch the locked crates first.');
      return JSON.parse(cargo.stdout);
    })();
const overrides = JSON.parse(readFileSync(join(output, 'upstream-overrides.json'), 'utf8'));
const hash = (value) => createHash('sha256').update(value).digest('hex');
const textFiles = new Map();
const licenseName = /^(?:(?:licen[cs]e|copying|copyright|notice)(?:[-._ ].*)?|.+[-._ ]licen[cs]e)$/i;
const excludedSource = /\.(?:rs|c|cc|cpp|h|hpp|js|ts|py)$/i;
const compare = (a, b) => a < b ? -1 : a > b ? 1 : 0;

function findNotices(directory, root = directory) {
  const found = [];
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) found.push(...findNotices(path, root));
    else if (entry.isFile() && licenseName.test(entry.name) && !excludedSource.test(entry.name)) {
      found.push({ path, name: relative(root, path).replaceAll('\\', '/') });
    }
  }
  return found.sort((a, b) => compare(a.name, b.name));
}

function storeNotice(bytes, name, source, provenance) {
  const sha256 = hash(bytes);
  textFiles.set(sha256, bytes);
  return { name, sha256, source, provenance };
}

const packages = [];
for (const pkg of metadata.packages.filter((pkg) => pkg.source || pkg.name === 'glib')) {
  const patched = reviewedGlibPackage(pkg);
  if (!patched && pkg.source !== 'registry+https://github.com/rust-lang/crates.io-index') {
    throw new Error(`Unreviewed source for ${pkg.name}`);
  }
  const source = patched?.source ?? `https://crates.io/crates/${pkg.name}/${pkg.version}`;
  const root = dirname(pkg.manifest_path);
  const files = findNotices(root);
  if (pkg.license_file) {
    const path = resolve(root, pkg.license_file);
    if (relative(root, path).startsWith('..')) throw new Error(`License path escapes crate ${pkg.name}`);
    if (!files.some((file) => file.path === path)) files.push({ path, name: pkg.license_file });
  }
  let texts = files.map((file) => storeNotice(readFileSync(file.path), file.name, source, patched ? 'crate-archive+reviewed-upstream-backport' : 'crate-archive'));
  if (texts.length === 0) {
    texts = overrides[`${pkg.name}@${pkg.version}`];
    if (!texts?.length) throw new Error(`Missing license text for ${pkg.name}@${pkg.version}`);
    for (const text of texts) {
      if (!/^[a-f0-9]{64}$/.test(text.sha256)) throw new Error('Invalid override hash');
      const bytes = readFileSync(join(output, 'texts', `${text.sha256}.txt`));
      if (hash(bytes) !== text.sha256) throw new Error(`Changed upstream license for ${pkg.name}`);
      textFiles.set(text.sha256, bytes);
    }
  }
  packages.push({ ecosystem: 'cargo', name: pkg.name, version: pkg.version, license: pkg.license,
    source, ...(patched ? { local_patch: patched.local_patch } : {}), texts });
}

const npmLock = JSON.parse(readFileSync(join(desktop, 'ui', 'package-lock.json'), 'utf8'));
for (const [path, pkg] of Object.entries(npmLock.packages)) {
  // Include the entire production dependency closure, even compiler packages
  // brought in by Vue which need not be present in the final browser bundle.
  if (!path || pkg.dev) continue;
  const root = join(desktop, 'ui', path);
  const actual = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));
  if (actual.version !== pkg.version) throw new Error('npm installation differs from lockfile');
  const source = `https://www.npmjs.com/package/${actual.name}/v/${pkg.version}`;
  // Traverse only this package's own contents, not its nested dependencies.
  const files = findNotices(root).filter((file) => !file.name.startsWith('node_modules/'));
  if (!files.length) throw new Error(`Missing npm license text for ${actual.name}`);
  const texts = files.map((file) => storeNotice(readFileSync(file.path), file.name, source, 'npm-archive'));
  packages.push({ ecosystem: 'npm', name: actual.name, version: pkg.version, license: pkg.license,
    source, texts });
}
packages.sort((a, b) => compare(`${a.ecosystem}:${a.name}@${a.version}`, `${b.ecosystem}:${b.name}@${b.version}`));
const cargoLock = readFileSync(join(desktop, 'Cargo.lock'), 'utf8');
const lockedCrates = cargoLock.split('[[package]]').slice(1).flatMap((section) => {
  if (!/^source = "registry\+https:\/\/github\.com\/rust-lang\/crates\.io-index"$/m.test(section) && !/^name = "glib"$/m.test(section)) return [];
  const name = section.match(/^name = "([^"]+)"$/m)?.[1];
  const version = section.match(/^version = "([^"]+)"$/m)?.[1];
  if (!name || !version) throw new Error('Unexpected Cargo.lock package syntax.');
  return [`${name}@${version}`];
}).sort(compare);
const metadataCrates = packages.filter((pkg) => pkg.ecosystem === 'cargo')
  .map((pkg) => `${pkg.name}@${pkg.version}`).sort(compare);
if (JSON.stringify(lockedCrates) !== JSON.stringify(metadataCrates)) {
  throw new Error('Cargo metadata does not match the current locked crate inventory.');
}
const manifest = {
  schema: 'mnema-desktop-license-bundle-v1',
  scope: 'Conservative locked Rust graph and production npm closure; not a claim that all packages are linked.',
  cargo_lock_sha256: hash(readFileSync(join(desktop, 'Cargo.lock'))),
  npm_lock_sha256: hash(readFileSync(join(desktop, 'ui', 'package-lock.json'))),
  supplemental: JSON.parse(readFileSync(join(output, 'supplemental.json'), 'utf8')),
  packages,
};
for (const notice of Object.values(manifest.supplemental)) {
  if (!/^[a-zA-Z0-9._-]+$/.test(notice.file)
    || hash(readFileSync(join(output, notice.file))) !== notice.sha256) {
    throw new Error('Invalid or modified supplemental runtime license notice.');
  }
  for (const asset of [...(notice.source_assets ?? []), ...(notice.additional_files ?? [])]) {
    if (!/^ui\/src\/assets\/[a-zA-Z0-9_./-]+$/.test(asset.path)
      || asset.path.split('/').includes('..')
      || hash(readFileSync(join(desktop, asset.path))) !== asset.sha256) {
      throw new Error('Vendored asset differs from its reviewed source or attribution.');
    }
  }
}
const pinnedRust = readFileSync(join(desktop, 'rust-toolchain.toml'), 'utf8')
  .match(/^channel\s*=\s*"([^"]+)"$/m)?.[1];
if (manifest.supplemental.rust_standard_library.version !== pinnedRust) {
  throw new Error('Rust standard library notices differ from the pinned toolchain.');
}
const manifestBytes = `${JSON.stringify(manifest, null, 2)}\n`;
if (check) {
  if (readFileSync(join(output, 'manifest.json'), 'utf8') !== manifestBytes) {
    throw new Error('License manifest is stale; regenerate and review it.');
  }
  for (const [sha, bytes] of textFiles) {
    const path = join(output, 'texts', `${sha}.txt`);
    if (!existsSync(path) || !readFileSync(path).equals(bytes)) throw new Error('Missing or modified license text.');
  }
  const actualTextNames = readdirSync(join(output, 'texts')).sort(compare);
  const expectedTextNames = [...textFiles.keys()].map((sha) => `${sha}.txt`).sort(compare);
  if (JSON.stringify(actualTextNames) !== JSON.stringify(expectedTextNames)) {
    throw new Error('Unreferenced files in license text directory; review and remove stale texts.');
  }
} else {
  mkdirSync(join(output, 'texts'), { recursive: true });
  for (const [sha, bytes] of textFiles) writeFileSync(join(output, 'texts', `${sha}.txt`), bytes);
  writeFileSync(join(output, 'manifest.json'), manifestBytes);
}
process.stdout.write(`License bundle ${check ? 'verified' : 'generated'}: ${packages.length} packages, ${textFiles.size} verbatim texts.\n`);
