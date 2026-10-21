import { useToastStore } from '../../stores/toast'
import { createVoiceGate } from '../../lib/levelMeter'
import { t } from '../../i18n'
import { gamingMicLease } from '../../lib/gamingMicLease'
import { useVoiceStore } from '../../stores/voice'
import type { Cx, RtcState } from './state'
import { calculateRMSLevel, setNodeGain } from './audioMath'
import { teardownMicPipeline } from './micPipeline'

/** Input volume: the gaming microphone lease follows push-to-talk. */
export function setGamingPttLease(st: RtcState, pressed: boolean): void {
  if (st.gamingGainNode && st.audioContext) gamingMicLease(st.gamingGainNode, st.audioContext, useVoiceStore().inputMode === 'ptt', pressed)
}

/** What one speaking-detection tick carries from the last one. */
interface SpeakingTick {
  buffer: Uint8Array<ArrayBuffer>
  gateOpen: (level: number) => boolean
  wasSpeaking: boolean
  lastGamingTick: number
}

// Nothing goes out: the speaking ring ends and the track is disabled.
function stopTransmitting(cx: Cx, tick: SpeakingTick) {
  const { chat, st } = cx
  if (tick.wasSpeaking) {
    tick.wasSpeaking = false
    chat.sendWSEvent('voice_speaking', { active: false })
  }
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

function shouldSkipNoAudioWatch(voice: Cx['voice'], level: number): boolean {
  if (!voice.warnNoAudioDetected || level >= 1) return true
  return voice.isMuted || voice.isMicTesting
}

// Warns (at most every two minutes) when an unmuted mic has been silent for 15 s.
function watchForNoAudio(cx: Cx, level: number) {
  const { voice, st } = cx
  if (shouldSkipNoAudioWatch(voice, level)) {
    st.unmutedZeroAudioStartTime = 0
    return
  }
  if (!st.unmutedZeroAudioStartTime) {
    st.unmutedZeroAudioStartTime = Date.now()
    return
  }
  if (Date.now() - st.unmutedZeroAudioStartTime <= 15000) return
  if (Date.now() - st.lastNoAudioWarningToastTime <= 120000) return
  useToastStore().info(t('audio.noAudioDetectedToast'))
  st.lastNoAudioWarningToastTime = Date.now()
}

// Mic testing in a call: channel transmission is muted and the loopback is gated.
function gateMicTestLoopback(cx: Cx, tick: SpeakingTick, level: number) {
  const { voice, st } = cx
  stopTransmitting(cx, tick)
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

  // Physical audio track gate (muting track when below threshold to eliminate background bleed)
  if (st.localAudioStream.value) {
    const track = st.localAudioStream.value.getAudioTracks()[0]
    if (track && track.enabled !== shouldTransmit) {
      track.enabled = shouldTransmit
    }
  }

  // Animated speaking halo state
  if (shouldTransmit !== tick.wasSpeaking) {
    tick.wasSpeaking = shouldTransmit
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
  watchForNoAudio(cx, level)

  if (voice.isMicTesting) gateMicTestLoopback(cx, tick, level)
  else if (voice.isMuted) stopTransmitting(cx, tick)
  else gateTransmission(cx, tick, level)
}

export function startSpeakingDetection(cx: Cx) {
  const { voice, st } = cx
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  if (!st.analyser) return

  const tick: SpeakingTick = {
    buffer: new Uint8Array(st.analyser.fftSize),
    gateOpen: createVoiceGate(voice),
    wasSpeaking: false,
    lastGamingTick: performance.now()
  }
  st.speakingInterval = setInterval(() => speakingTick(cx, tick), 60)
}

export function cleanupVoiceAudio(cx: Cx) {
  const { voice, st } = cx
  st.failedAIModel = null
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }

  st.pushToTalk.detach()

  if (st.localAudioStream.value) {
    st.localAudioStream.value.getTracks().forEach(t => t.stop())
    st.localAudioStream.value = null
  }
  voice.localAudioStream = null

  teardownMicPipeline(cx)
  voice.currentInputLevel = 0
}

