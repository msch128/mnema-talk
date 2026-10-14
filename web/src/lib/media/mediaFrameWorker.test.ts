import { webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { importEpochKey, openFrame, sealFrame } from './frame'

const previous = globalThis.crypto

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

async function frames(direction: 'send' | 'recv', packets: ArrayBufferLike[], commands: unknown[], { named = true, type, mime = 'video/H264' }: { named?: boolean, type?: string, mime?: string } = {}) {
  vi.resetModules()
  const scope: WorkerScope = { onrtctransform: null, postMessage() {} }
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
  scope.onrtctransform?.({ transformer: { readable, writable, options: { port, direction, channelId: 'channel', ...(named ? { senderId: 'alice' } : {}) } } })
  for (const command of commands) port.postMessage(command)
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

describe('media frame worker', () => {
  it('drops frames until a key exists and then seals them', async () => {
    const early = await frames('send', [new Uint8Array([1, 2, 3]).buffer], [])
    expect(early).toEqual([])
    const key = await importEpochKey(new Uint8Array(16).fill(3))
    if (!key) throw new Error('expected an epoch key')
    const written = await frames('send', [new Uint8Array([1, 2, 3]).buffer], [
      null,
      { type: 'nope' },
      { type: 'key', epoch: -1, key },
      { type: 'key', epoch: 1.5, key },
      { type: 'key', epoch: 2, key },
    ])
    expect(written).toHaveLength(1)
    const frame = written[0] as { data: ArrayBuffer }
    const opened = await openFrame({
      keys: new Map([[2, key]]),
      channelId: 'channel',
      senderId: 'alice',
      packet: new Uint8Array(frame.data),
    })
    expect(opened).toEqual(new Uint8Array([1, 2, 3]))
  })

  it('opens a matching frame and drops a foreign or forgotten one', async () => {
    const key = await importEpochKey(new Uint8Array(16).fill(5))
    const older = await importEpochKey(new Uint8Array(16).fill(4))
    const newest = await importEpochKey(new Uint8Array(16).fill(6))
    if (!key || !older || !newest) throw new Error('expected epoch keys')
    const packet = await sealFrame({ key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
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
    const sealed = await openFrame({
      keys: new Map([[3, newest]]),
      channelId: 'channel',
      senderId: 'alice',
      packet: new Uint8Array((rotated[0] as { data: ArrayBuffer }).data),
    })
    expect(sealed).toEqual(new Uint8Array([1]))
    const replacedKey = await importEpochKey(new Uint8Array(16).fill(9))
    if (!replacedKey) throw new Error('expected an epoch key')
    const replaced = await frames('send', [new Uint8Array([8]).buffer], [
      { type: 'key', epoch: 5, key: older },
      { type: 'key', epoch: 6, key: newest },
      { type: 'key', epoch: 0, key: replacedKey, replace: true },
    ])
    expect(replaced).toHaveLength(1)
    expect(await openFrame({
      keys: new Map([[0, replacedKey]]),
      channelId: 'channel',
      senderId: 'alice',
      packet: new Uint8Array((replaced[0] as { data: ArrayBuffer }).data),
    })).toEqual(new Uint8Array([8]))
    const rewound = await frames('send', [new Uint8Array([7]).buffer], [
      { type: 'key', epoch: 5, key: newest },
      { type: 'key', epoch: 1, key: replacedKey },
    ])
    expect(rewound).toHaveLength(1)
    expect(await openFrame({
      keys: new Map([[1, replacedKey]]),
      channelId: 'channel',
      senderId: 'alice',
      packet: new Uint8Array((rewound[0] as { data: ArrayBuffer }).data),
    })).toEqual(new Uint8Array([7]))
    const ignored = await frames('send', [], [])
    expect(ignored).toEqual([])
    vi.resetModules()
    const scope: WorkerScope = { onrtctransform: null }
    vi.stubGlobal('self', scope)
    await import('./mediaFrameWorker')
    scope.onrtctransform?.({ transformer: { readable: new ReadableStream(), writable: new WritableStream(), options: { direction: 'send', channelId: '', senderId: 'alice' } } })
    scope.onrtctransform?.({ transformer: { readable: new ReadableStream(), writable: new WritableStream(), options: { port: { onmessage: null }, direction: 'sideways', channelId: 'channel', senderId: 'alice' } } })
  })

  it('keeps a video frame parseable and opens it again', async () => {
    const key = await importEpochKey(new Uint8Array(16).fill(2))
    if (!key) throw new Error('expected an epoch key')
    const nal = [0x61, 0xe0, 0x00, 0x7e, ...Array.from({ length: 64 }, (_, i) => i + 1)]
    const frame = Uint8Array.from([0, 0, 0, 1, ...nal])
    const sealed = await frames('send', [frame.buffer], [{ type: 'key', epoch: 1, key }], { type: 'delta' })
    const written = new Uint8Array((sealed[0] as { data: ArrayBuffer }).data)
    expect([...written.subarray(0, 6)]).toEqual([0, 0, 0, 1, 0x61, 0xe0])
    const opened = await frames('recv', [written.buffer], [{ type: 'key', epoch: 1, key }], { type: 'delta' })
    expect(new Uint8Array((opened[0] as { data: ArrayBuffer }).data)).toEqual(frame)
  })

  it('counts, per publisher, frames it opened and frames from an epoch newer than any key', async () => {
    vi.useFakeTimers({ toFake: ['Date'] })
    const key = await importEpochKey(new Uint8Array(16).fill(5))
    if (!key) throw new Error('expected an epoch key')
    const sealedAt = async (epoch: number, senderId = 'alice') => {
      const packet = await sealFrame({ key, epoch, channelId: 'channel', senderId, plaintext: new Uint8Array([4]) })
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
    scope.onrtctransform?.({ transformer: { readable, writable: new WritableStream({ close() { done() } }), options: { port, direction: 'recv', channelId: 'channel', senderId: 'alice' } } })
    port.onmessage?.({ data: { type: 'key', epoch: 2, key } })
    // An old epoch counts for nothing; a newer one and an opened one count, once a second.
    enqueue({ data: asBuffer(await sealedAt(1)) })
    enqueue({ data: asBuffer(await sealedAt(3)) })
    await vi.waitFor(() => expect(posted).toEqual([{ type: 'stats', senders: { alice: { opened: 0, newer: 1, epoch: 3 } } }]))
    enqueue({ data: asBuffer(await sealedAt(2)) })
    enqueue({ data: asBuffer(await sealedAt(5)) })
    await new Promise(resolve => setTimeout(resolve, 10))
    expect(posted).toHaveLength(1)
    vi.setSystemTime(Date.now() + 1500)
    enqueue({ data: asBuffer(await sealedAt(2)) })
    await vi.waitFor(() => expect(posted).toHaveLength(2))
    expect(posted[1]).toEqual({ type: 'stats', senders: { alice: { opened: 2, newer: 1, epoch: 5 } } })
    close()
    await finished
    vi.useRealTimers()
  })

  it('drops video whose codec is unknown or whose type is not a picture', async () => {
    const key = await importEpochKey(new Uint8Array(16).fill(2))
    if (!key) throw new Error('expected an epoch key')
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
    const key = await importEpochKey(new Uint8Array(16).fill(5))
    if (!key) throw new Error('expected an epoch key')
    const packet = await sealFrame({ key, epoch: 1, channelId: 'channel', senderId: 'alice', plaintext: new Uint8Array([4]) })
    if (!packet) throw new Error('expected a sealed frame')
    const keyed = { type: 'key', epoch: 1, key }
    expect(await frames('recv', [packet.buffer], [keyed], { named: false })).toEqual([])
    const named = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: 'alice' }], { named: false })
    expect(new Uint8Array((named[0] as { data: ArrayBuffer }).data)).toEqual(new Uint8Array([4]))
    const other = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: 'bob' }], { named: false })
    expect(other).toEqual([])
    const ignored = await frames('recv', [packet.buffer], [keyed, { type: 'sender', senderId: '' }], { named: false })
    expect(ignored).toEqual([])
    // A sender is always named at the start; a message cannot rename it.
    const renamed = await frames('send', [new Uint8Array([1]).buffer], [keyed, { type: 'sender', senderId: 'mallory' }])
    expect(await openFrame({
      keys: new Map([[1, key]]),
      channelId: 'channel',
      senderId: 'alice',
      packet: new Uint8Array((renamed[0] as { data: ArrayBuffer }).data),
    })).toEqual(new Uint8Array([1]))
  })

  it('drops a frame the key cannot seal and a stream that fails', async () => {
    const decryptOnly = await crypto.subtle.importKey('raw', new Uint8Array(16), 'AES-GCM', false, ['decrypt'])
    const dropped = await frames('send', [new Uint8Array([1]).buffer], [{ type: 'key', epoch: 4, key: decryptOnly }])
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
})
