import { useToastStore } from '../../stores/toast'
import { t } from '../../i18n'
import type { NoiseModel } from '../../lib/dfn/dfnTypes'
import type { Cx } from './state'
import { getAudioConstraints, setupMicPipeline, teardownMicPipeline } from './micPipeline'
import { startMicTest } from './micTest'
import { activeAudioTrack, applyQos } from './senders'
import { startSpeakingDetection } from './voiceAudio'

export function recoverFilter(cx: Cx, ctx: AudioContext, model: NoiseModel, test = false) {
  const { voice, st } = cx
  if ((test ? st.testAudioContext : st.audioContext) !== ctx || st.failedAIModel === model) return
  st.failedAIModel = model
  useToastStore().info(t('audio.filterDegraded'))
  // Allow the current setup to finish before acquiring the native fallback.
  // The old downstream mute/gate remains active throughout this transition.
  setTimeout(() => {
    if ((test ? st.testAudioContext : st.audioContext) !== ctx) return
    if (test) {
      if (voice.isMicTesting) startMicTest(cx).catch(() => {})
    } else if (st.localAudioStream.value && voice.currentChannelId) {
      applyAudioSettings(cx).catch(() => {})
    }
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

async function swapMicrophone(cx: Cx) {
  const { voice, st } = cx
  await applyQos(cx)
  // Left the call while an earlier run was busy.
  if (!st.localAudioStream.value) return
  const gen = st.joinGeneration
  let stream
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: getAudioConstraints(cx) })
  } catch (err) {
    console.warn('Could not switch microphone:', err)
    return
  }
  if (gen !== st.joinGeneration || !st.localAudioStream.value) {
    stream.getTracks().forEach(t => t.stop())
    return
  }
  const old = st.localAudioStream.value
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  teardownMicPipeline(cx)
  old.getTracks().forEach(t => t.stop())
  const sendStream = await setupMicPipeline(cx, stream)
  if (gen !== st.joinGeneration || !sendStream) {
    // The new capture is not installed yet, so leaving can only stop the
    // previous localAudioStream. Release this run's own streams as well.
    sendStream?.getTracks().forEach(track => track.stop())
    stream.getTracks().forEach(track => track.stop())
    return
  }
  const track = sendStream.getAudioTracks()[0]
  if (track) track.enabled = !voice.isMuted
  st.localAudioStream.value = sendStream
  voice.localAudioStream = sendStream
  const outgoing = activeAudioTrack(cx)
  if (st.audioSender && outgoing) await st.audioSender.replaceTrack(outgoing).catch(() => {})
  startSpeakingDetection(cx)
}

