import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

import type { DfnWorkerCommand, DfnWorkerReply } from './dfnTypes'
import { present } from '../media-test.fixture'

interface NodeOptions extends AudioWorkletNodeOptions { maxChannels?: number; wasmBinary?: ArrayBuffer }
class FakeWorkletNode {
  port = { postMessage: vi.fn(), onmessage: (_event: { data: unknown }) => {} }
  disconnect = vi.fn()
  destroy = () => {}
  constructor(public ctx: AudioContext, public name: string, public opts: NodeOptions) {}
}
vi.mock('@sapphi-red/web-noise-suppressor', () => ({
  GtcrnWorkletNode: class extends FakeWorkletNode {
    constructor(ctx: AudioContext, opts: NodeOptions) { super(ctx, 'gtcrn', opts) }
  }
}))

class FakeWorker {
  static instances: FakeWorker[] = []
  messages: DfnWorkerCommand[] = []
  terminate = vi.fn()
  onmessage = (_event: { data: DfnWorkerReply }) => {}
  onerror = () => {}
  constructor() { FakeWorker.instances.push(this) }
  postMessage(message: DfnWorkerCommand) {
    this.messages.push(message)
    if (message.type === 'init') queueMicrotask(() => this.onmessage({ data: { type: 'ready', frameLength: 480 } }))
  }
}
class FakeContext {
  audioWorklet = { addModule: vi.fn().mockResolvedValue(undefined) }
  constructor(options: AudioContextOptions = {}) { this.sampleRate = options.sampleRate ?? 48000 }
  sampleRate: number
}
function fakeContext(sampleRate = 48000): AudioContext { return new AudioContext({ sampleRate }) }
let mod: typeof import('./noiseSuppressor')
const compiledModule = new WebAssembly.Module(new Uint8Array([0,97,115,109,1,0,0,0]))
async function createNode(ctx: AudioContext, model: 'dfn3' | 'gtcrn' = 'dfn3', options = {}) {
  const node = await mod.createNoiseSuppressorNode(ctx, model, options)
  if (!(node instanceof FakeWorkletNode)) throw new Error('Expected mocked filter node')
  return node
}
beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('AudioWorkletNode', FakeWorkletNode)
  vi.stubGlobal('AudioContext', FakeContext)
  FakeWorker.instances = []
  vi.stubGlobal('Worker', FakeWorker)
  vi.stubGlobal('MessageChannel', class {
    port1 = { close: vi.fn() }; port2 = { close: vi.fn() }
  })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)) }))
  vi.spyOn(WebAssembly, 'compile').mockResolvedValue(compiledModule)
  mod = await import('./noiseSuppressor')
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('createNoiseSuppressorNode', () => {
  it('returns null without AudioWorklet support', async () => {
    vi.stubGlobal('AudioWorkletNode', undefined)
    expect(mod.isNoiseSuppressionSupported()).toBe(false)
    expect(await mod.createNoiseSuppressorNode(fakeContext())).toBeNull()
  })

  it('returns null for sample rates the model cannot run at', async () => {
    expect(await mod.createNoiseSuppressorNode(fakeContext(44100), 'gtcrn')).toBeNull()
    expect(await mod.createNoiseSuppressorNode(fakeContext(16000), 'dfn3')).toBeNull()
  })

  it('creates DeepFilterNet3 by default with the compiled wasm and the model', async () => {
    const node = await createNode(fakeContext())
    expect(node.name).toBe('deepfilter-worker-bridge')
    const initialization = present(present(FakeWorker.instances[0]).messages[0])
    if (initialization.type !== 'init') throw new Error('Expected model initialization')
    expect(initialization.wasmModule).toBe(compiledModule)
    expect(initialization.modelBytes).toBeInstanceOf(ArrayBuffer)
    expect(initialization.suppressionLevel).toBe(100)
    expect(node.opts.processorOptions).toBeUndefined()
    node.destroy()
  })

  it('creates GTCRN on request', async () => {
    const node = await createNode(fakeContext(), 'gtcrn')
    expect(node.opts).toMatchObject({ maxChannels: 1 })
    expect(node.opts.wasmBinary).toBeInstanceOf(ArrayBuffer)
  })

  it('registers each worklet once per context and downloads assets once', async () => {
    const a = fakeContext()
    const b = fakeContext()
    await mod.createNoiseSuppressorNode(a)
    await mod.createNoiseSuppressorNode(a)
    await mod.createNoiseSuppressorNode(a, 'gtcrn')
    await mod.createNoiseSuppressorNode(b)

    expect(a.audioWorklet.addModule).toHaveBeenCalledTimes(2) // dfn3 + gtcrn
    expect(b.audioWorklet.addModule).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledTimes(3) // dfn3 wasm + model, gtcrn wasm
  })

  it('returns null and retries the download after a failure', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 404 }))
    expect(await mod.createNoiseSuppressorNode(fakeContext())).toBeNull()
    expect(await mod.createNoiseSuppressorNode(fakeContext())).not.toBeNull()
  })

  it('deduplicates concurrent worklet registration in one context', async () => {
    const ctx = fakeContext()
    const nodes = await Promise.all([mod.createNoiseSuppressorNode(ctx), mod.createNoiseSuppressorNode(ctx)])
    expect(ctx.audioWorklet.addModule).toHaveBeenCalledTimes(1)
    nodes.forEach(node => present(node).destroy?.())
  })

  it('does not initialize a model after capture was canceled during asset loading', async () => {
    let release: ((value: Response) => void) | undefined
    vi.mocked(fetch).mockImplementationOnce(() => new Promise<Response>(resolve => { release = resolve }))
    const controller = new AbortController()
    const ctx = fakeContext()
    const creating = mod.createNoiseSuppressorNode(ctx, 'dfn3', { signal: controller.signal })
    controller.abort()
    present(release)(new Response(new ArrayBuffer(8)))
    expect(await creating).toBeNull()
    expect(FakeWorker.instances).toHaveLength(0)
    expect(ctx.audioWorklet.addModule).not.toHaveBeenCalled()
  })

  it('terminates initialization if capture is canceled before the worker is ready', async () => {
    vi.spyOn(FakeWorker.prototype, 'postMessage').mockImplementation(function (this: FakeWorker, message: DfnWorkerCommand) { this.messages.push(message) })
    const controller = new AbortController()
    const creating = mod.createNoiseSuppressorNode(fakeContext(), 'dfn3', { signal: controller.signal })
    await vi.waitFor(() => expect(FakeWorker.instances).toHaveLength(1))
    controller.abort()
    expect(await creating).toBeNull()
    expect(present(FakeWorker.instances[0]).terminate).toHaveBeenCalledOnce()
  })

  it('reports a runtime worker failure once and leaves bounded bridge bypass until replacement', async () => {
    const onFailure = vi.fn()
    const node = await createNode(fakeContext(), 'dfn3', { onFailure })
    const worker = present(FakeWorker.instances[0])
    worker.onerror()
    worker.onerror()
    expect(onFailure).toHaveBeenCalledOnce()
    expect(node.port.postMessage).toHaveBeenCalledWith({ type: 'failed' })
    node.destroy()
  })
})

