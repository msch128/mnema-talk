import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import {
  FRAME_OVERHEAD_BYTES, FrameStream, REPLAY_STREAMS, ReplayWindow, SIGNATURE_BYTES, importEpochKey, importFrameSigner, importFrameVerifier,
  openFrame, rememberEpochKey, sealFrame, type EpochKeys,
} from './frame'
import { testAesKey, testEpochKeys, testMember, type TestMember } from './keys.fixture'

const previous = globalThis.crypto
let alice: TestMember
let bob: TestMember

beforeAll(async () => {
  vi.stubGlobal('crypto', webcrypto)
  alice = await testMember()
  bob = await testMember()
})
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function aliceKeys(byte = 7): Promise<EpochKeys> {
  return testEpochKeys(await testAesKey(byte), alice, { alice, bob })
}

async function sealed(keys: EpochKeys, { epoch = 4, stream = 9, counter = 0, senderId = 'alice', plaintext = Uint8Array.of(1, 2, 3) } = {}) {
  const packet = await sealFrame({ keys, epoch, stream, counter, channelId: 'channel', senderId, plaintext })
  if (!packet) throw new Error('expected a sealed frame')
  return packet
}

function open(keys: EpochKeys | ReadonlyMap<number, EpochKeys>, packet: Uint8Array, { senderId = 'alice', channelId = 'channel', replay = new ReplayWindow(), epoch = 4 } = {}) {
  const map = keys instanceof Map ? keys : new Map([[epoch, keys as EpochKeys]])
  return openFrame({ keys: map, channelId, senderId, packet, replay })
}

function flipped(packet: Uint8Array, index: number): Uint8Array {
  const copy = packet.slice()
  copy[index] = (copy[index] ?? 0) ^ 0x01
  return copy
}

