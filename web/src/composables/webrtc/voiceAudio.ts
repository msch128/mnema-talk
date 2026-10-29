import { createVoiceGate } from '../../lib/levelMeter'
import { gamingMicLease } from '../../lib/gamingMicLease'
import { useVoiceStore } from '../../stores/voice'
import type { Cx, RtcState } from './state'
import { calculateRMSLevel, setNodeGain } from './audioMath'
import { teardownMicPipeline } from './micPipeline'
import { resetNoAudioWatch, watchRawSilence } from './rawSilence'

/** Input volume: the gaming microphone lease follows push-to-talk. */
export function setGamingPttLease(st: RtcState, pressed: boolean): void {
  if (st.gamingGainNode && st.audioContext) gamingMicLease(st.gamingGainNode, st.audioContext, useVoiceStore().inputMode === 'ptt', pressed)
}

/** What one speaking-detection tick carries from the last one. */
interface SpeakingTick {
  buffer: Uint8Array<ArrayBuffer>
  gateOpen: (level: number) => boolean
  lastGamingTick: number
}

/** Takes my speaking ring down for the others, if it is up. */
export function endSpeakingRing({ chat, st }: Cx) {
  if (!st.speakingAnnounced) return
  st.speakingAnnounced = false
  chat.sendWSEvent('voice_speaking', { active: false })
}

// Nothing goes out: the speaking ring ends and the track is disabled.
function stopTransmitting(cx: Cx) {
  const { st } = cx
  endSpeakingRing(cx)
  if (st.localAudioStream.value) {
    const track = st.localAudioStream.value.getAudioTracks()[0]
    if (track && track.enabled) track.enabled = false
  }
}

// An overdue callback cannot renew the pre-stall pressed state. A
// fresh native pulse or local key press must establish it again.
function releaseStalledGamingLease(cx: Cx, tick: SpeakingTick) {
  const { voice, st } = cx
  const now = performance.now()
  if (st.gamingGainNode && voice.inputMode === 'ptt' && now - tick.lastGamingTick > 250) {
    voice.isPttPressed = false
    setGamingPttLease(st, false)
  }
  tick.lastGamingTick = now
}

// Mic testing in a call: channel transmission is muted and the loopback is gated.
function gateMicTestLoopback(cx: Cx, tick: SpeakingTick, level: number) {
  const { voice, st } = cx
  stopTransmitting(cx)
  const open = tick.gateOpen(level)
  if (st.loopbackGain && st.audioContext && st.loopbackCtx === st.audioContext) {
    setNodeGain(st.loopbackGain, open ? (voice.outputVolume / 100) : 0, st.audioContext.currentTime)
  }
}

// Noise gate: push-to-talk, or voice activity with hangover.
function gateTransmission(cx: Cx, tick: SpeakingTick, level: number) {
  const { voice, chat, st } = cx
  setGamingPttLease(st, voice.isPttPressed)
  const shouldTransmit = tick.gateOpen(level)
  // A context that never started reads level 0 for good: voice activity would
  // keep the track closed forever. Push-to-talk needs no meter and stays as is.
  const gateBroken = st.contextStalled && voice.inputMode !== 'ptt' && st.audioContext?.state !== 'running'

  // Physical audio track gate (muting track when below threshold to eliminate background bleed)
  if (st.localAudioStream.value) {
    const track = st.localAudioStream.value.getAudioTracks()[0]
    const send = shouldTransmit || gateBroken
    if (track && track.enabled !== send) {
      track.enabled = send
    }
  }

  // Animated speaking halo state
  if (shouldTransmit !== st.speakingAnnounced) {
    st.speakingAnnounced = shouldTransmit
    chat.sendWSEvent('voice_speaking', { active: shouldTransmit })
  }
}

function speakingTick(cx: Cx, tick: SpeakingTick) {
  const { voice, st } = cx
  releaseStalledGamingLease(cx, tick)
  if (!st.analyser) return

  const level = calculateRMSLevel(st.analyser, tick.buffer)

  // PERFORMANCE FIX: Only update reactive Pinia store if settings modal is open
  if (voice.showAudioSettings) {
    voice.currentInputLevel = level
  }
  watchRawSilence(cx)

  if (voice.isMicTesting) gateMicTestLoopback(cx, tick, level)
  else if (voice.isMuted) stopTransmitting(cx)
  else gateTransmission(cx, tick, level)
}

/** Stops the detection; a ring it had put up comes down with it, as no tick is left to do that. */
export function stopSpeakingDetection(cx: Cx) {
  const { st } = cx
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  endSpeakingRing(cx)
}

export function startSpeakingDetection(cx: Cx) {
  const { voice, st } = cx
  stopSpeakingDetection(cx)
  if (!st.analyser) return

  const tick: SpeakingTick = {
    buffer: new Uint8Array(st.analyser.fftSize),
    gateOpen: createVoiceGate(voice),
    lastGamingTick: performance.now()
  }
  st.speakingInterval = setInterval(() => speakingTick(cx, tick), 60)
}

export function cleanupVoiceAudio(cx: Cx) {
  const { voice, st } = cx
  st.failedAIModel = null
  st.contextStalled = false
  resetNoAudioWatch(st)
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  // The leave (or the end of the call) takes the member's ring down itself.
  st.speakingAnnounced = false

  st.pushToTalk.detach()

  if (st.localAudioStream.value) {
    st.localAudioStream.value.getTracks().forEach(t => t.stop())
    st.localAudioStream.value = null
  }
  voice.localAudioStream = null

  teardownMicPipeline(cx)
  voice.currentInputLevel = 0
}

