import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import {
  FRAME_OVERHEAD_BYTES, ReplayWindow, SOURCE_CAMERA, SOURCE_MIC, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, isFrameSource, layerStreamId, lineByte,
  openFrame, sealFrame, type EpochKeys, type FrameSource,
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

async function keysOf(byte = 7): Promise<EpochKeys> {
  return testEpochKeys(await testAesKey(byte), alice, { alice, bob })
}

interface Made { source?: FrameSource, layer?: number | undefined, stream?: number, counter?: number }

async function made(keys: EpochKeys, { source = SOURCE_MIC, layer, stream = 9, counter = 0 }: Made = {}) {
  const packet = await sealFrame({ keys, epoch: 4, stream, counter, channelId: 'channel', senderId: 'alice', plaintext: Uint8Array.of(1, 2, 3), source, layer })
  if (!packet) throw new Error('expected a sealed frame')
  return packet
}

function open(keys: EpochKeys, packet: Uint8Array, over: { source?: FrameSource | null, onLegacy?: (() => void) | undefined, replay?: ReplayWindow } = {}) {
  return openFrame({ keys: new Map([[4, keys]]), channelId: 'channel', senderId: 'alice', packet, replay: over.replay ?? new ReplayWindow(), source: over.source, ...(over.onLegacy ? { onLegacy: over.onLegacy } : {}) })
}

describe('the frame binds the line', () => {
  it('seals a line byte and opens it on the line it names', async () => {
    const keys = await keysOf()
    for (const source of [SOURCE_MIC, SOURCE_CAMERA, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO] as const) {
      const stream = source === SOURCE_SCREEN ? layerStreamId(0, 100) : 9
      const packet = await made(keys, { source, stream })
      expect([...packet.subarray(0, 2)]).toEqual([0x4d, 0x33])
      expect(packet[14]).toBe(lineByte(source))
      expect(packet.byteLength).toBe(3 + FRAME_OVERHEAD_BYTES)
      await expect(open(keys, packet, { source })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    }
    expect(isFrameSource(3)).toBe(true)
    expect(isFrameSource(4)).toBe(false)
  })

  it('refuses a frame relabelled onto another line: mic as screen sound, camera as screen, and the reverse', async () => {
    const keys = await keysOf()
    const mic = await made(keys, { source: SOURCE_MIC })
    const camera = await made(keys, { source: SOURCE_CAMERA })
    const screen = await made(keys, { source: SOURCE_SCREEN, stream: layerStreamId(0, 100) })
    const sound = await made(keys, { source: SOURCE_SCREEN_AUDIO })
    await expect(open(keys, mic, { source: SOURCE_SCREEN_AUDIO })).resolves.toBeNull()
    await expect(open(keys, sound, { source: SOURCE_MIC })).resolves.toBeNull()
    await expect(open(keys, camera, { source: SOURCE_SCREEN })).resolves.toBeNull()
    await expect(open(keys, screen, { source: SOURCE_CAMERA })).resolves.toBeNull()
    // Nothing opens where the receiver does not know the line.
    await expect(open(keys, mic)).resolves.toBeNull()
    await expect(open(keys, mic, { source: null })).resolves.toBeNull()
  })

  it('cannot be relabelled by rewriting the line byte: it is signed and part of the AAD', async () => {
    const keys = await keysOf()
    const mic = await made(keys, { source: SOURCE_MIC })
    const forged = mic.slice()
    forged[14] = lineByte(SOURCE_SCREEN_AUDIO)
    await expect(open(keys, forged, { source: SOURCE_SCREEN_AUDIO })).resolves.toBeNull()
    // A unit made to look like the retired version 2 is not opened.
    const lowered = mic.slice()
    lowered[1] = 0x32
    await expect(open(keys, lowered, { source: SOURCE_MIC })).resolves.toBeNull()
  })

  it('keeps a screen layer to its own stream: layer 1 does not pass for layer 0', async () => {
    const keys = await keysOf()
    const layer1 = await made(keys, { source: SOURCE_SCREEN, layer: 1, stream: layerStreamId(1, 100) })
    await expect(open(keys, layer1, { source: SOURCE_SCREEN })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    // Rewritten to say layer 0: the signature breaks.
    const forged = layer1.slice()
    forged[14] = lineByte(SOURCE_SCREEN, 0)
    await expect(open(keys, forged, { source: SOURCE_SCREEN })).resolves.toBeNull()
    // Sealed honestly with a layer its stream does not carry: refused as well.
    const mismatched = await made(keys, { source: SOURCE_SCREEN, layer: 1, stream: layerStreamId(0, 100) })
    await expect(open(keys, mismatched, { source: SOURCE_SCREEN })).resolves.toBeNull()
    // Only a screen has layers.
    const layered = await made(keys, { source: SOURCE_MIC, layer: 2 })
    expect(layered[14]).toBe(lineByte(SOURCE_MIC))
    const odd = layered.slice()
    odd[14] = (SOURCE_MIC << 4) | 2
    await expect(open(keys, odd, { source: SOURCE_MIC })).resolves.toBeNull()
    const junk = layered.slice()
    junk[14] = (SOURCE_MIC << 4) | 8
    await expect(open(keys, junk, { source: SOURCE_MIC })).resolves.toBeNull()
    const nowhere = layered.slice()
    nowhere[14] = 0x70
    await expect(open(keys, nowhere, { source: SOURCE_MIC })).resolves.toBeNull()
  })

  it('keeps the replay window: a unit opens once, whatever its line', async () => {
    const keys = await keysOf()
    const replay = new ReplayWindow()
    const packet = await made(keys, { counter: 5 })
    await expect(open(keys, packet, { source: SOURCE_MIC, replay })).resolves.toEqual(Uint8Array.of(1, 2, 3))
    await expect(open(keys, packet, { source: SOURCE_MIC, replay })).resolves.toBeNull()
    // The unit that did not open on the wrong line was not marked: it still opens on its own.
    const other = await made(keys, { counter: 6 })
    await expect(open(keys, other, { source: SOURCE_CAMERA, replay })).resolves.toBeNull()
    await expect(open(keys, other, { source: SOURCE_MIC, replay })).resolves.toEqual(Uint8Array.of(1, 2, 3))
  })

  it('seals only for a source, and short units do not open', async () => {
    const keys = await keysOf()
    await expect(sealFrame({ keys, epoch: 4, stream: 1, counter: 0, channelId: 'channel', senderId: 'alice', plaintext: Uint8Array.of(1), source: 9 as FrameSource })).resolves.toBeNull()
    const packet = await made(keys)
    // One byte short of the smallest unit.
    const short = packet.subarray(0, FRAME_OVERHEAD_BYTES - 1)
    await expect(open(keys, short, { source: SOURCE_MIC })).resolves.toBeNull()
  })
})

/** What the retired version 2 unit looked like: no line byte, magic 'M' '2'. */
function legacyOf(packet: Uint8Array): Uint8Array {
  const legacy = new Uint8Array(packet.byteLength - 1)
  legacy.set(packet.subarray(0, 14))
  legacy.set(packet.subarray(15), 14)
  legacy[1] = 0x32
  return legacy
}

describe('the retired frame version 2', () => {
  it('is not opened on any line, however it is relabelled, and is reported', async () => {
    const keys = await keysOf()
    const legacy = legacyOf(await made(keys))
    for (const source of [SOURCE_MIC, SOURCE_CAMERA, SOURCE_SCREEN, SOURCE_SCREEN_AUDIO, null, undefined] as (FrameSource | null | undefined)[]) {
      let reported = 0
      await expect(open(keys, legacy, { ...(source === undefined ? {} : { source }), onLegacy: () => { reported++ } })).resolves.toBeNull()
      expect(reported).toBe(1)
    }
  })

  it('is not sealed: the output is always version 3', async () => {
    const keys = await keysOf()
    for (const source of [SOURCE_MIC, SOURCE_CAMERA, SOURCE_SCREEN_AUDIO] as const) {
      expect([...(await made(keys, { source })).subarray(0, 2)]).toEqual([0x4d, 0x33])
    }
  })
})

describe('malformed units are refused without a crash', () => {
  it('refuses empty, truncated, mismagicked and bit-flipped units, and reports only version 2', async () => {
    const keys = await keysOf()
    const good = await made(keys)
    let legacyReports = 0
    const cases: [string, Uint8Array][] = [
      ['empty', new Uint8Array()],
      ['one byte', Uint8Array.of(0x4d)],
      ['magic only', Uint8Array.of(0x4d, 0x33)],
      ['wrong magic', Uint8Array.from(good, (byte, at) => (at === 0 ? 0x00 : byte))],
      ['unknown version', Uint8Array.from(good, (byte, at) => (at === 1 ? 0x34 : byte))],
      ['version 1', Uint8Array.from(good, (byte, at) => (at === 1 ? 0x31 : byte))],
      ['all zero', new Uint8Array(good.byteLength)],
      ['all ones', new Uint8Array(good.byteLength).fill(0xff)],
      ['header only', good.subarray(0, 27)],
      ['signature missing', good.subarray(0, good.byteLength - 64)],
      ['one byte short', good.subarray(0, good.byteLength - 1)],
    ]
    for (let length = 0; length < FRAME_OVERHEAD_BYTES; length++) cases.push([`truncated to ${length}`, good.subarray(0, length)])
    // A deterministic spread of random bodies behind a valid magic.
    let state = 0x9e3779b9
    for (let round = 0; round < 24; round++) {
      const body = new Uint8Array(FRAME_OVERHEAD_BYTES + (round % 5) * 7)
      for (let at = 0; at < body.length; at++) {
        state = (Math.imul(state, 1664525) + 1013904223) >>> 0
        body[at] = state >>> 24
      }
      body[0] = 0x4d
      body[1] = 0x33
      cases.push([`random ${round}`, body])
    }
    for (const [name, packet] of cases) {
      await expect(open(keys, packet, { source: SOURCE_MIC, onLegacy: () => { legacyReports++ } }), name).resolves.toBeNull()
    }
    expect(legacyReports).toBe(0)
    await expect(open(keys, good, { source: SOURCE_MIC })).resolves.toEqual(Uint8Array.of(1, 2, 3))
  })

  it('refuses units too short or oddly shaped as version 2 too', async () => {
    const keys = await keysOf()
    const legacy = legacyOf(await made(keys))
    let reported = 0
    for (const length of [2, 3, 13, 14, 26, legacy.byteLength - 1]) {
      await expect(open(keys, legacy.subarray(0, length), { source: SOURCE_MIC, onLegacy: () => { reported++ } })).resolves.toBeNull()
    }
    expect(reported).toBe(6)
  })
})
