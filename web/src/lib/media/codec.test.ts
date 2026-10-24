import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { openMedia, sealMedia, videoCodecOf, type OpenBytes, type SealBytes } from './codec'
import { FrameStream, ReplayWindow, openFrame, sealFrame } from './frame'
import { testAesKey, testEpochKeys, testMember } from './keys.fixture'

const previous = globalThis.crypto

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function ends() {
  const alice = await testMember()
  const epochKeys = testEpochKeys(await testAesKey(7), alice, { alice })
  const keys = new Map([[1, epochKeys]])
  const stream = new FrameStream()
  const seal: SealBytes = (plaintext, clear) => sealFrame({ keys: epochKeys, epoch: 1, stream: stream.id, counter: stream.next() ?? 0, channelId: 'channel', senderId: 'alice', plaintext, clear, source: 0 })
  // Framing is under test here, not replay: every unit may be opened again.
  const open: OpenBytes = (packet, clear) => openFrame({ keys, channelId: 'channel', senderId: 'alice', packet, clear, replay: new ReplayWindow(), source: 0 })
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
    const sealed = await sealMedia(frame, 'key', 'h264', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect(cut).toHaveLength(3)
    expect([...(cut[0] ?? new Uint8Array())]).toEqual(sps)
    expect([...(cut[1] ?? new Uint8Array())]).toEqual(pps)
    // A browser reads the NAL type and the slice header up to pps_id.
    expect([...(cut[2] ?? new Uint8Array()).subarray(0, 3)]).toEqual(idr.slice(0, 3))
    expect(sealed.length).toBeGreaterThan(frame.length)
    expect(cut[2]?.[cut[2].length - 1]).not.toBe(0)
    // The picture itself is not readable.
    expect(Buffer.from(sealed).includes(Buffer.from(idr.slice(4, 40)))).toBe(false)
    expect(await openMedia(sealed, 'key', 'h264', open)).toEqual(frame)
  })

  it('seals a delta slice and leaves the access unit delimiter readable', async () => {
    const { seal, open } = await ends()
    const aud = [0x09, 0xf0]
    const slice = [0x61, 0xe0, 0x00, 0x7e, ...fill(300, 11)]
    const frame = annexB(aud, slice)
    const sealed = await sealMedia(frame, 'delta', 'h264', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect([...(cut[0] ?? new Uint8Array())]).toEqual(aud)
    expect([...(cut[1] ?? new Uint8Array()).subarray(0, 2)]).toEqual(slice.slice(0, 2))
    expect(await openMedia(sealed, 'delta', 'h264', open)).toEqual(frame)
  })

  it('never lets sealed bytes look like a start code or end in a zero', async () => {
    const plain = Uint8Array.from([0x65, 0x88, 0x84, 0x21, 9, 9, 9])
    const frame = annexB([...plain])
    const awkward = [0, 0, 1, 0, 0, 0, 0, 2, 0, 0, 3, 0, 0, 0]
    const seal: SealBytes = async () => Uint8Array.from(awkward)
    const sealed = await sealMedia(frame, 'key', 'h264', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const cut = nals(sealed)
    expect(cut).toHaveLength(1)
    const body = cut[0] ?? new Uint8Array()
    for (let i = 3; i + 2 < body.length; i++) {
      if (body[i] === 0 && body[i + 1] === 0) expect(body[i + 2]).toBeGreaterThanOrEqual(3)
    }
    let seen: Uint8Array | null = null
    const open: OpenBytes = async (packet) => { seen = packet; return plain.subarray(3) }
    expect(await openMedia(sealed, 'key', 'h264', open)).toEqual(frame)
    expect([...(seen ?? new Uint8Array())]).toEqual(awkward)
  })

  it('refuses a picture NAL that was not sealed or was altered', async () => {
    const { seal, open } = await ends()
    const frame = annexB(sps, [0x65, 0x88, 0x84, 0x21, ...fill(80, 5)])
    expect(await openMedia(frame, 'key', 'h264', open)).toBeNull()
    const sealed = await sealMedia(frame, 'key', 'h264', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    const header = sealed.length - nals(sealed)[1]!.length + 2
    const changedClear = sealed.slice()
    changedClear[header] = (changedClear[header] ?? 0) ^ 1
    expect(await openMedia(changedClear, 'key', 'h264', open)).toBeNull()
    const changedBody = sealed.slice()
    changedBody[sealed.length - 10] = (changedBody[sealed.length - 10] ?? 0) ^ 1
    expect(await openMedia(changedBody, 'key', 'h264', open)).toBeNull()
    expect(await sealMedia(annexB([0x65, 0x88, 0x84]), 'key', 'h264', seal)).toBeNull()
    const refuse: SealBytes = async () => null
    expect(await sealMedia(frame, 'key', 'h264', refuse)).toBeNull()
  })

  it('keeps the VP8 payload header readable', async () => {
    const { seal, open } = await ends()
    const key = Uint8Array.from([0x50, 0x42, 0x00, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01, ...fill(200, 2)])
    const sealedKey = await sealMedia(key, 'key', 'vp8', seal)
    expect([...(sealedKey ?? new Uint8Array()).subarray(0, 10)]).toEqual([...key.subarray(0, 10)])
    expect(sealedKey?.length).toBeGreaterThan(key.length)
    expect(await openMedia(sealedKey ?? new Uint8Array(), 'key', 'vp8', open)).toEqual(key)
    const delta = Uint8Array.from([0x31, 0x01, 0x00, ...fill(120, 9)])
    const sealedDelta = await sealMedia(delta, 'delta', 'vp8', seal)
    expect([...(sealedDelta ?? new Uint8Array()).subarray(0, 3)]).toEqual([...delta.subarray(0, 3)])
    expect(await openMedia(sealedDelta ?? new Uint8Array(), 'delta', 'vp8', open)).toEqual(delta)
    const altered = (sealedDelta ?? new Uint8Array()).slice()
    altered[1] = (altered[1] ?? 0) ^ 1
    expect(await openMedia(altered, 'delta', 'vp8', open)).toBeNull()
    expect(await sealMedia(Uint8Array.of(1, 2), 'key', 'vp8', seal)).toBeNull()
    expect(await openMedia(Uint8Array.of(1, 2), 'delta', 'vp8', open)).toBeNull()
  })

  it('seals audio whole', async () => {
    const { seal, open } = await ends()
    const opus = Uint8Array.from(fill(40, 1))
    const sealed = await sealMedia(opus, undefined, null, seal)
    expect([...(sealed ?? new Uint8Array()).subarray(0, 2)]).toEqual([0x4d, 0x33])
    expect(await openMedia(sealed ?? new Uint8Array(), undefined, null, open)).toEqual(opus)
  })

  it('picks the framing from the codec, never from the bytes', async () => {
    const { seal, open } = await ends()
    expect(videoCodecOf('video/H264')).toBe('h264')
    expect(videoCodecOf('video/vp8')).toBe('vp8')
    for (const unknown of ['video/VP9', 'video/AV1', 'video/H265', 'audio/opus', '', undefined, 7]) expect(videoCodecOf(unknown)).toBeNull()
    // A VP8 frame whose payload header looks like an Annex B start code.
    const lookalike = Uint8Array.from([0, 0, 1, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01, ...fill(90, 4)])
    const sealed = await sealMedia(lookalike, 'key', 'vp8', seal)
    expect([...(sealed ?? new Uint8Array()).subarray(0, 10)]).toEqual([...lookalike.subarray(0, 10)])
    expect(await openMedia(sealed ?? new Uint8Array(), 'key', 'vp8', open)).toEqual(lookalike)
    // The same bytes as H.264 are one start code with no picture NAL: refused.
    expect(await sealMedia(lookalike, 'key', 'h264', seal)).toBeNull()
    // An Annex B frame labelled VP8 is sealed as VP8, not parsed as H.264.
    const labelled = annexB([0x65, 0x88, 0x84, 0x21, ...fill(40, 1)])
    const asVp8 = await sealMedia(labelled, 'key', 'vp8', seal)
    expect(await openMedia(asVp8 ?? new Uint8Array(), 'key', 'h264', open)).toBeNull()
  })

  it('refuses video of an unknown codec and an H.264 frame with no start code', async () => {
    const { seal, open } = await ends()
    const frame = annexB([0x65, 0x88, 0x84, 0x21, ...fill(40, 1)])
    expect(await sealMedia(frame, 'key', null, seal)).toBeNull()
    expect(await sealMedia(frame, 'delta', null, seal)).toBeNull()
    expect(await openMedia(frame, 'key', null, open)).toBeNull()
    expect(await sealMedia(Uint8Array.from(fill(50, 1)), 'key', 'h264', seal)).toBeNull()
    expect(await openMedia(Uint8Array.from(fill(50, 1)), 'key', 'h264', open)).toBeNull()
    expect(await sealMedia(annexB(sps, pps), 'key', 'h264', seal)).toBeNull()
  })

  it('keeps a long slice header readable, however many macroblocks the first slice skips', async () => {
    const { seal, open } = await ends()
    const bits = (n: number) => { const b = (n + 1).toString(2); return '0'.repeat(b.length - 1) + b }
    const asBytes = (text: string) => {
      const padded = text + '1'.repeat((8 - (text.length % 8)) % 8)
      return Array.from({ length: padded.length / 8 }, (_, i) => Number.parseInt(padded.slice(i * 8, i * 8 + 8), 2))
    }
    // A 4K picture split into slices: first_mb_in_slice reaches 32399, which is 29 bits.
    const header = asBytes(bits(32_399) + bits(7) + bits(200))
    expect(header.length).toBe(Math.ceil((29 + 7 + 15) / 8))
    const slice = [0x41, ...header, ...fill(200, 9)]
    const frame = annexB([0x67, 1, 2], [0x68, 3], slice)
    const sealed = await sealMedia(frame, 'delta', 'h264', seal)
    const cut = nals(sealed ?? new Uint8Array())
    expect([...(cut[2] ?? new Uint8Array()).subarray(0, 1 + header.length)]).toEqual(slice.slice(0, 1 + header.length))
    expect(cut[2]?.[1 + header.length]).not.toBe(slice[1 + header.length])
    expect(await openMedia(sealed ?? new Uint8Array(), 'delta', 'h264', open)).toEqual(frame)
  })

  it('counts emulation prevention bytes inside the slice header', async () => {
    const { seal, open } = await ends()
    // 23 leading zeros: the header holds 00 00 01, which is stored as 00 00 03 01.
    const escaped = [0x00, 0x00, 0x03, 0x01, 0x00, 0x00, 0x03, 0x00, 0xff, 0xff]
    const slice = [0x41, ...escaped, ...fill(120, 5)]
    const frame = annexB([0x67, 1, 2], [0x68, 3], slice)
    const sealed = await sealMedia(frame, 'delta', 'h264', seal)
    const cut = nals(sealed ?? new Uint8Array())
    // 23 + 1 + 23 bits of first_mb_in_slice, then two single-bit values: 49 bits, 7 bytes.
    const readable = 1 + 7 + 2
    expect([...(cut[2] ?? new Uint8Array()).subarray(0, readable)]).toEqual(slice.slice(0, readable))
    expect(await openMedia(sealed ?? new Uint8Array(), 'delta', 'h264', open)).toEqual(frame)
  })

  it('refuses a slice header that does not fit', async () => {
    const { seal } = await ends()
    expect(await sealMedia(annexB([0x41, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1]), 'delta', 'h264', seal)).toBeNull()
    expect(await sealMedia(annexB([0x41, 0x00, 0x00]), 'delta', 'h264', seal)).toBeNull()
  })
})

describe('authenticating everything a frame leaves readable', () => {
  const idr = [0x65, 0x88, 0x84, 0x21, ...fill(120, 3)]
  const other = [0x61, 0xe0, 0x00, 0x7e, ...fill(90, 8)]

  it('removes SEI before sealing and refuses it on receipt', async () => {
    const { seal, open } = await ends()
    const sei = [0x06, 0x05, 0x02, 0xaa, 0xbb]
    const sealed = await sealMedia(annexB(sps, pps, sei, idr), 'key', 'h264', seal)
    const types = nals(sealed ?? new Uint8Array()).map(nal => (nal[0] ?? 0) & 0x1f)
    expect(types).toEqual([7, 8, 5])
    expect(await openMedia(sealed ?? new Uint8Array(), 'key', 'h264', open)).toEqual(annexB(sps, pps, idr))
    const plain = await sealMedia(annexB(sps, idr), 'key', 'h264', seal)
    const injected = Uint8Array.from([...(plain ?? []), ...START, ...sei])
    expect(await openMedia(injected, 'key', 'h264', open)).toBeNull()
  })

  it('allows only slice, IDR, SPS, PPS and delimiter NAL units', async () => {
    const { seal, open } = await ends()
    for (const header of [0x62, 0x63, 0x64, 0x4e, 0x4a, 0x4c, 0x7e, 0xe5]) {
      expect(await sealMedia(annexB(sps, [header, 1, 2, 3], idr), 'key', 'h264', seal)).toBeNull()
    }
    const sealed = await sealMedia(annexB(sps, idr), 'key', 'h264', seal)
    const withPartition = Uint8Array.from([...(sealed ?? []), ...START, 0x62, 1, 2, 3])
    expect(await openMedia(withPartition, 'key', 'h264', open)).toBeNull()
  })

  it('refuses a frame made only of readable NAL units', async () => {
    const { open } = await ends()
    expect(await openMedia(annexB(sps, pps), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(annexB(sps, pps, [0x09, 0xf0]), 'delta', 'h264', open)).toBeNull()
  })

  it('fails to open when any readable part is changed, added, removed or moved', async () => {
    const { seal, open } = await ends()
    const frame = annexB([0x09, 0xf0], sps, pps, idr, other)
    const sealed = await sealMedia(frame, 'key', 'h264', seal)
    if (!sealed) throw new Error('expected a sealed frame')
    expect(await openMedia(sealed, 'key', 'h264', open)).toEqual(frame)
    const cut = nals(sealed)
    const rebuild = (parts: Uint8Array[]) => Uint8Array.from(parts.flatMap(part => [...START, ...part]))

    const changedSps = cut.map(nal => nal.slice())
    changedSps[1]![3] = (changedSps[1]![3] ?? 0) ^ 1
    expect(await openMedia(rebuild(changedSps), 'key', 'h264', open)).toBeNull()

    expect(await openMedia(rebuild([...cut, Uint8Array.of(0x09, 0xf0)]), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(rebuild(cut.filter((_, i) => i !== 2)), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(rebuild(cut.filter((_, i) => i !== 0)), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(rebuild([cut[0]!, cut[2]!, cut[1]!, ...cut.slice(3)]), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(rebuild([...cut.slice(0, 3), cut[4]!, cut[3]!]), 'key', 'h264', open)).toBeNull()

    // The sealed body of one picture NAL cannot stand in for another's.
    const swapped = cut.map(nal => nal.slice())
    const a = swapped[3]!, b = swapped[4]!
    const bodyA = a.slice(3), bodyB = b.slice(2)
    swapped[3] = Uint8Array.from([...a.subarray(0, 3), ...bodyB])
    swapped[4] = Uint8Array.from([...b.subarray(0, 2), ...bodyA])
    expect(await openMedia(rebuild(swapped), 'key', 'h264', open)).toBeNull()

    // Not even the forbidden_zero_bit.
    const forbidden = cut.map(nal => nal.slice())
    forbidden[1]![0] = 0xe7
    expect(await openMedia(rebuild(forbidden), 'key', 'h264', open)).toBeNull()
  })

  it('does not let the kind or the codec select a weaker framing', async () => {
    const { seal, open } = await ends()
    const frame = annexB(sps, idr)
    const sealed = await sealMedia(frame, 'key', 'h264', seal)
    expect(await openMedia(sealed ?? new Uint8Array(), 'key', 'vp8', open)).toBeNull()
    expect(await openMedia(sealed ?? new Uint8Array(), 'delta', 'vp8', open)).toBeNull()
    const vp8 = Uint8Array.from([0x50, 0x42, 0x00, 0x9d, 0x01, 0x2a, 0x80, 0x02, 0x68, 0x01, ...fill(100, 2)])
    const sealedVp8 = await sealMedia(vp8, 'key', 'vp8', seal)
    expect(await openMedia(sealedVp8 ?? new Uint8Array(), 'delta', 'vp8', open)).toBeNull()
    expect(await openMedia(sealedVp8 ?? new Uint8Array(), 'key', 'h264', open)).toBeNull()
    expect(await openMedia(sealedVp8 ?? new Uint8Array(), undefined, null, open)).toBeNull()
  })
})
