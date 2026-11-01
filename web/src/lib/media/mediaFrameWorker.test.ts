import { webcrypto } from 'node:crypto'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { requireValue } from '../../test-fixtures.fixture'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys, type FrameSeal } from './frame'
import { testAesKey, testEpochKeys, testMember, type TestMember } from './keys.fixture'
import { PREVIOUS_EPOCH_MS, PREVIOUS_GROUP_MS, SEND_GRACE_MS, STALE_SEAL_MS } from './bounds'

const previous = globalThis.crypto
let alice: TestMember
let bob: TestMember
let counter = 0

beforeAll(async () => {
  alice = await testMember()
  bob = await testMember()
})

/** Epoch keys as alice holds them, with bob as the other member. */
async function epochKeys(byte: number): Promise<EpochKeys> {
  return testEpochKeys(await testAesKey(byte), alice, { alice, bob })
}

/** Seals as a sending transform would: one stream, counting up. */
function seal(frame: Omit<FrameSeal, 'stream' | 'counter' | 'source'> & { source?: FrameSeal['source'] }) {
  return sealFrame({ source: 0, ...frame, stream: 7, counter: counter++ })
}

function open(keys: ReadonlyMap<number, EpochKeys>, packet: Uint8Array) {
  return openFrame({ keys, channelId: 'channel', senderId: 'alice', packet, replay: new ReplayWindow(), source: 0 })
}

interface WorkerScope {
  onrtctransform: ((event: { transformer?: unknown }) => void) | null
  postMessage?: (data: unknown) => void
}

function asBuffer(data: ArrayBufferLike): ArrayBuffer {
  const view = new Uint8Array(data)
  const copy = new Uint8Array(view.byteLength)
  copy.set(view)
  return copy.buffer
}

async function frames(direction: 'send' | 'recv', packets: ArrayBufferLike[], commands: unknown[], { named = true, type, mime = 'video/H264', afterCommands, layer, source = 0, posted, dtx }: { posted?: unknown[], named?: boolean, type?: string, mime?: string, afterCommands?: () => void, layer?: unknown, source?: unknown, dtx?: boolean } = {}) {
  vi.resetModules()
  const scope: WorkerScope = { onrtctransform: null, postMessage(data) { posted?.push(data) } }
  vi.stubGlobal('self', scope)
  vi.stubGlobal('crypto', webcrypto)
  await import('./mediaFrameWorker')
  const received: unknown[] = []
  const port = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    postMessage(data: unknown) { port.onmessage?.({ data }) },
  }
  let enqueueFrame: (frame: { data: ArrayBuffer, type?: string, getMetadata?: () => unknown }) => void = () => {}
  let closeStream: () => void = () => {}
  const readable = new ReadableStream<{ data: ArrayBuffer, type?: string, getMetadata?: () => unknown }>({
    start(controller) {
      enqueueFrame = (frame) => { controller.enqueue(frame) }
      closeStream = () => { controller.close() }
    },
  })
  let resolveDone = () => {}
  const done = new Promise<void>(resolve => { resolveDone = resolve })
  const writable = new WritableStream({
    write(frame) { received.push(frame) },
    close() { resolveDone() },
  })
  scope.onrtctransform?.({})
  scope.onrtctransform?.({ transformer: { readable, writable, options: { port, direction, channelId: 'channel', ...(source === undefined ? {} : { source }), ...(named ? { senderId: 'alice' } : {}), ...(layer === undefined ? {} : { layer }), ...(dtx ? { dtx } : {}) } } })
  for (const command of commands) port.postMessage(command)
  afterCommands?.()
  for (const data of packets) enqueueFrame({ data: asBuffer(data), ...(type ? { type, getMetadata: () => ({ mimeType: mime }) } : {}) })
  closeStream()
  await done
  return received
}

beforeEach(() => { vi.stubGlobal('crypto', webcrypto) })
afterEach(() => {
  vi.unstubAllGlobals()
  vi.stubGlobal('crypto', previous)
})

describe('media frame worker, Opus DTX', () => {
  const sizes = (written: unknown[]) => written.map(frame => (frame as { data: ArrayBuffer }).data.byteLength)

  it('drops the empty frames a DTX encoder hands over while silent, and seals the ones with sound', async () => {
    const key = { type: 'key', epoch: 2, key: await epochKeys(3) }
    const packets = [new Uint8Array([0xf8]).buffer, new Uint8Array([0xf8, 0xff]).buffer, new Uint8Array([0xfc, 0xff, 0xfe]).buffer, new Uint8Array(16).fill(7).buffer]
    const written = await frames('send', packets, [key], { dtx: true })
    // The overhead of a sealed frame: header, nonce, tag and signature.
    expect(sizes(written)).toEqual([3 + 107, 16 + 107])
    // A line that did not negotiate DTX seals everything it is given.
    expect(sizes(await frames('send', packets, [key]))).toEqual([1 + 107, 2 + 107, 3 + 107, 16 + 107])
  })

  it('never drops what a line receives, however small', async () => {
    const key = await epochKeys(3)
    const packet = await seal({ keys: key, epoch: 2, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([9]) })
    const received = await frames('recv', [requireValue(packet).buffer], [{ type: 'key', epoch: 2, key }], { dtx: true })
    expect(received).toHaveLength(1)
  })
})

