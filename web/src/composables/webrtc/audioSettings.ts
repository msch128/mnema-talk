import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { preloadNoiseSuppressor } from '../../lib/noiseSuppressor'
import type { NoiseModel } from '../../lib/dfn/dfnTypes'
import type { Cx } from './state'
import { micErrorKey, requestMicrophone, setupMicPipeline, teardownMicPipeline } from './micPipeline'
import { startMicTest } from './micTest'
import { aiModel } from './audioMath'
import { noticeOnce, withAudioSettings } from './audioNotice'
import { activeAudioTrack, applyQos } from './senders'
import { startSpeakingDetection, stopSpeakingDetection } from './voiceAudio'

function targetAudioContext(st: Cx['st'], test: boolean): AudioContext | null {
  return test ? st.testAudioContext : st.audioContext
}

function reapplyDegradedFilter(cx: Cx, test: boolean) {
  const { voice, st } = cx
  if (test) {
    if (voice.isMicTesting) startMicTest(cx).catch(() => {})
    return
  }
  if (st.localAudioStream.value && voice.currentChannelId) {
    applyAudioSettings(cx).catch(() => {})
  }
}

export function recoverFilter(cx: Cx, ctx: AudioContext, model: NoiseModel, test = false) {
  const { st } = cx
  if (targetAudioContext(st, test) !== ctx || st.failedAIModel === model) return
  st.failedAIModel = model
  useToastStore().info(t('audio.filterDegraded'), withAudioSettings('noise'))
  // Allow the current setup to finish before acquiring the native fallback.
  // The old downstream mute/gate remains active throughout this transition.
  setTimeout(() => {
    if (targetAudioContext(st, test) !== ctx) return
    reapplyDegradedFilter(cx, test)
  }, 0)
}

/**
 * The audio context did not start rendering (no audio output device, or the
 * browser holds it back until a gesture). Whatever is routed through it is
 * silent, so the call is rebuilt around it: no AI filter, the capture sent as
 * it is, the gate open (see voiceAudio).
 */
export function recoverContext(cx: Cx, ctx: AudioContext) {
  const { voice, st } = cx
  if (st.audioContext !== ctx) return
  restoreWhenRunning(cx, ctx)
  if (st.contextStalled) return
  console.warn('[WebRTC] The audio context did not start; sending the microphone around it')
  st.contextStalled = true
  const model = aiModel(voice.noiseMode)
  if (model) st.failedAIModel = model
  noticeOnce('audio.engineStalled', 'output-device')
  setTimeout(() => {
    if (st.audioContext !== ctx) return
    reapplyDegradedFilter(cx, false)
  }, 0)
}

// A context that only waited for a gesture runs eventually, and the call then
// gets its AI filter and input volume back. The rebuilt call's own context is
// watched the same way.
function restoreWhenRunning(cx: Cx, ctx: AudioContext) {
  const { voice, st } = cx
  const onChange = () => {
    if (st.audioContext !== ctx) { ctx.removeEventListener('statechange', onChange); return }
    if (ctx.state !== 'running') return
    ctx.removeEventListener('statechange', onChange)
    st.contextStalled = false
    const model = aiModel(voice.noiseMode)
    if (model && st.failedAIModel === model) st.failedAIModel = null
    reapplyDegradedFilter(cx, false)
  }
  ctx.addEventListener('statechange', onChange)
}

// Applies changed input settings (device, AGC, noise suppression, echo
// cancellation). In a call the mic is re-acquired and swapped into the
// running connection without renegotiation; otherwise the mic test restarts.
// Runs are serialized: two overlapping swaps would each tear down the
// other's pipeline.
export function applyAudioSettings(cx: Cx) {
  const { st } = cx
  if (!st.localAudioStream.value && !st.audioSettingsBusy) {
    return applyQos(cx).then(() => startMicTest(cx))
  }
  st.audioSettingsBusy++
  const run = st.audioSettingsChain.then(() => swapMicrophone(cx))
  st.audioSettingsChain = run.catch(() => {}).finally(() => { st.audioSettingsBusy-- })
  return run
}

async function acquireMicrophone(cx: Cx): Promise<MediaStream | null> {
  try {
    return await requestMicrophone(cx)
  } catch (err) {
    console.warn('Could not switch microphone:', err)
    // The running microphone stays; the user learns the new one was not applied.
    useToastStore().error(t(micErrorKey(err)), withAudioSettings('input-device'))
    return null
  }
}

function stopMediaTracks(stream?: MediaStream | null) {
  stream?.getTracks().forEach(t => t.stop())
}

function teardownCurrentAudio(cx: Cx) {
  const { st } = cx
  stopSpeakingDetection(cx)
  const old = st.localAudioStream.value
  teardownMicPipeline(cx)
  stopMediaTracks(old)
}

async function installSwappedTrack(cx: Cx, sendStream: MediaStream) {
  const { voice, st } = cx
  const track = sendStream.getAudioTracks()[0]
  // Closed like the speaking gate would have it until its first tick.
  if (track) track.enabled = !voice.isMuted && !voice.isMicTesting && (voice.inputMode !== 'ptt' || voice.isPttPressed)
  st.localAudioStream.value = sendStream
  voice.localAudioStream = sendStream
  const outgoing = activeAudioTrack(cx)
  if (st.audioSender && outgoing) {
    await st.audioSender.replaceTrack(outgoing).catch(() => {})
  }
  startSpeakingDetection(cx)
}

// The new pipeline could not be built after the old one was released: the
// call must not keep an ended track that looks alive.
function dropMicrophone(cx: Cx) {
  const { voice, st } = cx
  st.localAudioStream.value = null
  voice.localAudioStream = null
  voice.currentInputLevel = 0
  st.audioSender?.replaceTrack(null).catch(() => {})
  useToastStore().error(t('voice.micLost'), withAudioSettings('input-device'))
}

async function swapMicrophone(cx: Cx) {
  const { voice, st } = cx
  await applyQos(cx)
  if (!st.localAudioStream.value) return
  const gen = st.joinGeneration
  // An AI model downloads before the live microphone is released, so the call
  // is not silent while it loads.
  const model = aiModel(voice.noiseMode)
  const [stream] = await Promise.all([acquireMicrophone(cx), model ? preloadNoiseSuppressor(model) : undefined])
  if (!stream) return
  if (gen !== st.joinGeneration || !st.localAudioStream.value) {
    stopMediaTracks(stream)
    return
  }
  teardownCurrentAudio(cx)
  const sendStream = await setupMicPipeline(cx, stream)
  if (gen !== st.joinGeneration) {
    stopMediaTracks(sendStream)
    stopMediaTracks(stream)
    return
  }
  if (!sendStream) {
    stopMediaTracks(stream)
    dropMicrophone(cx)
    return
  }
  await installSwappedTrack(cx, sendStream)
}

