import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import { preloadNoiseSuppressor } from '../../lib/noiseSuppressor'
import type { NoiseModel } from '../../lib/dfn/dfnTypes'
import type { Cx } from './state'
import { micErrorKey, requestMicrophone, setupMicPipeline, teardownMicPipeline } from './micPipeline'
import { startMicTest } from './micTest'
import { aiModel } from './audioMath'
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
  useToastStore().info(t('audio.filterDegraded'))
  // Allow the current setup to finish before acquiring the native fallback.
  // The old downstream mute/gate remains active throughout this transition.
  setTimeout(() => {
    if (targetAudioContext(st, test) !== ctx) return
    reapplyDegradedFilter(cx, test)
  }, 0)
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
    useToastStore().error(t(micErrorKey(err)))
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
  useToastStore().error(t('voice.micLost'))
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

