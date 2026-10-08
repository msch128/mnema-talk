#!/usr/bin/env node
// Gitleaks allowances apply only to these byte-identical public known-answer vectors.
// Changes require an explicit provenance/security review and new reviewed hashes.
import { createHash } from 'node:crypto';
import { readFileSync, lstatSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
const expected = Object.fromEntries([
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/crypto-basics.json",
    "e42d1ad35b6b863ae5f23093515e34abc24d8008c55336fd978c35fbcf7f9572"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/kat_encryption_openmls.json",
    "ed66289b48617ade8ad6459abe05006c438d3b2a06c4d58550a95f3d2eaa9797"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/kat_tree_kem_openmls.json",
    "ee133cf4a4e99f42397dea137ff5eadf6f77afd4e2752c1b909253fc55c8f3dd"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/key-schedule.json",
    "05aa9a68bd2538ace72d8c53375984cc728ef62220ebf314df675708546d97a7"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/message-protection.json",
    "1d29f6eca12a219f459d9c889488d39694aefc70828fba6be7bface176a81787"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/messages.json",
    "b194abe1561995223482dbad51c180146920dc2f637e74d01e07a388308791fb"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/passive-client-handling-commit.json",
    "b24949fdf857bf79d511f57504a8bdc7ce3247eb199684745e439c11b1a1da21"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/passive-client-random.json",
    "0095d863e9d316872e237fc708debcd20ba2687ee905a3e831f97dd3e578d3c1"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/passive-client-welcome.json",
    "92ebb04b67b1aca4290965ae363650921842ceebe275bd79a2733414078d849c"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/psk_secret.json",
    "193d87f4db7b2b8a19fe9f38a68daf3f27ec397825cf658318ccd1790e041e07"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/secret-tree.json",
    "08f92e6272452e2c832e32d38e16cf0c4aa28967e47d3842c60bc354c6b67a94"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/targeted-messages.json",
    "8e05784c1b7a13ec0f722de7f701a4754bc7c33a7d271705f4578bba4821e0d0"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/transcript-hashes.json",
    "0cd5311a86c8736401f042ce1618632972623f71a21ca09ded8662003f84b466"
  ],
  [
    "desktop/experimental/vendor/openmls/openmls/test_vectors/treekem.json",
    "d8bbdf78394797f21db7476fd286af6830ebcb18d2e3b2c839fecd4eb11056f8"
  ],
  [
    "desktop/experimental/vendor/sframe/src/test_vectors/test-vectors.json",
    "b8d35efd41749567427cb9ae52d9a7362904154978ff6a5fb20c0258a8ffdec1"
  ]
]);
const args = process.argv.slice(2);
if (args.some(arg => arg !== '--staged') || args.length > 1) throw new Error('Usage: check-native-test-vectors.mjs [--staged]');
const staged = args.includes('--staged');
for (const [path, hash] of Object.entries(expected)) {
  const bytes = staged ? execFileSync('git', ['show', `:${path}`], { maxBuffer: 64 * 1024 * 1024, stdio: ['ignore', 'pipe', 'ignore'] })
    : (() => { if (!lstatSync(path).isFile() || lstatSync(path).isSymbolicLink()) throw new Error(`Not a regular test-vector file: ${path}`); return readFileSync(path); })();
  if (createHash('sha256').update(bytes).digest('hex') !== hash) throw new Error(`Pinned public test-vector bytes changed: ${path}`);
}
console.log(`Verified ${Object.keys(expected).length} pinned public test-vector files (${staged ? 'index' : 'working tree'}).`);
