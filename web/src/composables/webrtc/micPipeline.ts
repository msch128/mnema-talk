import { t } from '../../i18n'
import { useToastStore } from '../../stores/toast'
import { gamingMicLease } from '../../lib/gamingMicLease'
import { createMeteringTrack } from '../../lib/micMetering'
import { isPageUnloading, watchContextStart } from '../../lib/audioHealth'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported } from '../../lib/noiseSuppressor'
import { platform } from '../../lib/platform'
import { applyOutput } from './remoteAudio'
import type { Cx } from './state'
import { aiModel, setNodeGain } from './audioMath'
import { noticeOnce } from './audioNotice'

export async function refreshAudioDevices(cx: Cx) {
  const { voice } = cx
  try {
    if (!navigator.mediaDevices?.enumerateDevices) return
    const devices = await navigator.mediaDevices.enumerateDevices()
    voice.availableInputDevices = devices.filter(d => d.kind === 'audioinput')
    voice.availableOutputDevices = devices.filter(d => d.kind === 'audiooutput')
  } catch (err) {
    console.warn('enumerateDevices error:', err)
  }
}

// The AI filter wants the unprocessed signal; without worklet support the
// browser's own suppression stands in for it.
function useBrowserNoiseSuppression(cx: Cx) {
  const { voice, st } = cx
  const mode = voice.noiseMode
  return mode === 'browser' || (aiModel(mode) !== null && (!isNoiseSuppressionSupported() || aiModel(mode) === st.failedAIModel))
}

export function getAudioConstraints(cx: Cx, withDevice = true) {
  const { voice } = cx
  const audioConstraints: MediaTrackConstraints & { googEchoCancellation: boolean; googAutoGainControl: boolean; googNoiseSuppression: boolean; googHighpassFilter: boolean } = {
    channelCount: 1,
    sampleRate: 48000,
    echoCancellation: voice.echoCancellation,
    noiseSuppression: useBrowserNoiseSuppression(cx),
    autoGainControl: voice.autoGainControl, // Crucial: false prevents boosting background voice
    googEchoCancellation: voice.echoCancellation,
    googAutoGainControl: voice.autoGainControl,
    googNoiseSuppression: useBrowserNoiseSuppression(cx),
    googHighpassFilter: true
  }
  if (withDevice && voice.selectedInputDeviceId) {
    audioConstraints.deviceId = { exact: voice.selectedInputDeviceId }
  }
  return audioConstraints
}

function errorName(err: unknown) {
  return typeof err === 'object' && err !== null && 'name' in err ? err.name : undefined
}

/** Tells the user why the microphone failed instead of leaving them silent. */
export function micErrorKey(err: unknown) {
  switch (errorName(err)) {
    case 'NotAllowedError':
    case 'SecurityError':
      return 'voice.micDenied'
    case 'NotFoundError':
    case 'OverconstrainedError':
      return 'voice.micNotFound'
    case 'NotReadableError':
    case 'AbortError':
      return 'voice.micBusy'
    default:
      return 'voice.micFailed'
  }
}

/**
 * Captures the microphone. A saved device that is gone (unplugged headset)
 * must not silence every later join: the capture falls back to the default
 * device once, forgets the dead id and says so. Any other failure is thrown.
 */
export async function requestMicrophone(cx: Cx): Promise<MediaStream> {
  const { voice } = cx
  try {
    return await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints(cx) })
  } catch (err) {
    const name = errorName(err)
    if (!voice.selectedInputDeviceId || (name !== 'NotFoundError' && name !== 'OverconstrainedError')) throw err
    const stream = await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints(cx, false) })
    voice.selectedInputDeviceId = ''
    useToastStore().info(t('voice.micDeviceReset'))
    return stream
  }
}

// Builds the mic-test monitor (what I hear of myself) once, on the given
// context after the high-pass filter. Starts silent; the meter interval
// opens it with the gate.
export function connectLoopback(cx: Cx, ctx: AudioContext, source: AudioNode) {
  const { voice, st } = cx
  if (st.loopbackGain || !ctx || !source) return
  try {
    const gain = ctx.createGain()
    setNodeGain(gain, 0, ctx.currentTime)
    source.connect(gain)
    gain.connect(ctx.destination)
    st.loopbackGain = gain
    st.loopbackCtx = ctx
    // The monitor plays on the same device as the voices in a call.
    applyOutput(voice, ctx)
  } catch (err) {
    console.warn('[WebRTC] Mic test loopback setup failed:', err)
  }
}

function disconnectNodeSafely(node: AudioNode | null | undefined, label: string) {
  if (!node) return
  try {
    node.disconnect()
  } catch (err) {
    console.debug(`[WebRTC] ${label} disconnect:`, err)
  }
}

function destroySuppressorSafely(node: { disconnect: () => void; destroy?: () => void } | null | undefined) {
  if (!node) return
  try {
    node.disconnect()
    node.destroy?.()
  } catch (err) {
    console.debug('[WebRTC] suppressor destroy:', err)
  }
}

