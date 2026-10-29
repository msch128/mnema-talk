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
import dfnWorkletUrl from './dfn/dfnBridgeWorklet.ts?worker&url'
import dfnWasmUrl from '../third_party/deepfilternet3/df_bg.wasm?url'
import dfnModelUrl from '../third_party/deepfilternet3/DeepFilterNet3_onnx.tgz?url'
import { isPageUnloading, watchFilterFrames } from './audioHealth'
import { createDeepFilterNode } from './dfn/dfnNode'
import type { FilterNode } from './dfn/dfnNode'
import type { FilterOptions, NoiseModel } from './dfn/dfnTypes'

const assetPromises = new Map<string, Promise<ArrayBuffer>>()
let dfnModulePromise: Promise<WebAssembly.Module> | null = null
let gtcrnModulePromise: Promise<typeof import('@sapphi-red/web-noise-suppressor')> | null = null
// addModule is per AudioContext, and contexts are recreated on settings changes.
const registeredWorklets = new WeakMap<AudioContext, Map<NoiseModel, Promise<void>>>()

export function isNoiseSuppressionSupported() {
  return typeof AudioWorkletNode !== 'undefined' && typeof WebAssembly !== 'undefined'
    && typeof Worker !== 'undefined' && typeof MessageChannel !== 'undefined'
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
      if (!gtcrnModulePromise) {
        const loading = import('@sapphi-red/web-noise-suppressor')
        gtcrnModulePromise = loading
        // A failed chunk fetch must not be remembered: the next attempt retries.
        loading.catch(() => { if (gtcrnModulePromise === loading) gtcrnModulePromise = null })
      }
      return Promise.all([gtcrnModulePromise, fetchOnce(gtcrnWasmUrl)])
    },
    create: (ctx: AudioContext, [{ GtcrnWorkletNode }, wasmBinary]: readonly [typeof import('@sapphi-red/web-noise-suppressor'), ArrayBuffer]) => new GtcrnWorkletNode(ctx, { maxChannels: 1, wasmBinary })
  }
}

/** Starts downloading a model early (e.g. while the mic permission prompt is open). Resolves once it is loaded or has failed. */
export function preloadNoiseSuppressor(model: NoiseModel = 'dfn3'): Promise<void> {
  if (!isNoiseSuppressionSupported() || !MODELS[model]) return Promise.resolve()
  return MODELS[model].load().then(() => {}, () => {})
}

function isContextCompatible(audioContext: AudioContext, model: NoiseModel): boolean {
  const spec = MODELS[model]
  if (!spec || !isNoiseSuppressionSupported() || !audioContext?.audioWorklet) return false
  return spec.sampleRates.includes(audioContext.sampleRate)
}

function getWorkletRegistry(audioContext: AudioContext): Map<NoiseModel, Promise<void>> {
  let registered = registeredWorklets.get(audioContext)
  if (!registered) {
    registered = new Map()
    registeredWorklets.set(audioContext, registered)
  }
  return registered
}

async function registerWorkletModule(audioContext: AudioContext, model: NoiseModel, workletUrl: string): Promise<void> {
  const registry = getWorkletRegistry(audioContext)
  if (!registry.has(model)) {
    const registration = audioContext.audioWorklet.addModule(workletUrl)
    registry.set(model, registration)
    registration.catch(() => {
      if (registry.get(model) === registration) registry.delete(model)
    })
  }
  await registry.get(model)
}

type Dfn3Assets = Awaited<ReturnType<typeof MODELS.dfn3.load>>
type GtcrnAssets = Awaited<ReturnType<typeof MODELS.gtcrn.load>>
type LoadedModel =
  | { model: 'dfn3'; assets: Dfn3Assets }
  | { model: 'gtcrn'; assets: GtcrnAssets }

async function loadAssets(model: NoiseModel): Promise<LoadedModel> {
  if (model === 'dfn3') {
    return { model: 'dfn3', assets: await MODELS.dfn3.load() }
  }
  return { model: 'gtcrn', assets: await MODELS.gtcrn.load() }
}

async function buildNode(
  audioContext: AudioContext,
  loaded: LoadedModel,
  options: FilterOptions
): Promise<FilterNode | null> {
  if (loaded.model === 'dfn3') {
    return MODELS.dfn3.create(audioContext, loaded.assets, options)
  }
  return MODELS.gtcrn.create(audioContext, loaded.assets)
}

/**
 * Returns a mono AudioWorkletNode running the model, or null when the browser
 * can't run it. Callers fall back to the browser's own suppression.
 */
export async function createNoiseSuppressorNode(audioContext: AudioContext, model: NoiseModel = 'dfn3', options: FilterOptions = {}): Promise<FilterNode | null> {
  if (!isContextCompatible(audioContext, model)) return null
  const stale = () => Boolean(options.signal?.aborted || audioContext.state === 'closed' || isPageUnloading())
  if (stale()) return null

  try {
    const loaded = await loadAssets(model)
    if (stale()) return null
    await registerWorkletModule(audioContext, model, MODELS[model].workletUrl)
    if (stale()) return null
    const node = await buildNode(audioContext, loaded, options)
    if (node && model === 'dfn3') guardFrames(audioContext, node, model, options)
    return node
  } catch (err) {
    // A reload or a closed context cancels the worker and the module import; that is no failure.
    if (stale()) return null
    console.warn(`AI noise suppression (${model}) unavailable:`, err)
    return null
  }
}

// Only DFN3 reports its processed frames. A filter whose context renders but which delivers no frame is given up, so
// the call falls back to the browser's own suppression instead of going quiet.
function guardFrames(audioContext: AudioContext, node: FilterNode, model: NoiseModel, options: FilterOptions) {
  watchFilterFrames(audioContext, node, options.signal, reason => {
    console.warn(`AI noise suppression (${model}) gave up: ${reason}, falling back to the browser's suppression`)
    try { node.destroy?.() } catch (err) { console.debug('Filter destroy error:', err) }
    options.onFailure?.()
  })
}
