import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMediaGuard } from './guard'
import type { EpochKeys } from './frame'

interface Port {
  postMessage: ReturnType<typeof vi.fn>
  close: ReturnType<typeof vi.fn>
}
interface CreatedWorker {
  url: URL
  options: WorkerOptions | undefined
  terminate: ReturnType<typeof vi.fn>
}
interface CreatedTransform {
  worker: CreatedWorker
  options: unknown
  transfer: unknown[]
}

const created = {
  workers: [] as CreatedWorker[],
  channels: [] as { port1: Port, port2: Port }[],
  transforms: [] as CreatedTransform[],
}

function installBrowser() {
  created.workers = []
  created.channels = []
  created.transforms = []
  vi.stubGlobal('Worker', function Worker(this: CreatedWorker, url: URL, options?: WorkerOptions) {
    this.url = url
    this.options = options
    this.terminate = vi.fn()
    created.workers.push(this)
  })
  vi.stubGlobal('MessageChannel', function MessageChannel(this: { port1: Port, port2: Port }) {
    this.port1 = { postMessage: vi.fn(), close: vi.fn() }
    this.port2 = { postMessage: vi.fn(), close: vi.fn() }
    created.channels.push(this)
  })
  vi.stubGlobal('RTCRtpScriptTransform', function RTCRtpScriptTransform(this: CreatedTransform, worker: CreatedWorker, options: unknown, transfer: unknown[]) {
    this.worker = worker
    this.options = options
    this.transfer = transfer
    created.transforms.push(this)
  })
}

afterEach(() => { vi.unstubAllGlobals() })

