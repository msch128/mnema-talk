import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import {
  FRAME_OVERHEAD_BYTES, FrameStream, LAYER_STREAM_MASK, REPLAY_IDLE_MS, REPLAY_RETIRED, REPLAY_STREAMS, ReplayWindow, SIGNATURE_BYTES, layerStreamId, importEpochKey, importFrameSigner, importFrameVerifier,
  openFrame, sealFrame, type EpochKeys,
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
  const packet = await sealFrame({ keys, epoch, stream, counter, channelId: 'channel', senderId, plaintext, source: 0 })
  if (!packet) throw new Error('expected a sealed frame')
  return packet
}

function open(keys: EpochKeys | ReadonlyMap<number, EpochKeys>, packet: Uint8Array, { senderId = 'alice', channelId = 'channel', replay = new ReplayWindow(), epoch = 4 } = {}) {
  const map = keys instanceof Map ? keys : new Map([[epoch, keys as EpochKeys]])
  return openFrame({ keys: map, channelId, senderId, packet, replay, source: 0 })
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
    expect([...packet.subarray(0, 2)]).toEqual([0x4d, 0x33])
    expect(packet.byteLength).toBe(3 + FRAME_OVERHEAD_BYTES)
    await expect(open(keys, packet)).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, packet, { channelId: 'other' })).resolves.toBeNull()
    await expect(open(await aliceKeys(8), packet)).resolves.toBeNull()
    const missed: number[] = []
    await expect(openFrame({ keys: new Map(), channelId: 'channel', senderId: 'alice', packet, replay: new ReplayWindow(), source: 0, onUnknownEpoch: epoch => missed.push(epoch) })).resolves.toBeNull()
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
    // epoch, stream, counter, line, nonce, ciphertext, tag, signature
    for (const index of [2, 6, 9, 13, 14, 15, 26, 27, packet.byteLength - SIGNATURE_BYTES - 1, packet.byteLength - 1]) {
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
      await expect(sealFrame({ keys, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: new Uint8Array(), source: 0, ...range })).resolves.toBeNull()
    }
    const decryptOnly = await crypto.subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['decrypt'])
    await expect(sealFrame({ keys: { ...keys, key: decryptOnly }, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1), source: 0 })).resolves.toBeNull()
    await expect(sealFrame({ keys: { ...keys, signer: bob.verifier }, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1), source: 0 })).resolves.toBeNull()
    const shortSignature = vi.spyOn(crypto.subtle, 'sign').mockResolvedValueOnce(new ArrayBuffer(10))
    await expect(sealFrame({ keys, epoch: 1, stream: 1, counter: 1, channelId: 'c', senderId: 'a', plaintext: Uint8Array.of(1), source: 0 })).resolves.toBeNull()
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

  it('retires streams idle for a minute to make room, and never lets a retired stream replay', () => {
    let now = 1_000_000
    const replay = new ReplayWindow(() => now)
    for (let stream = 0; stream < REPLAY_STREAMS; stream++) expect(replay.mark(1, stream, 100 + stream)).toBe(true)
    // Nobody is idle yet: a new stream is still refused.
    now += REPLAY_IDLE_MS
    expect(replay.mark(1, 1000, 0)).toBe(false)
    // Stream 0 keeps talking; the rest go idle past the limit.
    now += 1
    expect(replay.mark(1, 0, 200)).toBe(true)
    expect(replay.mark(1, 1000, 0)).toBe(true)
    // Retired streams refuse everything up to their highest counter, even ones never seen.
    expect(replay.fresh(1, 5, 105)).toBe(false)
    expect(replay.fresh(1, 5, 100)).toBe(false)
    expect(replay.fresh(1, 5, 50)).toBe(false)
    // A newer counter reopens the stream, if there is room for it.
    expect(replay.fresh(1, 5, 106)).toBe(true)
    expect(replay.mark(1, 5, 106)).toBe(true)
    expect(replay.mark(1, 5, 106)).toBe(false)
    expect(replay.mark(1, 5, 105)).toBe(false)
    // A dropped epoch forgets its retired streams too.
    replay.retain([2])
    expect(replay.fresh(1, 6, 0)).toBe(true)
  })

  it('refuses new streams again once the retired ones fill up, and while no live stream is idle', () => {
    let now = 0
    const replay = new ReplayWindow(() => now)
    let stream = 0
    for (let round = 0; round < REPLAY_RETIRED / REPLAY_STREAMS + 1; round++) {
      for (let i = 0; i < REPLAY_STREAMS; i++) expect(replay.mark(1, stream++, 1)).toBe(true)
      now += REPLAY_IDLE_MS + 1
    }
    // 1280 streams are open or retired now; the last round is idle but the retired list is full.
    expect(replay.mark(1, stream, 1)).toBe(false)
    expect(replay.fresh(1, 0, 1)).toBe(false)
    expect(replay.fresh(2, stream, 1)).toBe(true)
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
})

