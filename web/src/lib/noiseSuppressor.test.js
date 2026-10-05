import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

vi.mock('@sapphi-red/web-noise-suppressor', () => ({
  GtcrnWorkletNode: class {
    constructor(ctx, opts) { this.ctx = ctx; this.opts = opts }
  }
}))

class FakeWorkletNode {
  constructor(ctx, name, opts) { this.ctx = ctx; this.name = name; this.opts = opts }
}

function fakeContext(sampleRate = 48000) {
  return { sampleRate, audioWorklet: { addModule: vi.fn().mockResolvedValue() } }
}

let mod
beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal('AudioWorkletNode', FakeWorkletNode)
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
    expect(node.name).toBe('deepfilter-audio-processor')
    expect(node.opts.processorOptions.wasmModule).toEqual({ compiled: true })
    expect(node.opts.processorOptions.modelBytes).toBeInstanceOf(ArrayBuffer)
    expect(node.opts.processorOptions.suppressionLevel).toBe(100)
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
})
