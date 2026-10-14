import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { openMedia, sealMedia, type OpenBytes, type SealBytes } from './codec'
import { importEpochKey, openFrame, sealFrame } from './frame'

const previous = globalThis.crypto

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function ends() {
  const key = await importEpochKey(new Uint8Array(16).fill(7))
  if (!key) throw new Error('expected an epoch key')
  const keys = new Map([[1, key]])
  const seal: SealBytes = (plaintext, clear) => sealFrame({ key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext, clear })
  const open: OpenBytes = (packet, clear) => openFrame({ keys, channelId: 'channel', senderId: 'alice', packet, clear })
  return { seal, open }
}

const START = [0, 0, 0, 1]

function annexB(...nals: number[][]): Uint8Array {
  return Uint8Array.from(nals.flatMap(nal => [...START, ...nal]))
}

function fill(length: number, from: number): number[] {
  return Array.from({ length }, (_, i) => (from + i * 37) & 0xff)
}

/** NAL bodies of an Annex B frame, the way a packetizer cuts them. */
function nals(data: Uint8Array): Uint8Array[] {
  const out: Uint8Array[] = []
  let at = -1
  for (let i = 0; i + 2 < data.length; i++) {
    if (data[i] === 0 && data[i + 1] === 0 && data[i + 2] === 1) {
      if (at >= 0) {
        let end = i
        while (end > at && data[end - 1] === 0) end--
        out.push(data.subarray(at, end))
      }
      at = i + 3
      i += 2
    }
  }
  if (at >= 0) out.push(data.subarray(at))
  return out
}

const sps = [0x67, 0x42, 0xc0, 0x1e, 0x8c, 0x68, 0x0a]
const pps = [0x68, 0xce, 0x3c, 0x80]

describe('sealing video so the network path still parses it', () => {
  it('keeps every H.264 NAL unit, header and parameter set readable', async () => {
    const { seal, open } = await ends()
    const idr = [0x65, 0x88, 0x84, 0x21, ...fill(600, 3)]
    const frame = annexB(sps, pps, idr)
    const sealed = await sealMedia(frame, 'key', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect(cut).toHaveLength(3)
    expect([...(cut[0] ?? new Uint8Array())]).toEqual(sps)
    expect([...(cut[1] ?? new Uint8Array())]).toEqual(pps)
    // A browser reads the NAL type and the start of the slice header.
    expect([...(cut[2] ?? new Uint8Array()).subarray(0, 4)]).toEqual(idr.slice(0, 4))
    expect(sealed.length).toBeGreaterThan(frame.length)
    expect(cut[2]?.[cut[2].length - 1]).not.toBe(0)
    // The picture itself is not readable.
    expect(Buffer.from(sealed).includes(Buffer.from(idr.slice(4, 40)))).toBe(false)
    expect(await openMedia(sealed, 'key', open)).toEqual(frame)
  })

  it('seals a delta slice and leaves other NAL types alone', async () => {
    const { seal, open } = await ends()
    const sei = [0x06, 0x05, 0x02, 0xaa, 0xbb]
    const slice = [0x61, 0xe0, 0x00, 0x7e, ...fill(300, 11)]
    const frame = annexB(sei, slice)
    const sealed = await sealMedia(frame, 'delta', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect([...(cut[0] ?? new Uint8Array())]).toEqual(sei)
    expect([...(cut[1] ?? new Uint8Array()).subarray(0, 4)]).toEqual(slice.slice(0, 4))
    expect(await openMedia(sealed, 'delta', open)).toEqual(frame)
  })

  it('never lets sealed bytes look like a start code or end in a zero', async () => {
    const plain = Uint8Array.from([0x65, 0x88, 0x84, 0x21, 9, 9, 9])
    const frame = annexB([...plain])
    const awkward = [0, 0, 1, 0, 0, 0, 0, 2, 0, 0, 3, 0, 0, 0]
    const seal: SealBytes = async () => Uint8Array.from(awkward)
    const sealed = await sealMedia(frame, 'key', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect(cut).toHaveLength(1)
    const body = cut[0] ?? new Uint8Array()
    for (let i = 4; i + 2 < body.length; i++) {
      if (body[i] === 0 && body[i + 1] === 0) expect(body[i + 2]).toBeGreaterThanOrEqual(3)
    }
    let seen: Uint8Array | null = null
    const open: OpenBytes = async (packet) => { seen = packet; return plain.subarray(4) }
    expect(await openMedia(sealed, 'key', open)).toEqual(frame)
    expect([...(seen ?? new Uint8Array())]).toEqual(awkward)
  })

  it('refuses a picture NAL that was not sealed or was altered', async () => {
    const { seal, open } = await ends()
    const frame = annexB(sps, [0x65, 0x88, 0x84, 0x21, ...fill(80, 5)])
    expect(await openMedia(frame, 'key', open)).toBeNull()
    const sealed = await sealMedia(frame, 'key', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const header = sealed.length - nals(sealed)[1]!.length + 2
    const changedClear = sealed.slice()
    changedClear[header] = (changedClear[header] ?? 0) ^ 1
    expect(await openMedia(changedClear, 'key', open)).toBeNull()
    const changedBody = sealed.slice()
    changedBody[sealed.length - 10] = (changedBody[sealed.length - 10] ?? 0) ^ 1
    expect(await openMedia(changedBody, 'key', open)).toBeNull()
    expect(await sealMedia(annexB([0x65, 0x88, 0x84, 0x21]), 'key', seal)).toBeNull()
    const refuse: SealBytes = async () => null
    expect(await sealMedia(frame, 'key', refuse)).toBeNull()
  })

  it('keeps the VP8 payload header readable', async () => {
    const { seal, open } = await ends()
    const key = Uint8Array.from([0x50, 0x42, 0x00, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01, ...fill(200, 2)])
    const sealedKey = await sealMedia(key, 'key', seal)
    expect([...(sealedKey ?? new Uint8Array()).subarray(0, 10)]).toEqual([...key.subarray(0, 10)])
    expect(sealedKey?.length).toBeGreaterThan(key.length)
    expect(await openMedia(sealedKey ?? new Uint8Array(), 'key', open)).toEqual(key)
    const delta = Uint8Array.from([0x31, 0x01, 0x00, ...fill(120, 9)])
    const sealedDelta = await sealMedia(delta, 'delta', seal)
    expect([...(sealedDelta ?? new Uint8Array()).subarray(0, 3)]).toEqual([...delta.subarray(0, 3)])
    expect(await openMedia(sealedDelta ?? new Uint8Array(), 'delta', open)).toEqual(delta)
    const altered = (sealedDelta ?? new Uint8Array()).slice()
    altered[1] = (altered[1] ?? 0) ^ 1
    expect(await openMedia(altered, 'delta', open)).toBeNull()
    expect(await sealMedia(Uint8Array.of(1, 2), 'key', seal)).toBeNull()
    expect(await openMedia(Uint8Array.of(1, 2), 'delta', open)).toBeNull()
  })

  it('seals audio whole', async () => {
    const { seal, open } = await ends()
    const opus = Uint8Array.from(fill(40, 1))
    const sealed = await sealMedia(opus, undefined, seal)
    expect([...(sealed ?? new Uint8Array()).subarray(0, 2)]).toEqual([0x4d, 0x31])
    expect(await openMedia(sealed ?? new Uint8Array(), undefined, open)).toEqual(opus)
  })
})
