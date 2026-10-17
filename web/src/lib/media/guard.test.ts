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
    expect(guard.armSender(sender, 'channel', 'alice')).toBe(false)
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
    expect(guard.armSender(sender, '', 'alice')).toBe(false)
    expect(guard.armReceiver(receiver, 'channel', '')).toBe(false)
    expect(created.workers).toHaveLength(0)
    guard.setKey(4, key)
    expect(guard.armSender(sender, 'channel', 'alice')).toBe(true)
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
    guard.setKey(0, key, true)
    expect(created.channels[0]?.port1.postMessage).toHaveBeenCalledWith({ type: 'key', epoch: 0, key, replace: true, seq: 3 })
    created.channels[0]?.port1.postMessage.mockImplementationOnce(() => { throw new Error('closed') })
    created.channels[0]?.port1.close.mockImplementationOnce(() => { throw new Error('closed') })
    guard.reset()
    expect(created.workers[0]?.terminate).toHaveBeenCalledOnce()
    expect(created.channels[1]?.port1.postMessage).toHaveBeenCalledWith({ type: 'drop' })
    expect(guard.armSender(sender, 'channel', 'alice')).toBe(true)
    expect(created.workers).toHaveLength(2)
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
    expect(guard.armSender({ transform: null } as RTCRtpSender, 'channel', null as unknown as string)).toBe(false)
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
    worker.onmessage({ data: { type: 'stats', senders: { bob: { opened: 3, newer: 1, epoch: 6 }, eve: { opened: -1, newer: 0, epoch: 1 }, mallory: { opened: 1 }, odd: 7 } } })
    for (const data of [null, 'stats', { type: 'other' }, { type: 'stats' }, { type: 'stats', senders: 3 }]) worker.onmessage({ data })
    expect(seen).toEqual([{ bob: { opened: 3, newer: 1, epoch: 6 } }])
    guard.reset()
  })
})
