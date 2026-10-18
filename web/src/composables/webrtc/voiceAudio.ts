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

export function startSpeakingDetection(cx: Cx) {
  const { voice, chat, st } = cx
  if (st.speakingInterval) {
    clearInterval(st.speakingInterval)
    st.speakingInterval = null
  }
  if (!st.analyser) return

  const buffer = new Uint8Array(st.analyser.fftSize)
  let wasSpeaking = false
  const gateOpen = createVoiceGate(voice)
  let lastGamingTick = performance.now()

  st.speakingInterval = setInterval(() => {
    const now = performance.now()
    if (st.gamingGainNode && voice.inputMode === 'ptt' && now - lastGamingTick > 250) {
      // An overdue callback cannot renew the pre-stall pressed state. A
      // fresh native pulse or local key press must establish it again.
      voice.isPttPressed = false
      setGamingPttLease(st, false)
    }
    lastGamingTick = now
    if (!st.analyser) return

    const level = calculateRMSLevel(st.analyser, buffer)

    // PERFORMANCE FIX: Only update reactive Pinia store if settings modal is open
    if (voice.showAudioSettings) {
      voice.currentInputLevel = level
    }

    // No Audio Detected Warning check
    if (voice.warnNoAudioDetected && !voice.isMuted && !voice.isMicTesting) {
      if (level < 1) {
        if (!st.unmutedZeroAudioStartTime) st.unmutedZeroAudioStartTime = Date.now()
        else if (Date.now() - st.unmutedZeroAudioStartTime > 15000) {
          if (Date.now() - st.lastNoAudioWarningToastTime > 120000) {
            useToastStore().info(t('audio.noAudioDetectedToast'))
            st.lastNoAudioWarningToastTime = Date.now()
          }
        }
      } else {
        st.unmutedZeroAudioStartTime = 0
      }
    } else {
      st.unmutedZeroAudioStartTime = 0
    }

    // If mic testing in a call: mute channel transmission and gate loopback
    if (voice.isMicTesting) {
      if (wasSpeaking) {
        wasSpeaking = false
        chat.sendWSEvent('voice_speaking', { active: false })
      }
      if (st.localAudioStream.value) {
        const track = st.localAudioStream.value.getAudioTracks()[0]
        if (track && track.enabled) track.enabled = false
      }
      const open = gateOpen(level)
      if (st.loopbackGain && st.audioContext && st.loopbackCtx === st.audioContext) {
        setNodeGain(st.loopbackGain, open ? (voice.outputVolume / 100) : 0, st.audioContext.currentTime)
      }
      return
    }

    if (voice.isMuted) {
      if (wasSpeaking) {
        wasSpeaking = false
        chat.sendWSEvent('voice_speaking', { active: false })
      }
      if (st.localAudioStream.value) {
        const track = st.localAudioStream.value.getAudioTracks()[0]
        if (track && track.enabled) track.enabled = false
      }
      return
    }

    // Noise gate: push-to-talk, or voice activity with hangover.
    setGamingPttLease(st, voice.isPttPressed)
    const shouldTransmit = gateOpen(level)

    // Physical audio track gate (muting track when below threshold to eliminate background bleed)
    if (st.localAudioStream.value) {
      const track = st.localAudioStream.value.getAudioTracks()[0]
      if (track && track.enabled !== shouldTransmit) {
        track.enabled = shouldTransmit
      }
    }

    // Animated speaking halo state
    if (shouldTransmit !== wasSpeaking) {
      wasSpeaking = shouldTransmit
      chat.sendWSEvent('voice_speaking', { active: shouldTransmit })
    }
  }, 60)
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