function stopMediaTracks(stream: MediaStream | null | undefined) {
  if (!stream) return
  for (const track of stream.getTracks()) {
    track.stop()
  }
}

function closeAudioContextSafely(ctx: AudioContext | null | undefined) {
  if (!ctx) return
  ctx.close().catch(err => {
    console.debug('[WebRTC] AudioContext close:', err)
  })
}

export function disconnectLoopback(cx: Cx) {
  const { st } = cx
  if (!st.loopbackGain) return
  disconnectNodeSafely(st.loopbackGain, 'loopbackGain')
  st.loopbackGain = null
  st.loopbackCtx = null
}

function stopTracks(stream: MediaStream) {
  stream.getTracks().forEach(track => track.stop())
}

function openAudioContext() {
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  try {
    return new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
  } catch (err) {
    console.warn('AudioContext setup error:', err)
    return null
  }
}

interface FilterStage { aborted: boolean; sendStream: MediaStream; meterSource: AudioNode | null }

// With an AI filter the sent stream is the model's output and the meter taps
// the model, not the raw capture.
async function applyNoiseFilter(cx: Cx, ctx: AudioContext, stream: MediaStream, controller: AbortController): Promise<FilterStage> {
  const { voice, st } = cx
  const stage: FilterStage = { aborted: false, sendStream: stream, meterSource: null }
  const model = aiModel(voice.noiseMode)
  if (!model || model === st.failedAIModel) return stage
  if (!isNoiseSuppressionSupported()) {
    // Said once; the capture then asks the browser for its own suppression.
    noticeOnce('audio.filterUnsupported')
    return stage
  }
  const node = await createNoiseSuppressorNode(ctx, model, { signal: controller.signal, onFailure: () => cx.hooks.filterFailed(ctx, model) })
  // Torn down (left or settings changed again) while the model loaded.
  if (st.audioContext !== ctx) {
    stopTracks(stream)
    stage.aborted = true
    return stage
  }
  if (!node) {
    // Cancelled by a reload or a newer setup: nothing failed.
    if (!controller.signal.aborted && !isPageUnloading()) cx.hooks.filterFailed(ctx, model)
    return stage
  }
  try {
    const destination = ctx.createMediaStreamDestination()
    ctx.createMediaStreamSource(stream).connect(node)
    node.connect(destination)
    st.suppressorNode = node
    st.rawMicStream = stream
    stage.sendStream = destination.stream
    stage.meterSource = node
  } catch (err) {
    destroySuppressorSafely(node)
    // A capture whose sample rate the context refuses to connect (Firefox): never leave the member without a microphone.
    if (!(err instanceof DOMException && err.name === 'NotSupportedError')) throw err
    console.warn('[WebRTC] AI filter could not be connected, using browser suppression:', err)
    cx.hooks.filterFailed(ctx, model)
  }
  return stage
}

// Input volume other than 100 %: microphone (or AI filter) -> gain -> sent stream.
function applyInputVolume(cx: Cx, ctx: AudioContext, stream: MediaStream, sendStream: MediaStream) {
  const { voice, st } = cx
  const inputVolume = Number(voice.inputVolume ?? 100)
  if (inputVolume === 100) return sendStream
  try {
    const gain = ctx.createGain()
    setNodeGain(gain, inputVolume / 100, ctx.currentTime)
    const destination = ctx.createMediaStreamDestination()
    if (st.suppressorNode) {
      st.suppressorNode.disconnect()
      st.suppressorNode.connect(gain)
    } else {
      ctx.createMediaStreamSource(stream).connect(gain)
      st.rawMicStream = stream
    }
    gain.connect(destination)
    st.inputGainNode = gain
    return destination.stream
  } catch (err) {
    console.warn('[WebRTC] Input volume stage unavailable:', err)
    return sendStream
  }
}

// Without a filter or an input gain the capture used to be sent as it is and
// metered through a clone. The gate switches the sent track off (track.enabled)
// whenever the level is below the threshold, and WebKit then sometimes left the
// clone silent for good: the meter read 0 %, the gate never reopened and the
// member stayed silent for the whole call (seen on a reload, with the capture
// not yet delivering when the gate first closed). The capture is therefore
// routed through the audio context like the filtered one: the capture track
// stays enabled and feeds both the meter and the sent stream, and the gate
// only switches the sent stream's track. Returns null (the capture is then
// sent as it is) where the audio graph cannot be built.
function routeCapture(cx: Cx, ctx: AudioContext, stream: MediaStream): { sendStream: MediaStream; meterSource: AudioNode } | null {
  try {
    const source = ctx.createMediaStreamSource(stream)
    const destination = ctx.createMediaStreamDestination()
    source.connect(destination)
    cx.st.rawMicStream = stream
    return { sendStream: destination.stream, meterSource: source }
  } catch (err) {
    console.warn('[WebRTC] Capture routing unavailable:', err)
    return null
  }
}

