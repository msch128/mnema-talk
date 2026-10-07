// AI noise suppression in the browser. DFN3 runs in a dedicated Worker with
// an AudioWorklet bridge; GTCRN runs in an AudioWorklet. WASM and weights are
// bundled and served
// by our own binary, so no audio or request ever leaves for a third party.
//
//   'dfn3'  DeepFilterNet3 (third_party/deepfilternet3): full-band 48 kHz, best
//           quality, ~10 MB compressed download once, more CPU.
//   'gtcrn' GTCRN (@sapphi-red/web-noise-suppressor): tiny and light, but
//           works on 16 kHz internally, so voices lose their top end.
import gtcrnWorkletUrl from '@sapphi-red/web-noise-suppressor/gtcrnWorklet.js?url'
import gtcrnWasmUrl from '@sapphi-red/web-noise-suppressor/gtcrn.wasm?url'
import dfnWorkletUrl from './dfnBridgeWorklet.ts?worker&url'
import dfnWasmUrl from '../third_party/deepfilternet3/df_bg.wasm?url'
import dfnModelUrl from '../third_party/deepfilternet3/DeepFilterNet3_onnx.tgz?url'
import { createDeepFilterNode } from './dfnNode'
import type { FilterNode } from './dfnNode'
import type { FilterOptions, NoiseModel } from './dfnTypes'

const assetPromises = new Map<string, Promise<ArrayBuffer>>()
let dfnModulePromise: Promise<WebAssembly.Module> | null = null
let gtcrnModulePromise: Promise<typeof import('@sapphi-red/web-noise-suppressor')> | null = null
// addModule is per AudioContext, and contexts are recreated on settings changes.
const registeredWorklets = new WeakMap<AudioContext, Map<NoiseModel, Promise<void>>>()

export function isNoiseSuppressionSupported() {
  return typeof AudioWorkletNode !== 'undefined' && typeof WebAssembly !== 'undefined'
}

function fetchOnce(url: string): Promise<ArrayBuffer> {
  if (!assetPromises.has(url)) {
    const p = fetch(url).then(res => {
      if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
      return res.arrayBuffer()
    })
    p.catch(() => assetPromises.delete(url))
    assetPromises.set(url, p)
  }
  const cached = assetPromises.get(url)
  if (!cached) throw new Error("Missing noise model asset request")
  return cached
}

function compileDfnOnce(): Promise<WebAssembly.Module> {
  if (!dfnModulePromise) {
    dfnModulePromise = fetch(dfnWasmUrl).then(res => {
      if (!res.ok) throw new Error(`${dfnWasmUrl}: HTTP ${res.status}`)
      return res.arrayBuffer().then(buf => WebAssembly.compile(buf))
    })
    dfnModulePromise.catch(() => { dfnModulePromise = null })
  }
  return dfnModulePromise
}

const MODELS = {
  dfn3: {
    sampleRates: [48000],
    workletUrl: dfnWorkletUrl,
    load: () => Promise.all([
      // Compiled once asynchronously; the Module is shared with each Worker.
      compileDfnOnce(),
      fetchOnce(dfnModelUrl)
    ]),
    create: createDeepFilterNode
  },
  gtcrn: {
    sampleRates: [16000, 48000],
    workletUrl: gtcrnWorkletUrl,
    load: () => {
      // The package's entry subclasses AudioWorkletNode at import time, so it
      // is only imported once support is confirmed.
      if (!gtcrnModulePromise) gtcrnModulePromise = import('@sapphi-red/web-noise-suppressor')
      return Promise.all([gtcrnModulePromise, fetchOnce(gtcrnWasmUrl)])
    },
    create: (ctx: AudioContext, [{ GtcrnWorkletNode }, wasmBinary]: readonly [typeof import('@sapphi-red/web-noise-suppressor'), ArrayBuffer]) => new GtcrnWorkletNode(ctx, { maxChannels: 1, wasmBinary })
  }
}

/** Starts downloading a model early (e.g. while the mic permission prompt is open). */
export function preloadNoiseSuppressor(model: NoiseModel = 'dfn3') {
  if (isNoiseSuppressionSupported() && MODELS[model]) MODELS[model].load().catch(() => {})
}

/**
 * Returns a mono AudioWorkletNode running the model, or null when the browser
 * can't run it. Callers fall back to the browser's own suppression.
 */
export async function createNoiseSuppressorNode(audioContext: AudioContext, model: NoiseModel = 'dfn3', options: FilterOptions = {}): Promise<FilterNode | null> {
  const spec = MODELS[model]
  if (!spec || !isNoiseSuppressionSupported() || !audioContext?.audioWorklet) return null
  if (!spec.sampleRates.includes(audioContext.sampleRate)) return null
  const stale = () => options.signal?.aborted || audioContext.state === 'closed'
  if (stale()) return null

  try {
    const loaded = model === 'dfn3'
      ? { model, assets: await MODELS.dfn3.load() }
      : { model, assets: await MODELS.gtcrn.load() }
    if (stale()) return null
    let registered = registeredWorklets.get(audioContext)
    if (!registered) registeredWorklets.set(audioContext, registered = new Map())
    if (!registered.has(model)) {
      const registration = audioContext.audioWorklet.addModule(spec.workletUrl)
      registered.set(model, registration)
      registration.catch(() => { if (registered.get(model) === registration) registered.delete(model) })
    }
    await registered.get(model)
    if (stale()) return null
    return loaded.model === 'dfn3'
      ? await MODELS.dfn3.create(audioContext, loaded.assets, options)
      : MODELS.gtcrn.create(audioContext, loaded.assets)
  } catch (err) {
    console.warn(`AI noise suppression (${model}) unavailable:`, err)
    return null
  }
}
