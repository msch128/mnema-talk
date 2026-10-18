import { gamingMicLease } from '../../lib/gamingMicLease'
import { createMeteringTrack } from '../../lib/micMetering'
import { createNoiseSuppressorNode, isNoiseSuppressionSupported } from '../../lib/noiseSuppressor'
import { platform } from '../../lib/platform'
import { applyOutput } from './remoteAudio'
import type { Cx } from './state'
import { aiModel, setNodeGain } from './audioMath'

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

export function getAudioConstraints(cx: Cx) {
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
  if (voice.selectedInputDeviceId) {
    audioConstraints.deviceId = { exact: voice.selectedInputDeviceId }
  }
  return audioConstraints
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

export function disconnectLoopback(cx: Cx) {
  const { st } = cx
  if (!st.loopbackGain) return
  try { st.loopbackGain.disconnect() } catch { /* ignore */ }
  st.loopbackGain = null
  st.loopbackCtx = null
}

// Creates the audio context for a freshly captured mic stream and returns
// the stream to send. With an AI filter that is the model's output: the raw
// capture stays enabled and only feeds the model, so the meter (tapping the
// model output) keeps working while the gate or mute disables the sent
// track, and typing or clicks don't count as speaking. Otherwise the raw
// stream is sent and metered through an always-enabled clone (see
// lib/micMetering).
export async function setupMicPipeline(cx: Cx, stream: MediaStream): Promise<MediaStream | null> {
  const { voice, st } = cx
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  let ctx
  try {
    ctx = new AudioCtx({ latencyHint: 'interactive', sampleRate: 48000 })
  } catch (err) {
    console.warn('AudioContext setup error:', err)
    if (platform.gamingBridgeToken() !== null) { stream.getTracks().forEach(track => track.stop()); return null }
    return stream
  }
  st.audioContext = ctx
  const controller = new AbortController()
  st.micPipelineController = controller
  // Not awaited: after a reload (voice resume) there is no user gesture yet
  // and resume() may stay pending until there is one; the graph starts then.
  if (ctx.state === 'suspended') ctx.resume().catch(() => {})

  let sendStream = stream
  let meterSource = null
  const model = aiModel(voice.noiseMode)
  if (model && model !== st.failedAIModel && isNoiseSuppressionSupported()) {
    const node = await createNoiseSuppressorNode(ctx, model, { signal: controller.signal, onFailure: () => cx.hooks.filterFailed(ctx, model) })
    // Torn down (left or settings changed again) while the model loaded.
    if (st.audioContext !== ctx) {
      stream.getTracks().forEach(t => t.stop())
      return null
    }
    if (node) {
      const destination = ctx.createMediaStreamDestination()
      ctx.createMediaStreamSource(stream).connect(node)
      node.connect(destination)
      st.suppressorNode = node
      st.rawMicStream = stream
      sendStream = destination.stream
      meterSource = node
    } else cx.hooks.filterFailed(ctx, model)
  }

  // Input volume other than 100 %: microphone (or AI filter) -> gain -> sent stream.
  const inputVolume = Number(voice.inputVolume ?? 100)
  if (inputVolume !== 100) {
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
      sendStream = destination.stream
    } catch (err) {
      console.warn('[WebRTC] Input volume stage unavailable:', err)
    }
  }

  // Only the desktop gaming bridge needs a native-key lease. The sent
  // microphone passes this gain even at 100% volume and without an AI filter.
  if (platform.gamingBridgeToken() !== null && window.location.protocol === 'https:') {
    try {
      const gain = ctx.createGain()
      const destination = ctx.createMediaStreamDestination()
      ctx.createMediaStreamSource(sendStream).connect(gain)
      gain.connect(destination)
      gamingMicLease(gain, ctx, voice.inputMode === 'ptt', false)
      st.gamingGainNode = gain
      st.rawMicStream ??= stream
      sendStream = destination.stream
    } catch {
      stream.getTracks().forEach(track => track.stop())
      sendStream.getTracks().forEach(track => track.stop())
      teardownMicPipeline(cx)
      return null // Never send an ungated desktop microphone after setup failure.
    }
  }

  try {
    st.analyser = ctx.createAnalyser()
    st.analyser.fftSize = 256
    st.analyser.smoothingTimeConstant = 0.2
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
  return sendStream
}

export function teardownMicPipeline(cx: Cx) {
  const { st } = cx
  if (st.gamingGainNode) {
    try { st.gamingGainNode.disconnect() } catch { /* already disconnected */ }
    st.gamingGainNode = null
  }
  st.micPipelineController?.abort()
  st.micPipelineController = null
  st.callBiquad = null
  if (st.inputGainNode) {
    try { st.inputGainNode.disconnect() } catch { /* ignore */ }
    st.inputGainNode = null
  }
  if (st.loopbackCtx && st.loopbackCtx === st.audioContext) disconnectLoopback(cx)
  if (st.suppressorNode) {
    try {
      st.suppressorNode.disconnect()
      st.suppressorNode.destroy?.()
    } catch { /* ignore */ }
    st.suppressorNode = null
  }
  if (st.rawMicStream) {
    st.rawMicStream.getTracks().forEach(t => t.stop())
    st.rawMicStream = null
  }
  if (st.meteringTrack) {
    st.meteringTrack.stop()
    st.meteringTrack = null
  }
  if (st.audioContext) {
    st.audioContext.close().catch(() => {})
    st.audioContext = null
  }
  st.analyser = null
}