describe('media frame worker', () => {
  it('drops frames until a key exists and then seals them', async () => {
    const early = await frames('send', [new Uint8Array([1, 2, 3]).buffer], [])
    expect(early).toEqual([])
    const key = await epochKeys(3)
    const written = await frames('send', [new Uint8Array([1, 2, 3]).buffer], [
      null,
      { type: 'nope' },
      { type: 'key', epoch: -1, key },
      { type: 'key', epoch: 1.5, key },
      { type: 'key', epoch: 2, key },
    ])
    expect(written).toHaveLength(1)
    const frame = written[0] as { data: ArrayBuffer }
    const opened = await open(new Map([[2, key]]), new Uint8Array(frame.data))
    expect(opened).toEqual(new Uint8Array([1, 2, 3]))
  })

  it('opens a matching frame and drops a foreign or forgotten one', async () => {
    const key = await epochKeys(5)
    const older = await epochKeys(4)
    const newest = await epochKeys(6)
    const packet = await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
    if (!packet) throw new Error('expected a sealed frame')
    const opened = await frames('recv', [packet.buffer, new Uint8Array([9]).buffer], [{ type: 'key', epoch: 1, key }])
    expect(opened).toHaveLength(1)
    expect(new Uint8Array((opened[0] as { data: ArrayBuffer }).data)).toEqual(new Uint8Array([4]))
    const forgotten = await frames('recv', [packet.buffer], [
      { type: 'key', epoch: 1, key },
      { type: 'drop' },
    ])
    expect(forgotten).toEqual([])
    const rotated = await frames('send', [new Uint8Array([1]).buffer], [
      { type: 'key', epoch: 1, key: older },
      { type: 'key', epoch: 2, key },
      { type: 'key', epoch: 3, key: newest },
    ])
    expect(rotated).toHaveLength(1)
    const sealed = await open(new Map([[3, newest]]), new Uint8Array((rotated[0] as { data: ArrayBuffer }).data))
    expect(sealed).toEqual(new Uint8Array([1]))
    const replacedKey = await epochKeys(9)
    const replaced = await frames('send', [new Uint8Array([8]).buffer], [
      { type: 'key', epoch: 5, key: older },
      { type: 'key', epoch: 6, key: newest },
      { type: 'key', epoch: 0, key: replacedKey, replace: true },
    ])
    expect(replaced).toHaveLength(1)
    expect(await open(new Map([[0, replacedKey]]), new Uint8Array((replaced[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([8]))
    const rewound = await frames('send', [new Uint8Array([7]).buffer], [
      { type: 'key', epoch: 5, key: newest },
      { type: 'key', epoch: 1, key: replacedKey },
    ])
    expect(rewound).toHaveLength(1)
    expect(await open(new Map([[1, replacedKey]]), new Uint8Array((rewound[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([7]))
    const ignored = await frames('send', [], [])
    expect(ignored).toEqual([])
    vi.resetModules()
    const scope: WorkerScope = { onrtctransform: null }
    vi.stubGlobal('self', scope)
    await import('./mediaFrameWorker')
    scope.onrtctransform?.({ transformer: { readable: new ReadableStream(), writable: new WritableStream(), options: { direction: 'send', channelId: '', senderId: 'alice' } } })
    scope.onrtctransform?.({ transformer: { readable: new ReadableStream(), writable: new WritableStream(), options: { port: { onmessage: null }, direction: 'sideways', channelId: 'channel', senderId: 'alice' } } })
  })

  it('keeps only the two newest epoch keys', async () => {
    const keys = [await epochKeys(1), await epochKeys(2), await epochKeys(3)]
    const sealedAt = async (epoch: number) => (await seal({ keys: keys[epoch - 1] as EpochKeys, epoch, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([epoch]) }))?.buffer as ArrayBuffer
    const opened = await frames('recv', [await sealedAt(1), await sealedAt(2), await sealedAt(3)], [
      { type: 'key', epoch: 1, key: keys[0] },
      { type: 'key', epoch: 2, key: keys[1] },
      { type: 'key', epoch: 3, key: keys[2] },
    ])
    const plain = opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])
    expect(plain).toEqual([[2], [3]])
  })

  it('keeps a video frame parseable and opens it again', async () => {
    const key = await epochKeys(2)
    const nal = [0x61, 0xe0, 0x00, 0x7e, ...Array.from({ length: 64 }, (_, i) => i + 1)]
    const frame = Uint8Array.from([0, 0, 0, 1, ...nal])
    const sealed = await frames('send', [frame.buffer], [{ type: 'key', epoch: 1, key }], { type: 'delta' })
    const written = new Uint8Array((sealed[0] as { data: ArrayBuffer }).data)
    expect([...written.subarray(0, 6)]).toEqual([0, 0, 0, 1, 0x61, 0xe0])
    const opened = await frames('recv', [written.buffer], [{ type: 'key', epoch: 1, key }], { type: 'delta' })
    expect(new Uint8Array((opened[0] as { data: ArrayBuffer }).data)).toEqual(frame)
  })

  it('counts, per publisher, frames it opened and frames from an epoch newer than any key', async () => {
    // The worker reads the monotonic clock.
    let at = 5000
    vi.spyOn(performance, 'now').mockImplementation(() => at)
    const key = await epochKeys(5)
    const sealedAt = async (epoch: number, senderId = 'alice') => {
      const packet = await seal({ keys: key, epoch, channelId: 'channel', senderId, plaintext: new Uint8Array([4]) })
      if (!packet) throw new Error('expected a sealed frame')
      return packet.buffer
    }
    const posted: unknown[] = []
    vi.resetModules()
    const scope = { onrtctransform: null, postMessage: (data: unknown) => { posted.push(data) } } as unknown as WorkerScope
    vi.stubGlobal('self', scope)
    vi.stubGlobal('crypto', webcrypto)
    await import('./mediaFrameWorker')
    const port = { onmessage: null as ((event: { data: unknown }) => void) | null }
    let enqueue: (frame: { data: ArrayBuffer }) => void = () => {}
    let close: () => void = () => {}
    const readable = new ReadableStream<{ data: ArrayBuffer }>({ start(controller) { enqueue = frame => controller.enqueue(frame); close = () => controller.close() } })
    let done: () => void = () => {}
    const finished = new Promise<void>((resolve) => { done = resolve })
    scope.onrtctransform?.({ transformer: { readable, writable: new WritableStream({ close() { done() } }), options: { port, direction: 'recv', channelId: 'channel', senderId: 'alice', source: 0 } } })
    port.onmessage?.({ data: { type: 'key', epoch: 2, key } })
    // An old epoch counts for nothing; a newer one and an opened one count, once a second.
    enqueue({ data: asBuffer(await sealedAt(1)) })
    enqueue({ data: asBuffer(await sealedAt(3)) })
    await vi.waitFor(() => expect(posted).toEqual([{ type: 'stats', senders: { alice: { opened: 0, newer: 1, epoch: 3, legacy: 0 } } }]))
    enqueue({ data: asBuffer(await sealedAt(2)) })
    enqueue({ data: asBuffer(await sealedAt(5)) })
    await new Promise(resolve => setTimeout(resolve, 10))
    expect(posted).toHaveLength(1)
    at += 1500
    enqueue({ data: asBuffer(await sealedAt(2)) })
    await vi.waitFor(() => expect(posted).toHaveLength(2))
    expect(posted[1]).toEqual({ type: 'stats', senders: { alice: { opened: 2, newer: 1, epoch: 5, legacy: 0 } } })
    close()
    await finished
    vi.mocked(performance.now).mockRestore()
  })

  it('drops video whose codec is unknown or whose type is not a picture', async () => {
    const key = await epochKeys(2)
    const frame = Uint8Array.from([0, 0, 0, 1, 0x61, 0xe0, 0x00, 0x7e, ...Array.from({ length: 32 }, (_, i) => i + 1)])
    const keyed = [{ type: 'key', epoch: 1, key }]
    expect(await frames('send', [frame.buffer], keyed, { type: 'delta', mime: 'video/AV1' })).toEqual([])
    expect(await frames('send', [frame.buffer], keyed, { type: 'delta', mime: 'video/VP9' })).toEqual([])
    expect(await frames('send', [frame.buffer], keyed, { type: 'empty' })).toEqual([])
    expect(await frames('recv', [frame.buffer], keyed, { type: 'delta', mime: 'video/H265' })).toEqual([])
    const vp8 = await frames('send', [frame.buffer], keyed, { type: 'delta', mime: 'video/VP8' })
    expect([...new Uint8Array((vp8[0] as { data: ArrayBuffer }).data).subarray(0, 3)]).toEqual([0, 0, 0])
  })

  it('opens nothing on a receiver until it is told who publishes on it', async () => {
    const key = await epochKeys(5)
    const packet = await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
    if (!packet) throw new Error('expected a sealed frame')
    const keyed = { type: 'key', epoch: 1, key }
    expect(await frames('recv', [packet.buffer], [keyed], { named: false })).toEqual([])
    const named = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: 'alice', source: 0 }], { named: false })
    expect(new Uint8Array((named[0] as { data: ArrayBuffer }).data)).toEqual(new Uint8Array([4]))
    const other = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: 'bob' }], { named: false })
    expect(other).toEqual([])
    const ignored = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: '' }], { named: false })
    expect(ignored).toEqual([])
    // A sender is always named at the start; a message cannot rename it.
    const renamed = await frames('send', [new Uint8Array([1]).buffer], [keyed, { type: 'sender', senderId: 'mallory' }])
    expect(await open(new Map([[1, key]]), new Uint8Array((renamed[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([1]))
  })

  it('opens a unit once per line and only from the line’s publisher', async () => {
    const key = await epochKeys(5)
    const packet = await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
    const later = await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([5]) })
    if (!packet || !later) throw new Error('expected sealed frames')
    const keyed = [{ type: 'key', epoch: 1, key }]
    // A replayed unit opens once; an earlier one still opens inside the window.
    const opened = await frames('recv', [later.buffer, packet.buffer, later.buffer, packet.buffer], keyed)
    expect(opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[5], [4]])
    // Bob shares the AES key but signs with his own leaf key.
    const fromBob = await sealFrame({
      keys: testEpochKeys(key.key, bob, { alice, bob }), epoch: 1, stream: 3, counter: 0, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([6]), source: 0,
    })
    if (!fromBob) throw new Error('expected a sealed frame')
    expect(await frames('recv', [fromBob.buffer], keyed)).toEqual([])
    expect(await frames('recv', [packet.buffer], [{ type: 'key', epoch: 1, key: { ...key, verifiers: new Map() } }])).toEqual([])
    expect(await frames('recv', [packet.buffer], [{ type: 'key', epoch: 1, key: { key: key.key } }])).toEqual([])
  })

  it('numbers the units of each sending transform as its own stream', async () => {
    const key = await epochKeys(5)
    const keyed = [{ type: 'key', epoch: 1, key }]
    const first = await frames('send', [new Uint8Array([1]).buffer, new Uint8Array([2]).buffer], keyed)
    const second = await frames('send', [new Uint8Array([3]).buffer], keyed)
    const header = (frame: unknown) => {
      const view = new DataView((frame as { data: ArrayBuffer }).data)
      return { stream: view.getUint32(6), counter: view.getUint32(10) }
    }
    const [a, b] = first.map(header)
    const [c] = second.map(header)
    expect([a?.counter, b?.counter, c?.counter]).toEqual([0, 1, 0])
    expect(a?.stream).toBe(b?.stream)
    // Both open on one line: a restarted sender is a new stream, not a replay.
    const opened = await frames('recv', [...first, ...second].map(frame => (frame as { data: ArrayBuffer }).data), keyed)
    expect(opened).toHaveLength(3)
  })

  it('opens a unit once per publisher, across lines and recreated receivers', async () => {
    const key = await epochKeys(5)
    const packet = await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
    if (!packet) throw new Error('expected a sealed frame')
    vi.resetModules()
    const scope: WorkerScope = { onrtctransform: null, postMessage() {} }
    vi.stubGlobal('self', scope)
    await import('./mediaFrameWorker')
    // One worker serves every line of the call, as the guard sets it up.
    const line = async (packets: ArrayBuffer[], first = false) => {
      const written: unknown[] = []
      const port = { onmessage: null as ((event: { data: unknown }) => void) | null }
      let done: () => void = () => {}
      const finished = new Promise<void>((resolve) => { done = resolve })
      let push: (data: ArrayBuffer) => void = () => {}
      let end: () => void = () => {}
      const readable = new ReadableStream({ start(controller) { push = data => controller.enqueue({ data }); end = () => controller.close() } })
      scope.onrtctransform?.({ transformer: { readable, writable: new WritableStream({ write(chunk) { written.push(chunk) }, close() { done() } }), options: { port, direction: 'recv', channelId: 'channel', senderId: 'alice', source: 0 } } })
      if (first) port.onmessage?.({ data: { type: 'key', epoch: 1, key } })
      for (const data of packets) push(data)
      end()
      await finished
      return written
    }
    expect(await line([asBuffer(packet.buffer)], true)).toHaveLength(1)
    // The same unit on the publisher's other line, or on a receiver created
    // again for the same line, is a replay.
    expect(await line([asBuffer(packet.buffer)])).toEqual([])
    expect(await line([asBuffer(packet.buffer)])).toEqual([])
  })

  it('stops sealing when a stream runs out of counters', async () => {
    vi.resetModules()
    const frame = await import('./frame')
    const spent = vi.spyOn(frame.FrameStream.prototype, 'next').mockReturnValue(null)
    const key = await epochKeys(5)
    const scope: WorkerScope = { onrtctransform: null, postMessage() {} }
    vi.stubGlobal('self', scope)
    await import('./mediaFrameWorker')
    const written: unknown[] = []
    const port = { onmessage: null as ((event: { data: unknown }) => void) | null }
    let done: () => void = () => {}
    const finished = new Promise<void>((resolve) => { done = resolve })
    const readable = new ReadableStream({ start(controller) { controller.enqueue({ data: new Uint8Array([1]).buffer }); controller.close() } })
    scope.onrtctransform?.({ transformer: { readable, writable: new WritableStream({ write(chunk) { written.push(chunk) }, close() { done() } }), options: { port, direction: 'send', channelId: 'channel', senderId: 'alice', source: 0 } } })
    port.onmessage?.({ data: { type: 'key', epoch: 1, key } })
    await finished
    expect(written).toEqual([])
    spent.mockRestore()
  })

  it('drops a frame the key cannot seal and a stream that fails', async () => {
    const decryptOnly = await crypto.subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['decrypt'])
    const dropped = await frames('send', [new Uint8Array([1]).buffer], [{ type: 'key', epoch: 4, key: { ...await epochKeys(1), key: decryptOnly } }])
    expect(dropped).toEqual([])
    vi.resetModules()
    const scope: WorkerScope = { onrtctransform: null }
    vi.stubGlobal('self', scope)
    await import('./mediaFrameWorker')
    const port = { onmessage: null as ((event: { data: unknown }) => void) | null }
    const readable = new ReadableStream({
      start(controller) { controller.close() },
    })
    const writable = new WritableStream({
      start(controller) { controller.error(new Error('gone')) },
    })
    scope.onrtctransform?.({
      transformer: { readable, writable, options: { port, direction: 'recv', channelId: 'channel', senderId: 'alice' } },
    })
    await new Promise(resolve => setTimeout(resolve, 10))
  })

  describe('across the epochs of one group', () => {
    async function sealedAt(keys: EpochKeys, epoch: number, byte: number) {
      const packet = await seal({ keys, epoch, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([byte]) })
      if (!packet) throw new Error('expected a sealed frame')
      return packet.buffer
    }

    function bytes(frames: unknown[]) {
      return frames.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])
    }

    it('opens the previous epoch only for a short deadline after the switch, then forgets its key', async () => {
      const first = await epochKeys(31)
      const second = await epochKeys(32)
      const now = vi.spyOn(performance, 'now')
      let clock = 3_000_000
      now.mockImplementation(() => clock)
      const keyed = [{ type: 'key', epoch: 1, key: first, seq: 1 }, { type: 'key', epoch: 2, key: second, seq: 2 }]
      const packets = async () => [await sealedAt(first, 1, 1), await sealedAt(second, 2, 2)]
      // Just inside the deadline both open (a commit still on its way to the slowest member).
      expect(bytes(await frames('recv', await packets(), keyed, { afterCommands: () => { clock += PREVIOUS_EPOCH_MS - 1 } }))).toEqual([[1], [2]])
      // After it the old epoch's key is gone: a member removed by the switch can inject nothing sealed under it.
      expect(bytes(await frames('recv', await packets(), keyed, { afterCommands: () => { clock += PREVIOUS_EPOCH_MS } }))).toEqual([[2]])
      now.mockRestore()
    })

    it('does not extend the deadline of an older epoch when yet another one arrives', async () => {
      const keys = [await epochKeys(33), await epochKeys(34), await epochKeys(35)]
      const now = vi.spyOn(performance, 'now')
      now.mockReturnValue(4_000_000)
      const opened = await frames('recv', [await sealedAt(keys[1] as EpochKeys, 2, 2), await sealedAt(keys[0] as EpochKeys, 1, 1), await sealedAt(keys[2] as EpochKeys, 3, 3)], [
        { type: 'key', epoch: 1, key: keys[0], seq: 1 },
        { type: 'key', epoch: 2, key: keys[1], seq: 2 },
        { type: 'key', epoch: 3, key: keys[2], seq: 3 },
      ])
      // Only the epoch right before the newest is held at all.
      expect(bytes(opened)).toEqual([[2], [3]])
      now.mockRestore()
    })

    it('seals under the epoch it left for a moment after a commit that removed nobody, never longer than the grace', async () => {
      const first = await epochKeys(36)
      const second = await epochKeys(37)
      const now = vi.spyOn(performance, 'now')
      let clock = 5_000_000
      now.mockImplementation(() => clock)
      const commands = [{ type: 'key', epoch: 1, key: first, seq: 1 }, { type: 'key', epoch: 2, key: second, soft: true, seq: 2 }]
      const during = await frames('send', [new Uint8Array([1]).buffer], commands, { afterCommands: () => { clock += SEND_GRACE_MS - 1 } })
      expect(await open(new Map([[1, first]]), new Uint8Array((during[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([1]))
      const after = await frames('send', [new Uint8Array([1]).buffer], commands, { afterCommands: () => { clock += SEND_GRACE_MS } })
      expect(await open(new Map([[2, second]]), new Uint8Array((after[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([1]))
      now.mockRestore()
    })

    it('seals under the new epoch at once after a removal or a new group, and after the old one expired', async () => {
      const first = await epochKeys(38)
      const second = await epochKeys(39)
      const removal = await frames('send', [new Uint8Array([2]).buffer], [{ type: 'key', epoch: 1, key: first, seq: 1 }, { type: 'key', epoch: 2, key: second, seq: 2 }])
      expect(await open(new Map([[2, second]]), new Uint8Array((removal[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([2]))
      const regrouped = await frames('send', [new Uint8Array([3]).buffer], [{ type: 'key', epoch: 1, key: first, seq: 1 }, { type: 'key', epoch: 0, key: second, soft: true, replace: true, seq: 2 }])
      expect(await open(new Map([[0, second]]), new Uint8Array((regrouped[0] as { data: ArrayBuffer }).data))).toEqual(new Uint8Array([3]))
    })
  })

  describe('while the call regroups', () => {
    async function sealedAt(keys: EpochKeys, epoch: number, byte: number) {
      const packet = await seal({ keys, epoch, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([byte]) })
      if (!packet) throw new Error('expected a sealed frame')
      return packet.buffer
    }

    it('opens frames under the previous group\'s keys, also on the same epoch number', async () => {
      const old = await epochKeys(21)
      const fresh = await epochKeys(22)
      const opened = await frames('recv', [await sealedAt(old, 3, 1), await sealedAt(old, 1, 2), await sealedAt(fresh, 1, 3)], [
        { type: 'key', epoch: 1, key: old, seq: 1 },
        { type: 'key', epoch: 3, key: old, seq: 2 },
        { type: 'key', epoch: 1, key: fresh, replace: true, seq: 3 },
      ])
      expect(opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[1], [2], [3]])
    })

    it('applies a key message once, however many lines deliver it', async () => {
      const old = await epochKeys(23)
      const fresh = await epochKeys(24)
      const replaced = { type: 'key', epoch: 0, key: fresh, replace: true, seq: 2 }
      const opened = await frames('recv', [await sealedAt(old, 5, 4)], [
        { type: 'key', epoch: 5, key: old, seq: 1 },
        replaced,
        replaced,
        { ...replaced, seq: 1 },
      ])
      expect(opened).toHaveLength(1)
    })

    it('stops opening the previous group\'s frames after a while', async () => {
      const old = await epochKeys(25)
      const fresh = await epochKeys(26)
      const now = vi.spyOn(performance, 'now')
      let clock = 1_000_000
      now.mockImplementation(() => clock)
      const opened = await frames('recv', [await sealedAt(old, 2, 5), await sealedAt(fresh, 0, 6)], [
        { type: 'key', epoch: 2, key: old, seq: 1 },
        { type: 'key', epoch: 0, key: fresh, replace: true, seq: 2 },
      ], { afterCommands: () => { clock += PREVIOUS_GROUP_MS + 1 } })
      now.mockRestore()
      expect(opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[6]])
    })

    it('seals with the last keys only a little while after the group is gone, until a new key comes', async () => {
      const old = await epochKeys(27)
      const now = vi.spyOn(performance, 'now')
      let clock = 2_000_000
      now.mockImplementation(() => clock)
      const keyed = [{ type: 'key', epoch: 3, key: old, seq: 1 }, { type: 'stale' }]
      expect(await frames('send', [new Uint8Array([1]).buffer], keyed)).toHaveLength(1)
      expect(await frames('send', [new Uint8Array([1]).buffer], keyed, { afterCommands: () => { clock += STALE_SEAL_MS + 1 } })).toEqual([])
      const fresh = await epochKeys(28)
      expect(await frames('send', [new Uint8Array([1]).buffer], [...keyed, { type: 'key', epoch: 0, key: fresh, replace: true, seq: 2 }], { afterCommands: () => { clock += STALE_SEAL_MS + 1 } })).toHaveLength(1)
      now.mockRestore()
    })
  })
})

describe('media frame worker, layers of a screen share', () => {
  const streamOf = (written: unknown[]) => written.map(frame => new DataView((frame as { data: ArrayBuffer }).data).getUint32(6))
  const payloads = [new Uint8Array([1]).buffer, new Uint8Array([2]).buffer, new Uint8Array([3]).buffer]

  it('seals each layer’s line in a stream that carries the layer', async () => {
    const key = await epochKeys(40)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    const streams: number[][] = []
    for (const layer of [0, 1, 2]) {
      streams.push(streamOf(await frames('send', payloads, keyed, { layer })))
    }
    for (const [layer, ids] of streams.entries()) {
      // One stream per line, and its low bits name the layer.
      expect(new Set(ids).size).toBe(1)
      expect((ids[0] ?? 0) & 3).toBe(layer)
    }
    expect(new Set(streams.map(ids => ids[0])).size).toBe(3)
  })

  it('treats anything but a layer number as no layer', async () => {
    const key = await epochKeys(41)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    for (const layer of [4, -1, 1.5, '1', null, Number.NaN]) {
      expect(streamOf(await frames('send', payloads.slice(0, 1), keyed, { layer }))).toHaveLength(1)
    }
    // Layers 0 to 3 fit the two bits.
    expect((streamOf(await frames('send', payloads.slice(0, 1), keyed, { layer: 3 }))[0] ?? 0) & 3).toBe(3)
  })

  it('opens frames of different layers of one sender as they come', async () => {
    const key = await epochKeys(42)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    const sealedBy = async (layer: number) => {
      const out = await frames('send', [new Uint8Array([layer, 9]).buffer], keyed, { layer })
      return new Uint8Array((out[0] as { data: ArrayBuffer }).data)
    }
    const packets = [await sealedBy(0), await sealedBy(2), await sealedBy(1), await sealedBy(2)]
    const opened = await frames('recv', packets.map(packet => asBuffer(packet.buffer)), keyed)
    expect(opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[0, 9], [2, 9], [1, 9], [2, 9]])
  })
})

describe('media frame worker, layers of a camera', () => {
  const streamOf = (written: unknown[]) => written.map(frame => new DataView((frame as { data: ArrayBuffer }).data).getUint32(6))
  const lineOf = (written: unknown[]) => new Uint8Array((written[0] as { data: ArrayBuffer }).data)[14]
  const payload = [new Uint8Array([1]).buffer]

  // The line byte keeps naming the camera, as every receiver checks it; the layer lives in the stream
  // only, so an older viewer that knows layers of screens alone still opens a camera layer.
  it('seals each layer in a stream of its own on the unchanged camera line', async () => {
    const key = await epochKeys(60)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    const streams: number[] = []
    for (const layer of [0, 1, 2]) {
      const out = await frames('send', payload, keyed, { layer, source: 1 })
      expect(lineOf(out)).toBe(0x10)
      streams.push(streamOf(out)[0] ?? 0)
      expect((streams[layer] ?? 0) & 3).toBe(layer)
    }
    expect(new Set(streams).size).toBe(3)
  })

  it('opens frames of the layers of a camera as the server switches between them', async () => {
    const key = await epochKeys(61)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    const sealedBy = async (layer: number) => {
      const out = await frames('send', [new Uint8Array([layer, 9]).buffer], keyed, { layer, source: 1 })
      return new Uint8Array((out[0] as { data: ArrayBuffer }).data)
    }
    const packets = [await sealedBy(2), await sealedBy(0), await sealedBy(2), await sealedBy(1)]
    const opened = await frames('recv', packets.map(packet => asBuffer(packet.buffer)), keyed, { source: 1 })
    expect(opened.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[2, 9], [0, 9], [2, 9], [1, 9]])
  })

  it('seals a camera with no layer as before: a stream of no particular layer', async () => {
    const key = await epochKeys(62)
    const keyed = [{ type: 'key', epoch: 2, key, seq: 1 }]
    const out = await frames('send', payload, keyed, { source: 1 })
    expect(lineOf(out)).toBe(0x10)
    expect(streamOf(out)).toHaveLength(1)
  })
})

describe('media frame worker, lines and the retired frame version', () => {
  const body = [new Uint8Array([6, 6]).buffer]
  const magic = async (keysAt: unknown[], over: Parameters<typeof frames>[3] = {}) => {
    const out = await frames('send', body, keysAt, over)
    return out.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data).subarray(0, 2)])
  }
  const V3 = [0x4d, 0x33]

  it('always seals version 3, whatever the key message says about its members', async () => {
    const key = await epochKeys(51)
    const next = await epochKeys(53)
    expect(await magic([{ type: 'key', epoch: 1, key, seq: 1 }])).toEqual([V3])
    // Capability fields of an older key message, and capability messages themselves, change nothing.
    expect(await magic([{ type: 'key', epoch: 1, key, seq: 1, caps: { members: ['alice'], complete: false } }])).toEqual([V3])
    expect(await magic([{ type: 'key', epoch: 1, key, seq: 1 }, { type: 'caps', epoch: 1, caps: { members: ['alice'], complete: false }, seq: 2 }])).toEqual([V3])
    expect(await magic([{ type: 'key', epoch: 1, key, seq: 1 }, { type: 'key', epoch: 2, key: next, seq: 2 }])).toEqual([V3])
    expect(await magic([{ type: 'key', epoch: 1, key, seq: 1 }, { type: 'key', epoch: 1, key: next, seq: 2, replace: true }])).toEqual([V3])
  })

  it('names the line it seals: microphone, camera, screen with its layer, screen sound', async () => {
    const key = await epochKeys(54)
    const keyed = [{ type: 'key', epoch: 1, key, seq: 1 }]
    const lineOf = async (over: Parameters<typeof frames>[3]) => {
      const out = await frames('send', body, keyed, over)
      return new Uint8Array((out[0] as { data: ArrayBuffer }).data)[14]
    }
    expect(await lineOf({ source: 0 })).toBe(0x00)
    expect(await lineOf({ source: 1 })).toBe(0x10)
    expect(await lineOf({ source: 2 })).toBe(0x20)
    expect(await lineOf({ source: 2, layer: 2 })).toBe(0x22)
    expect(await lineOf({ source: 3 })).toBe(0x30)
    // A line that does not say what it carries seals nothing; neither does an unknown source.
    for (const source of [null, 4, 'mic']) {
      vi.resetModules()
      const scope: WorkerScope = { onrtctransform: null, postMessage() {} }
      vi.stubGlobal('self', scope)
      await import('./mediaFrameWorker')
      const readable = new ReadableStream()
      const writable = new WritableStream()
      scope.onrtctransform?.({ transformer: { readable, writable, options: { port: { onmessage: null }, direction: 'send', channelId: 'channel', senderId: 'alice', source } } })
      expect(readable.locked).toBe(false)
    }
  })

  it('opens a unit only on the line it names, whoever relabelled it', async () => {
    const key = await epochKeys(55)
    const keyed = [{ type: 'key', epoch: 1, key, seq: 1 }]
    const made = async (source: 0 | 1 | 2 | 3, stream = 7) => {
      const packet = await sealFrame({ keys: key, epoch: 1, stream, counter: counter++, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([source]), source })
      return asBuffer((packet as Uint8Array).buffer)
    }
    const got = async (packet: ArrayBuffer, source: number) => (await frames('recv', [packet], keyed, { source })).length
    expect(await got(await made(0), 0)).toBe(1)
    expect(await got(await made(0), 3)).toBe(0)
    expect(await got(await made(1), 2)).toBe(0)
    expect(await got(await made(3), 0)).toBe(0)
    // A reused receiver is told its new line along with the publisher; without one, nothing opens.
    const screen = await made(2, 100)
    expect((await frames('recv', [screen], keyed, { named: false, source: null })).length).toBe(0)
    const told = [...keyed, { type: 'sender', senderId: 'alice', source: 2 }]
    expect((await frames('recv', [screen], told, { named: false, source: null })).length).toBe(1)
    expect((await frames('recv', [screen], [...keyed, { type: 'sender', senderId: 'alice', source: 0 }], { named: false, source: null })).length).toBe(0)
    expect((await frames('recv', [screen], [...keyed, { type: 'sender', senderId: 'alice' }], { named: false, source: null })).length).toBe(0)
  })

  it('drops version 2 units, reports them per publisher and goes on with version 3', async () => {
    const key = await epochKeys(56)
    const keyed = [{ type: 'key', epoch: 1, key, seq: 1 }]
    const v3 = asBuffer((await seal({ keys: key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([3]) }) as Uint8Array).buffer)
    // The retired layout: the line byte taken out, the version byte 2.
    const old = new Uint8Array(v3.byteLength - 1)
    old.set(new Uint8Array(v3, 0, 14))
    old.set(new Uint8Array(v3, 15), 14)
    old[1] = 0x32
    const posted: unknown[] = []
    const out = await frames('recv', [old.buffer, old.buffer, v3], keyed, { posted })
    expect(out.map(frame => [...new Uint8Array((frame as { data: ArrayBuffer }).data)])).toEqual([[3]])
    const stats = posted.find((message): message is { type: 'stats', senders: Record<string, { legacy: number }> } => (message as { type?: string }).type === 'stats')
    expect(stats?.senders.alice?.legacy).toBeGreaterThan(0)
  })
})