// Only the desktop gaming bridge needs a native-key lease. The sent
// microphone passes this gain even at 100% volume and without an AI filter.
// Resolves to null when the lease cannot be set up.
function applyGamingLease(cx: Cx, ctx: AudioContext, stream: MediaStream, sendStream: MediaStream) {
  const { voice, st } = cx
  if (platform.gamingBridgeToken() === null || window.location.protocol !== 'https:') return sendStream
  try {
    const gain = ctx.createGain()
    const destination = ctx.createMediaStreamDestination()
    ctx.createMediaStreamSource(sendStream).connect(gain)
    gain.connect(destination)
    gamingMicLease(gain, ctx, voice.inputMode === 'ptt', false)
    st.gamingGainNode = gain
    st.rawMicStream ??= stream
    return destination.stream
  } catch (err) {
    console.warn('[WebRTC] Gaming bridge lease setup error:', err)
    stopTracks(stream)
    stopTracks(sendStream)
    teardownMicPipeline(cx)
    return null // Never send an ungated desktop microphone after setup failure.
  }
}

// The speaking meter: a high-passed tap of the filter output or, without a
// filter, of an always-enabled clone of the raw stream.
function attachMeter(cx: Cx, ctx: AudioContext, stream: MediaStream, filterOutput: AudioNode | null) {
  const { voice, st } = cx
  try {
    st.analyser = ctx.createAnalyser()
    st.analyser.fftSize = 256
    st.analyser.smoothingTimeConstant = 0.2
    let meterSource = filterOutput
    if (!meterSource) {
      st.meteringTrack = createMeteringTrack(stream)
      meterSource = ctx.createMediaStreamSource(st.meteringTrack ? new MediaStream([st.meteringTrack]) : stream)
    }
    const biquad = ctx.createBiquadFilter()
    biquad.type = 'highpass'
    biquad.frequency.setValueAtTime(85, ctx.currentTime)
    meterSource.connect(biquad)
    biquad.connect(st.analyser)
    st.callBiquad = biquad

    if (voice.isMicTesting) connectLoopback(cx, ctx, biquad)
  } catch (err) {
    console.warn('AudioContext speaking detector setup error:', err)
  }
}

// Creates the audio context for a freshly captured mic stream and returns
// the stream to send. With an AI filter that is the model's output: the raw
// capture stays enabled and only feeds the model, so the meter (tapping the
// model output) keeps working while the gate or mute disables the sent
// track, and typing or clicks don't count as speaking. Otherwise the raw
// stream is sent and metered through an always-enabled clone (see
// lib/micMetering).
export async function setupMicPipeline(cx: Cx, stream: MediaStream): Promise<MediaStream | null> {
  const { st } = cx
  const ctx = openAudioContext()
  if (!ctx) {
    if (platform.gamingBridgeToken() !== null) { stopTracks(stream); return null }
    return stream
  }
  st.audioContext = ctx
  const controller = new AbortController()
  st.micPipelineController = controller
  watchContextStart(ctx, controller.signal, () => cx.hooks.contextStalled(ctx))
  // Not awaited: after a reload (voice resume) there is no user gesture yet
  // and resume() may stay pending until there is one; the graph starts then.
  if (ctx.state === 'suspended') {
    ctx.resume().catch(err => {
      console.debug('[WebRTC] AudioContext resume:', err)
    })
  }

  const filtered = await applyNoiseFilter(cx, ctx, stream, controller)
  if (filtered.aborted) return null
  const volumed = applyInputVolume(cx, ctx, stream, filtered.sendStream)
  const routed = volumed === stream ? routeCapture(cx, ctx, stream) : null
  const sendStream = applyGamingLease(cx, ctx, stream, routed?.sendStream ?? volumed)
  if (!sendStream) return null
  attachMeter(cx, ctx, stream, filtered.meterSource ?? routed?.meterSource ?? null)
  return sendStream
}

export function teardownMicPipeline(cx: Cx) {
  const { st } = cx
  disconnectNodeSafely(st.gamingGainNode, 'gamingGainNode')
  st.gamingGainNode = null

  st.micPipelineController?.abort()
  st.micPipelineController = null
  st.callBiquad = null

  disconnectNodeSafely(st.inputGainNode, 'inputGainNode')
  st.inputGainNode = null

  if (st.loopbackCtx && st.loopbackCtx === st.audioContext) {
    disconnectLoopback(cx)
  }

  destroySuppressorSafely(st.suppressorNode)
  st.suppressorNode = null

  stopMediaTracks(st.rawMicStream)
  st.rawMicStream = null

  st.meteringTrack?.stop()
  st.meteringTrack = null

  closeAudioContextSafely(st.audioContext)
  st.audioContext = null
  st.analyser = null
}

