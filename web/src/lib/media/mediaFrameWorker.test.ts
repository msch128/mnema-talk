import { webcrypto } from 'node:crypto'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { importEpochKey, openFrame, sealFrame } from './frame'

const previous = globalThis.crypto

interface WorkerScope {
  onrtctransform: ((event: { transformer?: unknown }) => void) | null
}

function asBuffer(data: ArrayBufferLike): ArrayBuffer {
  const view = new Uint8Array(data)
  const copy = new Uint8Array(view.byteLength)
  copy.set(view)
  return copy.buffer
}

async function frames(direction: 'send' | 'recv', packets: ArrayBufferLike[], commands: unknown[]) {
  vi.resetModules()
  const scope: WorkerScope = { onrtctransform: null }
  vi.stubGlobal('self', scope)
  vi.stubGlobal('crypto', webcrypto)
  await import('./mediaFrameWorker')
  const received: unknown[] = []
  const port = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    postMessage(data: unknown) { port.onmessage?.({ data }) },
  }
  let enqueueFrame: (frame: { data: ArrayBuffer }) => void = () => {}
  let closeStream: () => void = () => {}
  const readable = new ReadableStream<{ data: ArrayBuffer }>({
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
  scope.onrtctransform?.({ transformer: { readable, writable, options: { port, direction, channelId: 'channel', senderId: 'alice' } } })
  for (const command of commands) port.postMessage(command)
  for (const data of packets) enqueueFrame({ data: asBuffer(data) })
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
