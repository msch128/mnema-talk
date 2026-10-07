import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

vi.mock('@sapphi-red/web-noise-suppressor', () => ({
  GtcrnWorkletNode: class {
    constructor(ctx, opts) { this.ctx = ctx; this.opts = opts }
  }
}))

class FakeWorkletNode {
  constructor(ctx, name, opts) {
    this.ctx = ctx; this.name = name; this.opts = opts
    this.port = { postMessage: vi.fn() }
    this.disconnect = vi.fn()
  }
}

class FakeWorker {
  static instances = []
  constructor() { this.messages = []; this.terminate = vi.fn(); FakeWorker.instances.push(this) }
  postMessage(message) {
    this.messages.push(message)
    if (message.type === 'init') queueMicrotask(() => this.onmessage({ data: { type: 'ready', frameLength: 480 } }))
  }
}

function fakeContext(sampleRate = 48000) {
  return { sampleRate, audioWorklet: { addModule: vi.fn().mockResolvedValue() } }
}

let mod
beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('AudioWorkletNode', FakeWorkletNode)
  FakeWorker.instances = []
  vi.stubGlobal('Worker', FakeWorker)
  vi.stubGlobal('MessageChannel', class {
    constructor() { this.port1 = { close: vi.fn() }; this.port2 = { close: vi.fn() } }
  })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)) }))
  vi.spyOn(WebAssembly, 'compile').mockResolvedValue({ compiled: true })
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
    const node = await mod.createNoiseSuppressorNode(fakeContext())
    expect(node.name).toBe('deepfilter-worker-bridge')
    const initialization = FakeWorker.instances[0].messages[0]
    expect(initialization.wasmModule).toEqual({ compiled: true })
    expect(initialization.modelBytes).toBeInstanceOf(ArrayBuffer)
    expect(initialization.suppressionLevel).toBe(100)
    expect(node.opts.processorOptions).toBeUndefined()
    node.destroy()
  })

  it('creates GTCRN on request', async () => {
    const node = await mod.createNoiseSuppressorNode(fakeContext(), 'gtcrn')
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
    fetch.mockResolvedValueOnce({ ok: false, status: 404 })
    expect(await mod.createNoiseSuppressorNode(fakeContext())).toBeNull()
    expect(await mod.createNoiseSuppressorNode(fakeContext())).not.toBeNull()
  })

  it('deduplicates concurrent worklet registration in one context', async () => {
    const ctx = fakeContext()
    const nodes = await Promise.all([mod.createNoiseSuppressorNode(ctx), mod.createNoiseSuppressorNode(ctx)])
    expect(ctx.audioWorklet.addModule).toHaveBeenCalledTimes(1)
    nodes.forEach(node => node.destroy())
  })

  it('does not initialize a model after capture was canceled during asset loading', async () => {
    let release
    fetch.mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
    const controller = new AbortController()
    const ctx = fakeContext()
    const creating = mod.createNoiseSuppressorNode(ctx, 'dfn3', { signal: controller.signal })
    controller.abort()
    release({ ok: true, arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)) })
    expect(await creating).toBeNull()
    expect(FakeWorker.instances).toHaveLength(0)
    expect(ctx.audioWorklet.addModule).not.toHaveBeenCalled()
  })

  it('terminates initialization if capture is canceled before the worker is ready', async () => {
    vi.spyOn(FakeWorker.prototype, 'postMessage').mockImplementation(function (message) { this.messages.push(message) })
    const controller = new AbortController()
    const creating = mod.createNoiseSuppressorNode(fakeContext(), 'dfn3', { signal: controller.signal })
    await vi.waitFor(() => expect(FakeWorker.instances).toHaveLength(1))
    controller.abort()
    expect(await creating).toBeNull()
    expect(FakeWorker.instances[0].terminate).toHaveBeenCalledOnce()
  })

  it('reports a runtime worker failure once and leaves bounded bridge bypass until replacement', async () => {
    const onFailure = vi.fn()
    const node = await mod.createNoiseSuppressorNode(fakeContext(), 'dfn3', { onFailure })
    const worker = FakeWorker.instances[0]
    worker.onerror()
    worker.onerror()
    expect(onFailure).toHaveBeenCalledOnce()
    expect(node.port.postMessage).toHaveBeenCalledWith({ type: 'failed' })
    node.destroy()
  })
})