describe('media transform guard', () => {
  it('does not arm a sender when the browser has no encoded transform', () => {
    const guard = createMediaGuard()
    const sender = { transform: null } as RTCRtpSender
    expect(guard.armSender(sender, 'channel', 'alice', 0)).toBe(false)
    expect(sender.transform).toBeNull()
    guard.setKey(1, {} as EpochKeys)
    guard.reset()
  })

  it('arms both directions, repeats the current key and drops it on reset', () => {
    installBrowser()
    const guard = createMediaGuard()
    const sender = { transform: null } as RTCRtpSender
    const receiver = { transform: null } as RTCRtpReceiver
    const key = {} as EpochKeys
    expect(guard.armSender(sender, '', 'alice', 0)).toBe(false)
    expect(guard.armReceiver(receiver, 'channel', '')).toBe(false)
    expect(created.workers).toHaveLength(0)
    guard.setKey(4, key)
    expect(guard.armSender(sender, 'channel', 'alice', 0)).toBe(true)
    expect(guard.armReceiver(receiver, 'channel', 'bob')).toBe(true)
    expect(created.workers).toHaveLength(1)
    expect(created.workers[0]?.options).toEqual({ type: 'module' })
    expect(String(created.workers[0]?.url)).toContain('mediaFrameWorker.ts')
    expect(sender.transform).toBe(created.transforms[0])
    expect(created.transforms[0]?.transfer).toEqual([created.channels[0]?.port2])
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 4, key, seq: 1 })
    expect(created.channels[1]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 4, key, seq: 1 })
    const next = {} as EpochKeys
    guard.setKey(5, next)
    expect(created.channels[1]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 5, key: next, seq: 2 })
    guard.setKey(6, next, false, true)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 6, key: next, soft: true, seq: 3 })
    guard.setKey(0, key, true, true)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 0, key, replace: true, seq: 4 })
    guard.staleKeys()
    expect(created.channels[1]?.port1.postMessage).toHaveBeenCalledWith({ type: 'stale' })
    created.channels[0]?.port1.postMessage.mockImplementationOnce(() => { throw new Error('closed') })
    created.channels[0]?.port1.close.mockImplementationOnce(() => { throw new Error('closed') })
    guard.reset()
    expect(created.workers[0]?.terminate).toHaveBeenCalledOnce()
    expect(created.channels[1]?.port1.postMessage).toHaveBeenCalledWith({ type: 'drop' })
    expect(guard.armSender(sender, 'channel', 'alice', 0)).toBe(true)
    expect(created.workers).toHaveLength(2)
  })

  it('releases the port of a line when the same sender or receiver is armed again', () => {
    installBrowser()
    const guard = createMediaGuard()
    const sender = { transform: null } as RTCRtpSender
    expect(guard.armSender(sender, 'channel', 'alice', 0)).toBe(true)
    expect(guard.armSender(sender, 'channel', 'alice', 0)).toBe(true)
    const [first, second] = created.channels
    // 'drop' wipes the worker's keys for every line, so a replaced line must not send it.
    expect(first?.port1.postMessage).not.toHaveBeenCalledWith({ type: 'drop' })
    expect(first?.port1.close).toHaveBeenCalledOnce()
    expect(second?.port1.close).not.toHaveBeenCalled()
    // Only the live line hears the next key.
    guard.setKey(1, {} as EpochKeys)
    expect(first?.port1.postMessage).not.toHaveBeenCalledWith(expect.objectContaining({ type: 'key' }))
    expect(second?.port1.postMessage).toHaveBeenCalledWith(expect.objectContaining({ type: 'key', epoch: 1 }))
  })

  it('does not lose the negotiation when a browser refuses to clone what a line needs', () => {
    installBrowser()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const refused = () => { throw new DOMException('The object can not be cloned.', 'DataCloneError') }
    const guard = createMediaGuard()
    const key = {} as EpochKeys
    guard.setKey(2, key)

    // The key message cannot be cloned for a new line: the line is armed and keyless.
    const first = { transform: null } as RTCRtpSender
    vi.stubGlobal('MessageChannel', function MessageChannel(this: { port1: Port, port2: Port }) {
      this.port1 = { postMessage: vi.fn(refused), close: vi.fn() }
      this.port2 = { postMessage: vi.fn(), close: vi.fn() }
      created.channels.push(this)
    })
    expect(guard.armSender(first, 'channel', 'alice', 0)).toBe(true)
    expect(first.transform).not.toBeNull()
    expect(warn).toHaveBeenCalledWith('[media] Could not hand the key to a line:', expect.any(DOMException))

    // A later key still reaches every other line, and the refusing one does not stop it.
    const second = { transform: null } as RTCRtpReceiver
    installBrowser()
    expect(guard.armReceiver(second, 'channel', 'bob', 0)).toBe(true)
    expect(() => guard.setKey(3, key)).not.toThrow()
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 3, key, seq: 2 })

    // The transform itself cannot be built: the line stays unprotected, the caller is told, nothing leaks.
    vi.stubGlobal('RTCRtpScriptTransform', function RTCRtpScriptTransform() { refused() })
    const third = { transform: null } as RTCRtpSender
    expect(guard.armSender(third, 'channel', 'alice', 0)).toBe(false)
    expect(third.transform).toBeNull()
    expect(created.channels.at(-1)?.port1.close).toHaveBeenCalled()
    expect(warn).toHaveBeenCalledWith('[media] Could not arm a line:', expect.any(DOMException))
    guard.reset()
    warn.mockRestore()
  })

  it('tries a refused key message once more before giving up on a line', () => {
    installBrowser()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const guard = createMediaGuard()
    const key = {} as EpochKeys
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', 0)).toBe(true)
    const port = created.channels[0]?.port1
    port?.postMessage.mockImplementationOnce(() => { throw new DOMException('The object can not be cloned.', 'DataCloneError') })
    guard.setKey(7, key)
    expect(port?.postMessage).toHaveBeenCalledTimes(2)
    expect(warn).not.toHaveBeenCalled()
    port?.postMessage.mockImplementation(() => { throw new DOMException('The object can not be cloned.', 'DataCloneError') })
    guard.setKey(8, key)
    expect(port?.postMessage).toHaveBeenCalledTimes(4)
    expect(warn).toHaveBeenCalledTimes(1)
    guard.reset()
    warn.mockRestore()
  })

  it('names the layer of a screen share line in the transform, and only then', () => {
    installBrowser()
    const guard = createMediaGuard()
    const plain = { transform: null } as RTCRtpSender
    const layered = { transform: null } as RTCRtpSender
    expect(guard.armSender(plain, 'channel', 'alice', 0)).toBe(true)
    expect(guard.armSender(layered, 'channel', 'alice', 2, 2)).toBe(true)
    expect(created.transforms[0]?.options).toEqual({ port: created.channels[0]?.port2, direction: 'send', channelId: 'channel', senderId: 'alice', source: 0 })
    expect(created.transforms[1]?.options).toEqual({ port: created.channels[1]?.port2, direction: 'send', channelId: 'channel', senderId: 'alice', source: 2, layer: 2 })
    // Layer 0 is a layer too.
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', 2, 0)).toBe(true)
    expect(created.transforms[2]?.options).toMatchObject({ layer: 0 })
  })

  it('tells the transform of a microphone line that uses DTX, and only then', () => {
    installBrowser()
    const guard = createMediaGuard()
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', 0, undefined, true)).toBe(true)
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', 0, undefined, false)).toBe(true)
    expect(created.transforms[0]?.options).toEqual({ port: created.channels[0]?.port2, direction: 'send', channelId: 'channel', senderId: 'alice', source: 0, dtx: true })
    expect(created.transforms[1]?.options).not.toHaveProperty('dtx')
  })

  it('tells every line the key, and a line armed later too', () => {
    installBrowser()
    const guard = createMediaGuard()
    const key = {} as EpochKeys
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', 1)).toBe(true)
    guard.setKey(3, key)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 3, key, seq: 1 })
    expect(guard.armReceiver({ transform: null } as RTCRtpReceiver, 'channel', 'bob', 0)).toBe(true)
    expect(created.channels[1]?.port1.postMessage.mock.calls.map(call => (call[0] as { type: string }).type)).toEqual(['key'])
    // A new key replaces the one a line armed later hears; so does a reset.
    guard.setKey(4, key)
    expect(guard.armReceiver({ transform: null } as RTCRtpReceiver, 'channel', 'bob', 0)).toBe(true)
    expect(created.channels[2]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 4, key, seq: 2 })
    guard.reset()
  })

  it('says what a line carries and tells a reused receiver its new line', () => {
    installBrowser()
    const guard = createMediaGuard()
    const receiver = { transform: null } as RTCRtpReceiver
    expect(guard.armReceiver(receiver, 'channel', null)).toBe(true)
    expect(created.transforms[0]?.options).not.toHaveProperty('source')
    expect(guard.armReceiver(receiver, 'channel', 'bob', 3)).toBe(true)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenLastCalledWith({ type: 'sender', senderId: 'bob', source: 3 })
    expect(guard.armReceiver({ transform: null } as RTCRtpReceiver, 'channel', 'bob', 1)).toBe(true)
    expect(created.transforms[1]?.options).toMatchObject({ direction: 'recv', senderId: 'bob', source: 1 })
    // A sending line must say what it carries.
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', 'alice', undefined as unknown as 0)).toBe(false)
    guard.reset()
  })

  it('finds an armed line again when the browser hands out a new object for the same receiver', () => {
    // WebKit collects an unreferenced receiver object and makes another when it is asked again. Arming that one
    // anew put a second transform on a line already receiving, which then stopped delivering frames.
    installBrowser()
    const guard = createMediaGuard()
    const receiver = { transform: null } as RTCRtpReceiver
    expect(guard.armReceiver(receiver, 'channel', null)).toBe(true)
    const sameReceiverAnewObject = { transform: receiver.transform } as RTCRtpReceiver
    expect(guard.armReceiver(sameReceiverAnewObject, 'channel', null)).toBe(true)
    expect(guard.armReceiver(sameReceiverAnewObject, 'channel', 'bob', 0)).toBe(true)
    expect(created.transforms).toHaveLength(1)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenLastCalledWith({ type: 'sender', senderId: 'bob', source: 0 })
    guard.reset()
  })

  it('arms a receiver before its publisher is known and names the publisher later', () => {
    installBrowser()
    const guard = createMediaGuard()
    const receiver = { transform: null } as RTCRtpReceiver
    const key = {} as EpochKeys
    guard.setKey(2, key)
    expect(guard.armReceiver(receiver, 'channel', null)).toBe(true)
    expect(receiver.transform).toBe(created.transforms[0])
    expect(created.transforms[0]?.options).toEqual({ port: created.channels[0]?.port2, direction: 'recv', channelId: 'channel' })
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 2, key, seq: 1 })
    const first = receiver.transform
    expect(guard.armReceiver(receiver, 'channel', null)).toBe(true)
    expect(guard.armReceiver(receiver, 'channel', 'bob')).toBe(true)
    // A browser ignores a transform set after the line started, so the one
    // that was set first stays and is told the publisher.
    expect(receiver.transform).toBe(first)
    expect(created.transforms).toHaveLength(1)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenLastCalledWith({ type: 'sender', senderId: 'bob' })
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', null as unknown as string, 0)).toBe(false)
    guard.reset()
    expect(guard.armReceiver(receiver, 'channel', 'bob')).toBe(true)
    expect(created.transforms).toHaveLength(2)
  })

  it('passes on what the worker counted, ignoring anything malformed', () => {
    installBrowser()
    const guard = createMediaGuard()
    const seen: unknown[] = []
    guard.onStats(senders => seen.push(senders))
    expect(guard.armReceiver({ transform: null } as RTCRtpReceiver, 'channel', 'bob')).toBe(true)
    const worker = created.workers[0] as unknown as { onmessage: (event: { data: unknown }) => void }
    worker.onmessage({ data: { type: 'stats', senders: { bob: { opened: 3, newer: 1, epoch: 6, legacy: 2 }, eve: { opened: -1, newer: 0, epoch: 1, legacy: 0 }, old: { opened: 1, newer: 0, epoch: 1 }, mallory: { opened: 1 }, odd: 7 } } })
    for (const data of [null, 'stats', { type: 'other' }, { type: 'stats' }, { type: 'stats', senders: 3 }]) worker.onmessage({ data })
    expect(seen).toEqual([{ bob: { opened: 3, newer: 1, epoch: 6, legacy: 2 } }])
    guard.reset()
  })
})