describe('noise model loading lifecycle', () => {
  it('preloads the default model once and shares downloads with capture', async () => {
    mod.preloadNoiseSuppressor()
    mod.preloadNoiseSuppressor()
    const node = await createNode(fakeContext())
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(WebAssembly.compile).toHaveBeenCalledOnce()
    node.destroy()
  })
  it('does not preload unsupported models and returns without a context worklet', async () => {
    vi.stubGlobal('AudioWorkletNode', undefined)
    mod.preloadNoiseSuppressor('gtcrn')
    expect(fetch).not.toHaveBeenCalled()
    vi.stubGlobal('AudioWorkletNode', FakeWorkletNode)
    const context = fakeContext()
    Reflect.deleteProperty(context, 'audioWorklet')
    expect(await mod.createNoiseSuppressorNode(context)).toBeNull()
  })
  it('does not load an already canceled or closed capture', async () => {
    const controller = new AbortController()
    controller.abort()
    expect(await mod.createNoiseSuppressorNode(fakeContext(), 'dfn3', { signal: controller.signal })).toBeNull()
    const closed = fakeContext()
    Object.defineProperty(closed, 'state', { value: 'closed' })
    expect(await mod.createNoiseSuppressorNode(closed)).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })
  it('retries model assets and worklet registration after transient errors', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(fetch).mockResolvedValueOnce(new Response(new ArrayBuffer(8)))
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    const ctx = fakeContext()
    expect(await mod.createNoiseSuppressorNode(ctx)).toBeNull()
    expect(warning).toHaveBeenCalledWith('AI noise suppression (dfn3) unavailable:', expect.any(Error))
    vi.mocked(ctx.audioWorklet.addModule).mockRejectedValueOnce(new Error('registration failed'))
    expect(await mod.createNoiseSuppressorNode(ctx)).toBeNull()
    const node = await createNode(ctx)
    expect(ctx.audioWorklet.addModule).toHaveBeenCalledTimes(2)
    expect(WebAssembly.compile).toHaveBeenCalledOnce()
    node.destroy()
  })
  it('drops canceled captures after worklet registration completes', async () => {
    let finish: (() => void) | undefined
    const ctx = fakeContext()
    vi.mocked(ctx.audioWorklet.addModule).mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const controller = new AbortController()
    const creating = mod.createNoiseSuppressorNode(ctx, 'gtcrn', { signal: controller.signal })
    await vi.waitFor(() => expect(ctx.audioWorklet.addModule).toHaveBeenCalledOnce())
    controller.abort()
    present(finish)()
    expect(await creating).toBeNull()
  })
  it('ignores preload failures and permits a later successful download', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new Error('offline'))
    mod.preloadNoiseSuppressor('gtcrn')
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce())
    await Promise.resolve()
    const node = await createNode(fakeContext(16000), 'gtcrn')
    expect(node.name).toBe('gtcrn')
    expect(fetch).toHaveBeenCalledTimes(2)
  })
})
