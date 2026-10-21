import { webcrypto } from 'node:crypto'
import { type EpochKeys } from './frame'

const subtle = webcrypto.subtle as unknown as SubtleCrypto

export interface TestMember {
  signer: CryptoKey
  verifier: CryptoKey
}

/** An Ed25519 key pair standing in for one member's MLS leaf key. */
export async function testMember(): Promise<TestMember> {
  const pair = await subtle.generateKey('Ed25519', false, ['sign', 'verify']) as CryptoKeyPair
  return { signer: pair.privateKey, verifier: pair.publicKey }
}

export async function testAesKey(byte: number): Promise<CryptoKey> {
  return subtle.importKey('raw', new Uint8Array(16).fill(byte), 'AES-GCM', false, ['encrypt', 'decrypt'])
}

/**
 * Epoch keys as `self` holds them: the shared AES key, self's signer and the
 * verifiers of every listed member (self included).
 */
export function testEpochKeys(key: CryptoKey, self: TestMember, members: Record<string, TestMember>): EpochKeys {
  return { key, signer: self.signer, verifiers: new Map(Object.entries(members).map(([id, member]) => [id, member.verifier])) }
}