describe('media frames', () => {
  it('opens a frame signed by its sender for the same channel, epoch and key', async () => {
    const keys = await aliceKeys()
    const packet = await sealed(keys)
    expect([...packet.subarray(0, 2)]).toEqual([0x4d, 0x32])
    expect(packet.byteLength).toBe(3 + FRAME_OVERHEAD_BYTES)
    await expect(open(keys, packet)).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, packet, { channelId: 'other' })).resolves.toBeNull()
    await expect(open(await aliceKeys(8), packet)).resolves.toBeNull()
    const missed: number[] = []
    await expect(openFrame({ keys: new Map(), channelId: 'channel', senderId: 'alice', packet, replay: new ReplayWindow(), onUnknownEpoch: epoch => missed.push(epoch) })).resolves.toBeNull()
    expect(missed).toEqual([4])
    await expect(openFrame({ keys: new Map(), channelId: 'channel', senderId: 'alice', packet, replay: new ReplayWindow() })).resolves.toBeNull()
  })

  it('refuses a frame passed off as another member’s, or from a member without a single leaf', async () => {
    const keys = await aliceKeys()
    const packet = await sealed(keys)
    // The line says bob publishes: alice's signature does not verify as his.
    await expect(open(keys, packet, { senderId: 'bob' })).resolves.toBeNull()
    // Bob holds the same AES key but signs with his own leaf key.
    const asBob = testEpochKeys(keys.key, bob, { alice, bob })
    await expect(open(keys, await sealed(asBob))).resolves.toBeNull()
    // An id that holds two leaves has no verifier at all.
    const duplicated = testEpochKeys(keys.key, bob, { bob })
    await expect(open(duplicated, packet)).resolves.toBeNull()
  })

  it('refuses any changed byte of the header, ciphertext or signature', async () => {
    const keys = await aliceKeys()
    const packet = await sealed(keys, { counter: 5 })
    // epoch, stream, counter, nonce, ciphertext, tag, signature
    for (const index of [2, 6, 9, 13, 14, 26, packet.byteLength - SIGNATURE_BYTES - 1, packet.byteLength - 1]) {
      await expect(open(keys, flipped(packet, index)), `byte ${index}`).resolves.toBeNull()
    }
    await expect(open(keys, packet.slice(0, FRAME_OVERHEAD_BYTES - 1))).resolves.toBeNull()
    const unmarked = packet.slice()
    unmarked[1] = 0x31
    await expect(open(keys, unmarked)).resolves.toBeNull()
    await expect(open(keys, packet)).resolves.toEqual(Uint8Array.of(1, 2, 3))
  })

  it('opens a unit once, out of order inside the window, never below it', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    const at = (counter: number, stream = 9) => sealed(keys, { counter, stream })
    const first = await at(100)
    await expect(open(keys, first, { replay })).resolves.not.toBeNull()
    await expect(open(keys, first, { replay })).resolves.toBeNull()
    await expect(open(keys, await at(99), { replay })).resolves.not.toBeNull()
    await expect(open(keys, await at(37), { replay })).resolves.not.toBeNull()
    await expect(open(keys, await at(36), { replay })).resolves.toBeNull()
    await expect(open(keys, await at(200), { replay })).resolves.not.toBeNull()
    await expect(open(keys, await at(137), { replay })).resolves.not.toBeNull()
    await expect(open(keys, await at(136), { replay })).resolves.toBeNull()
    await expect(open(keys, first, { replay })).resolves.toBeNull()
    // A restarted sender starts a new stream at 0, the old one stays closed.
    await expect(open(keys, await at(0, 10), { replay })).resolves.not.toBeNull()
    await expect(open(keys, first, { replay })).resolves.toBeNull()
  })

  it('opens a unit only once when two copies are checked at the same time', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    const packet = await sealed(keys)
    const both = await Promise.all([open(keys, packet, { replay }), open(keys, packet, { replay })])
    expect(both.filter(opened => opened !== null)).toHaveLength(1)
  })

  it('does not mark a unit that failed to verify or open', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    const packet = await sealed(keys)
    await expect(open(keys, flipped(packet, packet.byteLength - 1), { replay })).resolves.toBeNull()
    await expect(open(keys, packet, { replay })).resolves.not.toBeNull()
  })

  it('seals nothing for an epoch, stream or counter out of range, or without usable keys', async () => {
    const keys = await aliceKeys()
    for (const range of [{ epoch: -1 }, { epoch: 1.5 }, { epoch: 0x1_0000_0000 }, { stream: -1 }, { counter: 0x1_0000_0000 }]) {
      await expect(sealFrame({ keys, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: new Uint8Array(), ...range })).resolves.toBeNull()
    }
    const decryptOnly = await crypto.subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['decrypt'])
    await expect(sealFrame({ keys: { ...keys, key: decryptOnly }, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1) })).resolves.toBeNull()
    await expect(sealFrame({ keys: { ...keys, signer: bob.verifier }, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1) })).resolves.toBeNull()
    const shortSignature = vi.spyOn(crypto.subtle, 'sign').mockResolvedValueOnce(new ArrayBuffer(10))
    await expect(sealFrame({ keys, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1) })).resolves.toBeNull()
    shortSignature.mockRestore()
  })

  it('numbers a stream until the counter runs out', () => {
    const stream = new FrameStream(42)
    expect(stream.id).toBe(42)
    expect([stream.next(), stream.next()]).toEqual([0, 1])
    stream.skipTo(0xffffffff)
    expect(stream.next()).toBe(0xffffffff)
    expect(stream.next()).toBeNull()
    expect(stream.next()).toBeNull()
    expect(Number.isInteger(new FrameStream().id)).toBe(true)
  })

  it('refuses new streams past the cap instead of forgetting old ones, and forgets dropped epochs', async () => {
    const replay = new ReplayWindow()
    for (let stream = 0; stream < REPLAY_STREAMS; stream++) expect(replay.mark(1, stream, 5)).toBe(true)
    // Nothing was evicted: every stream still refuses its seen unit.
    for (let stream = 0; stream < REPLAY_STREAMS; stream++) expect(replay.fresh(1, stream, 5)).toBe(false)
    expect(replay.fresh(1, REPLAY_STREAMS, 0)).toBe(false)
    expect(replay.mark(1, REPLAY_STREAMS, 0)).toBe(false)
    // Known streams go on; another epoch has its own room.
    expect(replay.mark(1, 0, 6)).toBe(true)
    expect(replay.mark(2, 1, 5)).toBe(true)
    replay.retain([2])
    expect(replay.fresh(1, 0, 5)).toBe(true)
    expect(replay.fresh(2, 1, 5)).toBe(false)
    expect(replay.mark(2, 1, 1000)).toBe(true)
    expect(replay.fresh(2, 1, 5)).toBe(false)
  })

  it('keeps refusing the frame of an old stream after the sender used up its streams', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    const first = await sealed(keys, { stream: 0 })
    await expect(open(keys, first, { replay })).resolves.not.toBeNull()
    for (let stream = 1; stream <= REPLAY_STREAMS; stream++) await open(keys, await sealed(keys, { stream }), { replay })
    await expect(open(keys, first, { replay })).resolves.toBeNull()
    await expect(open(keys, await sealed(keys, { stream: REPLAY_STREAMS + 1 }), { replay })).resolves.toBeNull()
  })

  it('imports MLS signing keys as a seed or PKCS#8, and verifying keys raw', async () => {
    const seed = new Uint8Array(32).fill(3)
    const fromSeed = await importFrameSigner(seed)
    expect(fromSeed?.type).toBe('private')
    expect(seed).toEqual(new Uint8Array(32).fill(3))
    const generated = await crypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']) as CryptoKeyPair
    const pkcs8 = new Uint8Array(await crypto.subtle.exportKey('pkcs8', generated.privateKey))
    const signer = await importFrameSigner(pkcs8)
    const verifier = await importFrameVerifier(new Uint8Array(await crypto.subtle.exportKey('raw', generated.publicKey)))
    if (!signer || !verifier) throw new Error('expected imported keys')
    const keys = { key: await testAesKey(1), signer, verifiers: new Map([['alice', verifier]]) }
    await expect(open(keys, await sealed(keys))).resolves.toEqual(Uint8Array.of(1, 2, 3))
    expect(await importFrameSigner(new Uint8Array(31))).toBeNull()
    expect(await importFrameVerifier(new Uint8Array(31))).toBeNull()
    const noEd25519 = vi.spyOn(crypto.subtle, 'importKey').mockRejectedValue(new Error('unsupported'))
    expect(await importFrameSigner(seed)).toBeNull()
    expect(await importFrameVerifier(new Uint8Array(32))).toBeNull()
    expect(await importEpochKey(new Uint8Array(16))).toBeNull()
    noEd25519.mockRestore()
    expect(await importEpochKey(new Uint8Array(15))).toBeNull()
  })

  it('keeps the two newest epoch keys', () => {
    const keys = new Map<number, string>()
    rememberEpochKey(keys, 1, 'first')
    rememberEpochKey(keys, 3, 'third')
    rememberEpochKey(keys, 2, 'second')
    expect([...keys.entries()].sort((a, b) => a[0] - b[0])).toEqual([[2, 'second'], [3, 'third']])
  })
})
