import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createDeepFilterNode } from './dfnNode'
import type { DfnWorkerCommand, DeepFilterAssets } from './dfnTypes'
import { present } from '../media-test.fixture'

class FakePort {
  onmessage = (_event: { data: unknown }) => {}
  postMessage = vi.fn()
  close = vi.fn()
}
class FakeChannel {
  static instances: FakeChannel[] = []
  port1 = new FakePort()
  port2 = new FakePort()
  constructor() { FakeChannel.instances.push(this) }
}
class FakeWorker {
  static instances: FakeWorker[] = []
  postMessage = vi.fn<(command: DfnWorkerCommand, transfers?: Transferable[]) => void>()
  terminate = vi.fn()
  onmessage = (_event: { data: unknown }) => {}
  onerror = () => {}
  constructor(public url: URL, public options: WorkerOptions) { FakeWorker.instances.push(this) }
}
class FakeNode {
  static throws = false
  port = new FakePort()
  onprocessorerror = () => {}
  disconnect = vi.fn()
  destroy?: () => void
  filterStats?: { processedFrames: number; droppedFrames: number; bridgeBudgetMs: number }
  constructor(public ctx: AudioContext, public name: string, public options: AudioWorkletNodeOptions) {
    if (FakeNode.throws) throw new Error('node construction failed')
  }
}
class FakeContext { state: AudioContextState = 'running' }
const assets: DeepFilterAssets = [new WebAssembly.Module(new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0])), new ArrayBuffer(8)]
function context() { return new AudioContext() }
function worker() { return present(FakeWorker.instances.at(-1)) }
function ready(frameLength = 480) { worker().onmessage({ data: { type: 'ready', frameLength } }) }
async function initialized(options = {}, ctx = context()) {
  const creating = createDeepFilterNode(ctx, assets, options)
  ready()
  const node = await creating
  if (!(node instanceof FakeNode)) throw new Error('Expected fake bridge')
  return { node, worker: worker(), channel: present(FakeChannel.instances.at(-1)), ctx }
}
beforeEach(() => {
  vi.useFakeTimers()
  FakeWorker.instances = []
  FakeChannel.instances = []
  FakeNode.throws = false
  vi.stubGlobal('AudioContext', FakeContext)
  vi.stubGlobal('AudioWorkletNode', FakeNode)
  vi.stubGlobal('Worker', FakeWorker)
  vi.stubGlobal('MessageChannel', FakeChannel)
})
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

