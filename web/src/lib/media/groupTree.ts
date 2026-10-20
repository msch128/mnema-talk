import {
  defaultKeyPackageEqualityConfig,
  defaultKeyRetentionConfig,
  defaultLifetimeConfig,
  defaultPaddingConfig,
  encodeMlsMessage,
  getCiphersuiteFromName,
  getCiphersuiteImpl,
  zeroOutUint8Array,
  type CiphersuiteImpl,
  type ClientConfig,
  type ClientState,
  type Credential,
} from 'ts-mls'
import { HANDSHAKE_MAX_BYTES, mediaUserId } from './bounds'
import { importFrameVerifier } from './frame'

let suitePromise: Promise<CiphersuiteImpl> | null = null

export function suite(): Promise<CiphersuiteImpl> {
  suitePromise ??= getCiphersuiteImpl(getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  return suitePromise
}

export function credentialIdentity(credential: Credential): string | null {
  if (credential.credentialType !== 'basic') return null
  const text = new TextDecoder().decode(credential.identity)
  return mediaUserId(text) === text ? text : null
}

export function clientConfig(): ClientConfig {
  return {
    keyRetentionConfig: defaultKeyRetentionConfig,
    lifetimeConfig: defaultLifetimeConfig,
    keyPackageEqualityConfig: defaultKeyPackageEqualityConfig,
    paddingConfig: defaultPaddingConfig,
    authService: {
      validateCredential(credential: Credential): Promise<boolean> {
        return Promise.resolve(credentialIdentity(credential) !== null)
      },
    },
  }
}

export function sameBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false
  for (let i = 0; i < left.byteLength; i++) if (left[i] !== right[i]) return false
  return true
}

export function wipeBytes(bytes: Uint8Array | undefined) {
  if (bytes) zeroOutUint8Array(bytes)
}

export function wipeConsumed(consumed: Uint8Array[]) {
  for (const secret of consumed) wipeBytes(secret)
}

export function treeMembers(state: ClientState): Map<string, number> {
  const members = new Map<string, number>()
  state.ratchetTree.forEach((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
    const id = credentialIdentity(node.leaf.credential)
    if (id) members.set(id, index / 2)
  })
  return members
}

/** Every leaf's identity, in tree order: a repeated or unreadable one stays visible ('' for unreadable). */
export function leafIdentities(state: ClientState): string[] {
  const ids: string[] = []
  state.ratchetTree.forEach((node, index) => {
    if (node && node.nodeType === 'leaf' && index % 2 === 0) ids.push(credentialIdentity(node.leaf.credential) ?? '')
  })
  return ids
}

function singleSignatureKeys(state: ClientState): Map<string, Uint8Array> {
  const leaves = new Map<string, Uint8Array[]>()
  state.ratchetTree.forEach((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
    const id = credentialIdentity(node.leaf.credential)
    if (id) leaves.set(id, [...(leaves.get(id) ?? []), node.leaf.signaturePublicKey])
  })
  const single = new Map<string, Uint8Array>()
  for (const [id, keys] of leaves) {
    if (keys.length === 1 && keys[0]) single.set(id, keys[0])
  }
  return single
}

/**
 * Each member's frame verifying key, from its leaf's signature key. An id that
 * holds more than one leaf gets none: which of its leaves signed a frame could
 * not be told apart.
 */
export async function frameVerifiers(state: ClientState): Promise<Map<string, CryptoKey> | null> {
  const single = singleSignatureKeys(state)
  const verifiers = new Map<string, CryptoKey>()
  for (const [id, keyBytes] of single) {
    const key = await importFrameVerifier(keyBytes)
    if (!key) return null
    verifiers.set(id, key)
  }
  return verifiers
}

/**
 * The signature key of a member's leaf. Unlike the leaf's HPKE key it stays the
 * same when that member commits, so it tells a fresh client (a reload) apart
 * from a member repeating the package it joined or founded the group with.
 */
export function treeLeafSigner(state: ClientState, id: string): Uint8Array | null {
  const index = treeMembers(state).get(id)
  const node = index === undefined ? undefined : state.ratchetTree[index * 2]
  return node && node.nodeType === 'leaf' ? node.leaf.signaturePublicKey : null
}

function shouldCheckLeaf(
  node: unknown,
  i: number,
  removed: ReadonlySet<number>
): node is { nodeType: 'leaf'; leaf: { credential: Credential; signaturePublicKey: Uint8Array; hpkePublicKey: Uint8Array } } {
  if (!node || i % 2 !== 0) return false
  const leafNode = node as { nodeType: string }
  if (leafNode.nodeType !== 'leaf') return false
  return !removed.has(i / 2)
}

function matchesRenewedLeaf(
  node: { leaf: { credential: Credential; signaturePublicKey: Uint8Array; hpkePublicKey: Uint8Array } },
  next: unknown,
  i: number,
  index: number
): boolean {
  if (!next) return false
  const nextNode = next as { nodeType: string; leaf: { credential: Credential; signaturePublicKey: Uint8Array; hpkePublicKey: Uint8Array } }
  if (nextNode.nodeType !== 'leaf') return false
  if (credentialIdentity(nextNode.leaf.credential) !== credentialIdentity(node.leaf.credential)) return false
  if (!sameBytes(node.leaf.signaturePublicKey, nextNode.leaf.signaturePublicKey)) return false
  return i === index * 2 || sameBytes(node.leaf.hpkePublicKey, nextNode.leaf.hpkePublicKey)
}

/**
 * What a commit may do to the leaves that are already there: every leaf of
 * `before` the commit does not remove (or replace) is still at its position
 * with its identity and signature key, and only the sender's own HPKE key (the
 * leaf at `index`) may have changed. The committer's path commit and a
 * member's renewal alike can change nothing else; leaves the commit adds are
 * checked against the roster and the relayed key packages separately.
 */
export function onlyRenewsLeaf(before: ClientState, after: ClientState, index: number, removed: ReadonlySet<number> = new Set()): boolean {
  return before.ratchetTree.every((node, i) => {
    if (!shouldCheckLeaf(node, i, removed)) return true
    return matchesRenewedLeaf(node, after.ratchetTree[i], i, index)
  })
}

/**
 * No leaf of `before` is gone or swapped in `after` (same position, same
 * signature key): the commit removed nobody. A leaf replaced by a new one of
 * the same user id is a removal.
 */
export function keepsEveryLeaf(before: ClientState, after: ClientState): boolean {
  return before.ratchetTree.every((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return true
    const next = after.ratchetTree[index]
    return !!next && next.nodeType === 'leaf' && sameBytes(node.leaf.signaturePublicKey, next.leaf.signaturePublicKey)
  })
}

export function lowest(ids: Iterable<string>): string | null {
  let best: string | null = null
  for (const id of ids) if (best === null || id < best) best = id
  return best
}

export function encodeWire(message: Parameters<typeof encodeMlsMessage>[0]): Uint8Array | null {
  try {
    const bytes = encodeMlsMessage(message)
    return bytes.byteLength > 0 && bytes.byteLength <= HANDSHAKE_MAX_BYTES ? bytes : null
  } catch {
    return null
  }
}
