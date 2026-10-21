import { playSoundEffect } from '../../lib/soundEffects'
import { DEFAULT_STREAM_QUALITY, normalizeQuality, SCREEN_QUALITY_STORAGE_KEY } from '../../lib/streamQuality'
import type { ScreenQuality } from '../../lib/streamQuality'
import type { NoiseMode } from '../voiceSettings'
import type { VoiceState } from './state'

export function recordPing(cs: VoiceState, rtt: number) {
  if (typeof rtt !== 'number' || isNaN(rtt) || rtt < 0) return
  const rounded = Math.min(9999, Math.round(rtt))
  cs.ping.value = rounded

  const updated = [...cs.pingHistory.value, rounded]
  if (updated.length > 30) updated.shift()
  cs.pingHistory.value = updated
}

export function toggleMute(cs: VoiceState) {
  cs.isMuted.value = !cs.isMuted.value
  playSoundEffect(cs.isMuted.value ? 'mute' : 'unmute')
  if (cs.localAudioStream.value) {
    cs.localAudioStream.value.getAudioTracks().forEach(track => {
      track.enabled = !cs.isMuted.value
    })
  }
}

export function toggleDeafen(cs: VoiceState) {
  cs.isDeafened.value = !cs.isDeafened.value
  playSoundEffect(cs.isDeafened.value ? 'deafen' : 'undeafen')
  if (cs.isDeafened.value) {
    cs.isMuted.value = true
    if (cs.localAudioStream.value) {
      cs.localAudioStream.value.getAudioTracks().forEach(track => {
        track.enabled = false
      })
    }
  }
}

export function setNoiseMode(cs: VoiceState, mode: NoiseMode) {
  cs.settings.noiseMode.value = mode
  cs.settings.saveSettings()
}

// Quick toggle in the call bar: off <-> AI filter.
export function toggleNoiseCancelling(cs: VoiceState) {
  setNoiseMode(cs, cs.settings.noiseMode.value === 'off' ? 'ai' : 'off')
}

export function toggleScreenAudioMute(cs: VoiceState) {
  cs.isScreenAudioMuted.value = !cs.isScreenAudioMuted.value
}

// q: { resolution, fps, custom? } (see useStreamQuality).
export function setScreenQuality(cs: VoiceState, q: ScreenQuality) {
  const norm = { ...normalizeQuality(q), ...(q?.custom === true ? { custom: true } : {}) }
  cs.screenQuality.value = norm
  try {
    localStorage.setItem(SCREEN_QUALITY_STORAGE_KEY, JSON.stringify(norm))
  } catch (error) {
    void error
    // Storage blocked or full: keeps quality for this session only.
  }
}

export function resetScreenQuality(cs: VoiceState) {
  cs.screenQuality.value = { ...DEFAULT_STREAM_QUALITY }
}

export function openScreenShareModal(cs: VoiceState) {
  cs.showScreenShareModal.value = true
}

export function closeScreenShareModal(cs: VoiceState) {
  cs.showScreenShareModal.value = false
}

