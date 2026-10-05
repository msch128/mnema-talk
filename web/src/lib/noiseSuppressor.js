// AI noise suppression as an AudioWorklet. Both models run as WebAssembly
// entirely in the browser; worklets, wasm and weights are bundled and served
// by our own binary, so no audio or request ever leaves for a third party.
//
//   'dfn3'  DeepFilterNet3 (third_party/deepfilternet3): full-band 48 kHz, best
//           quality, ~10 MB compressed download once, more CPU.
//   'gtcrn' GTCRN (@sapphi-red/web-noise-suppressor): tiny and light, but
//           works on 16 kHz internally, so voices lose their top end.
import gtcrnWorkletUrl from '@sapphi-red/web-noise-suppressor/gtcrnWorklet.js?url'
import gtcrnWasmUrl from '@sapphi-red/web-noise-suppressor/gtcrn.wasm?url'
import dfnWorkletUrl from '../third_party/deepfilternet3/worklet.js?url'
import dfnWasmUrl from '../third_party/deepfilternet3/df_bg.wasm?url'
import dfnModelUrl from '../third_party/deepfilternet3/DeepFilterNet3_onnx.tgz?url'

// No upper bound on how far DeepFilterNet may attenuate noise (dB).
const DFN_ATTENUATION_LIMIT = 100

const assetPromises = new Map()
let gtcrnModulePromise = null
// addModule is per AudioContext, and contexts are recreated on settings changes.
const registeredWorklets = new WeakMap()

export function isNoiseSuppressionSupported() {
  return typeof AudioWorkletNode !== 'undefined' && typeof WebAssembly !== 'undefined'
}

function fetchOnce(url, transform = res => res.arrayBuffer()) {
  if (!assetPromises.has(url)) {
    const p = fetch(url).then(res => {
      if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`)
      return transform(res)
    })
    p.catch(() => assetPromises.delete(url))
    assetPromises.set(url, p)
  }
  return assetPromises.get(url)
}

const MODELS = {
  dfn3: {
    sampleRates: [48000],
    workletUrl: dfnWorkletUrl,
    load: () => Promise.all([
      // Compiled once on the main thread; the Module is shared with every worklet.
      fetchOnce(dfnWasmUrl, res => res.arrayBuffer().then(buf => WebAssembly.compile(buf))),
      fetchOnce(dfnModelUrl)
    ]),
    create: (ctx, [wasmModule, modelBytes]) => new AudioWorkletNode(ctx, 'deepfilter-audio-processor', {
      processorOptions: { wasmModule, modelBytes, suppressionLevel: DFN_ATTENUATION_LIMIT }
    })
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
    create: (ctx, [{ GtcrnWorkletNode }, wasmBinary]) => new GtcrnWorkletNode(ctx, { maxChannels: 1, wasmBinary })
  }
}

/** Starts downloading a model early (e.g. while the mic permission prompt is open). */
export function preloadNoiseSuppressor(model = 'dfn3') {
  if (isNoiseSuppressionSupported() && MODELS[model]) MODELS[model].load().catch(() => {})
}

/**
 * Returns a mono AudioWorkletNode running the model, or null when the browser
 * can't run it. Callers fall back to the browser's own suppression.
 */
export async function createNoiseSuppressorNode(audioContext, model = 'dfn3') {
  const spec = MODELS[model]
  if (!spec || !isNoiseSuppressionSupported() || !audioContext?.audioWorklet) return null
  if (!spec.sampleRates.includes(audioContext.sampleRate)) return null

  try {
    const assets = await spec.load()
    let registered = registeredWorklets.get(audioContext)
    if (!registered) registeredWorklets.set(audioContext, registered = new Set())
    if (!registered.has(model)) {
      await audioContext.audioWorklet.addModule(spec.workletUrl)
      registered.add(model)
    }
    return spec.create(audioContext, assets)
  } catch (err) {
    console.warn(`AI noise suppression (${model}) unavailable:`, err)
    return null
  }
}