// A screen share goes out as up to three layers, each sealed by a transform
// of its own; the SFU hands a viewer one of them at a time and switches
// between them. The AES-GCM nonce of a frame is 96 fresh random bits, so it
// does not depend on the layer at all; the stream ID and counter only drive
// the replay window and are bound into the authenticated data.
describe('layers of a screen share', () => {
  const NONCE_OFFSET = 15
  const NONCE_BYTES = 12

  it('puts the layer in the stream ID, so two layers can never share a stream', () => {
    for (const random of [0, 1, 3, 4, 0x7fffffff, 0x80000000, 0xfffffffc, 0xffffffff, 0x12345678]) {
      const ids = [0, 1, 2, 3].map(layer => layerStreamId(layer, random))
      expect(new Set(ids).size).toBe(4)
      ids.forEach((id, layer) => {
        expect(id & LAYER_STREAM_MASK).toBe(layer)
        expect(Number.isInteger(id) && id >= 0 && id <= 0xffffffff).toBe(true)
      })
    }
    // Without a given random part it is random: two draws differ almost surely.
    expect(new Set(Array.from({ length: 8 }, () => layerStreamId(1))).size).toBeGreaterThan(1)
  })

  it('opens every frame whichever layer the SFU forwards, and still refuses replays', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    const layers = [0, 1, 2].map(layer => ({ layer, stream: new FrameStream(layerStreamId(layer, 0xabcdef00)) }))
    const forwarded: Array<{ layer: number, counter: number, packet: Uint8Array }> = []
    // All layers number their frames from zero and keep sealing while only one is forwarded.
    const sealNext = async (layer: number) => {
      const entry = layers[layer]
      const counter = entry?.stream.next()
      if (!entry || counter === null || counter === undefined) throw new Error('no counter')
      const packet = await sealed(keys, { stream: entry.stream.id, counter, plaintext: Uint8Array.of(layer, counter & 0xff) })
      return { layer, counter, packet }
    }
    // The viewer sees: layer 0 for a while, layer 2 (its counter ran meanwhile), layer 0 again (jumped forward), layer 1.
    const plan = [0, 0, 0, 0, 0, 2, 2, 2, 0, 0, 1, 1, 2, 2, 0]
    const skipped = new Map<number, number>([[2, 40], [0, 25], [1, 90]])
    const seen = new Set<number>()
    for (const layer of plan) {
      if (!seen.has(layer) && seen.size) {
        // Frames sealed while this layer was not forwarded: the counter moves on.
        for (let i = 0; i < (skipped.get(layer) ?? 0); i++) await sealNext(layer)
      }
      seen.add(layer)
      forwarded.push(await sealNext(layer))
    }
    for (const frame of forwarded) {
      const opened = await open(keys, frame.packet, { replay })
      expect(opened, `layer ${frame.layer} counter ${frame.counter}`).toEqual(Uint8Array.of(frame.layer, frame.counter & 0xff))
    }
    // Nothing opens twice, in any layer, even right after a switch.
    for (const frame of forwarded) await expect(open(keys, frame.packet, { replay })).resolves.toBeNull()
    // Reordering across a switch within the window still works once.
    const late = await sealNext(1)
    const later = await sealNext(1)
    await expect(open(keys, later.packet, { replay })).resolves.not.toBeNull()
    await expect(open(keys, late.packet, { replay })).resolves.not.toBeNull()
    await expect(open(keys, late.packet, { replay })).resolves.toBeNull()
  })

  it('does not mistake one layer’s counter for another’s', async () => {
    const keys = await aliceKeys()
    const replay = new ReplayWindow()
    // Both layers send their counter 0: different streams, so both are fresh.
    const first = await sealed(keys, { stream: layerStreamId(0, 0x1000), counter: 0 })
    const second = await sealed(keys, { stream: layerStreamId(2, 0x1000), counter: 0 })
    await expect(open(keys, first, { replay })).resolves.not.toBeNull()
    await expect(open(keys, second, { replay })).resolves.not.toBeNull()
    // A frame moved into the other layer's stream no longer verifies (stream is authenticated).
    const view = new DataView(first.buffer, first.byteOffset, first.byteLength)
    view.setUint32(6, layerStreamId(1, 0x1000))
    await expect(open(keys, first, { replay: new ReplayWindow() })).resolves.toBeNull()
  })

  it('never repeats a nonce under one key across layers and frames', async () => {
    const keys = await aliceKeys()
    const nonces = new Set<string>()
    const drawn: string[] = []
    const real = crypto.getRandomValues.bind(crypto)
    const draw = vi.spyOn(crypto, 'getRandomValues').mockImplementation(((array: Uint8Array<ArrayBuffer>) => {
      const out = real(array)
      if (out.byteLength === NONCE_BYTES) drawn.push(Buffer.from(out).toString('hex'))
      return out
    }) as typeof crypto.getRandomValues)
    try {
      const streams = [0, 1, 2].map(layer => new FrameStream(layerStreamId(layer)))
      let frames = 0
      for (let counter = 0; counter < 400; counter++) {
        for (const stream of streams) {
          // The same (epoch, stream, counter) twice must not repeat its nonce either.
          for (let again = 0; again < 2; again++) {
            const packet = await sealed(keys, { stream: stream.id, counter })
            nonces.add(Buffer.from(packet.subarray(NONCE_OFFSET, NONCE_OFFSET + NONCE_BYTES)).toString('hex'))
            frames++
          }
        }
      }
      expect(frames).toBe(2400)
      expect(nonces.size).toBe(frames)
      // Every nonce is a fresh draw of the browser's random source, nothing derived.
      expect(drawn).toHaveLength(frames)
      expect(new Set(drawn).size).toBe(frames)
      expect([...nonces].every(n => drawn.includes(n))).toBe(true)
    } finally {
      draw.mockRestore()
    }
  })

  it('keeps room for the streams of reconnects and restarted shares', async () => {
    // A share takes three streams, a reconnect all of the sender's lines again.
    expect(REPLAY_STREAMS).toBeGreaterThanOrEqual(6 * 32)
    const replay = new ReplayWindow()
    for (let i = 0; i < 64; i++) expect(replay.mark(1, i, 0)).toBe(true)
    expect(replay.mark(1, 64, 0)).toBe(true)
  })
})
