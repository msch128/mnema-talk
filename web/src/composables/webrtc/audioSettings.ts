import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import type { NoiseModel } from '../../lib/dfn/dfnTypes'
import type { Cx } from './state'
import { getAudioConstraints, setupMicPipeline, teardownMicPipeline } from './micPipeline'
import { startMicTest } from './micTest'
import { activeAudioTrack, applyQos } from './senders'
import { startSpeakingDetection } from './voiceAudio'

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
    return await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints(cx) })
  } catch (err) {
    console.warn('Could not switch microphone:', err)
    return null
  }
}

function stopMediaTracks(stream?: MediaStream | null) {
  stream?.getTracks().forEach(t => t.stop())
}

function teardownCurrentAudio(cx: Cx) {
  const { st } = cx
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  const old = st.localAudioStream.value
  teardownMicPipeline(cx)
  stopMediaTracks(old)
}

async function installSwappedTrack(cx: Cx, sendStream: MediaStream) {
  const { voice, st } = cx
  const track = sendStream.getAudioTracks()[0]
  if (track) track.enabled = !voice.isMuted
  st.localAudioStream.value = sendStream
  voice.localAudioStream = sendStream
  const outgoing = activeAudioTrack(cx)
  if (st.audioSender && outgoing) {
    await st.audioSender.replaceTrack(outgoing).catch(() => {})
  }
  startSpeakingDetection(cx)
}

async function swapMicrophone(cx: Cx) {
  const { st } = cx
  await applyQos(cx)
  if (!st.localAudioStream.value) return
  const gen = st.joinGeneration
  const stream = await acquireMicrophone(cx)
  if (!stream) return
  if (gen !== st.joinGeneration || !st.localAudioStream.value) {
    stopMediaTracks(stream)
    return
  }
  teardownCurrentAudio(cx)
  const sendStream = await setupMicPipeline(cx, stream)
  if (gen !== st.joinGeneration || !sendStream) {
    stopMediaTracks(sendStream)
    stopMediaTracks(stream)
    return
  }
  await installSwappedTrack(cx, sendStream)
}