describe('DFN node lifecycle', () => {
  it('skips closed contexts and already-aborted capture', async () => {
    const ctx = context()
    Object.defineProperty(ctx, 'state', { value: 'closed' })
    expect(await createDeepFilterNode(ctx, assets)).toBeNull()
    const controller = new AbortController()
    controller.abort()
    expect(await createDeepFilterNode(context(), assets, { signal: controller.signal })).toBeNull()
    expect(FakeWorker.instances).toHaveLength(0)
  })

  it('transfers only communication ports, accepts validated stats, and destroys once', async () => {
    const controller = new AbortController()
    const { node, worker: active, channel, ctx } = await initialized({ signal: controller.signal })
    expect(active.url.pathname).toContain('dfnWorker.ts')
    expect(active.options).toEqual({ type: 'module' })
    expect(active.postMessage).toHaveBeenNthCalledWith(1, { type: 'init', wasmModule: assets[0], modelBytes: assets[1], suppressionLevel: 100 })
    expect(active.postMessage).toHaveBeenNthCalledWith(2, { type: 'connect', port: channel.port2 }, [channel.port2])
    expect(node.port.postMessage).toHaveBeenCalledExactlyOnceWith({ type: 'connect', port: channel.port1 }, [channel.port1])
    expect(node.ctx).toBe(ctx)
    expect(node.name).toBe('deepfilter-worker-bridge')
    expect(node.options).toEqual({ numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1] })
    node.port.onmessage({ data: { type: 'stats', processedFrames: 100, droppedFrames: 2, bridgeBudgetMs: 30 } })
    expect(node.filterStats).toEqual({ processedFrames: 100, droppedFrames: 2, bridgeBudgetMs: 30 })
    node.port.onmessage({ data: { type: 'stats', processedFrames: 'bad' } })
    expect(node.filterStats?.processedFrames).toBe(100)
    present(node.destroy)()
    present(node.destroy)()
    controller.abort()
    active.onerror()
    expect(active.terminate).toHaveBeenCalledOnce()
    expect(channel.port1.close).toHaveBeenCalledOnce()
    expect(channel.port2.close).toHaveBeenCalledOnce()
    expect(node.disconnect).toHaveBeenCalledOnce()
  })

  it.each(['worker', 'bridge', 'processor'] as const)('reports %s runtime failure only once and switches bridge to bypass', async source => {
    const onFailure = vi.fn()
    const { node, worker: active } = await initialized({ onFailure })
    if (source === 'worker') active.onmessage({ data: { type: 'failed' } })
    if (source === 'bridge') node.port.onmessage({ data: { type: 'failed' } })
    if (source === 'processor') node.onprocessorerror()
    active.onerror()
    node.onprocessorerror()
    expect(onFailure).toHaveBeenCalledOnce()
    expect(node.port.postMessage).toHaveBeenLastCalledWith({ type: 'failed' })
    expect(active.terminate).toHaveBeenCalledOnce()
    present(node.destroy)()
  })

  it('handles failure without a callback and disconnect errors during cleanup', async () => {
    const { node, channel } = await initialized()
    node.onprocessorerror()
    node.disconnect.mockImplementationOnce(() => { throw new Error('already disconnected') })
    expect(() => present(node.destroy)()).not.toThrow()
    expect(channel.port1.close).toHaveBeenCalledOnce()
  })

  it.each(['worker-error', 'failed-message', 'wrong-size', 'timeout'] as const)('rejects %s initialization and terminates resources', async source => {
    const creating = createDeepFilterNode(context(), assets)
    const rejected = expect(creating).rejects.toThrow(source === 'wrong-size' ? 'Unexpected filter frame size' : source === 'timeout' ? 'timed out' : 'unavailable')
    worker().onmessage({ data: { type: 'irrelevant' } })
    if (source === 'worker-error') worker().onerror()
    if (source === 'failed-message') worker().onmessage({ data: { type: 'failed' } })
    if (source === 'wrong-size') ready(256)
    if (source === 'timeout') await vi.advanceTimersByTimeAsync(15000)
    await rejected
    expect(worker().terminate).toHaveBeenCalledOnce()
    expect(FakeChannel.instances).toHaveLength(0)
  })

  it('aborts while waiting for worker readiness', async () => {
    const controller = new AbortController()
    const creating = createDeepFilterNode(context(), assets, { signal: controller.signal })
    controller.abort()
    expect(await creating).toBeNull()
    expect(worker().terminate).toHaveBeenCalledOnce()
  })

  it.each(['abort', 'closed'] as const)('cancels %s capture between ready notification and node construction', async reason => {
    const controller = new AbortController()
    const ctx = context()
    const creating = createDeepFilterNode(ctx, assets, { signal: controller.signal })
    ready()
    if (reason === 'abort') controller.abort()
    else Object.defineProperty(ctx, 'state', { value: 'closed' })
    expect(await creating).toBeNull()
    expect(worker().terminate).toHaveBeenCalledOnce()
    expect(FakeChannel.instances).toHaveLength(0)
  })

  it('aborts an active node and releases its channels', async () => {
    const controller = new AbortController()
    const { node, channel } = await initialized({ signal: controller.signal })
    controller.abort()
    expect(node.disconnect).toHaveBeenCalledOnce()
    expect(channel.port2.close).toHaveBeenCalledOnce()
  })

  it('propagates worklet construction failure after worker readiness and terminates the worker', async () => {
    FakeNode.throws = true
    const creating = createDeepFilterNode(context(), assets)
    ready()
    await expect(creating).rejects.toThrow('node construction failed')
    expect(worker().terminate).toHaveBeenCalledOnce()
  })
})
