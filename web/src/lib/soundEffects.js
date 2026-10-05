// Synthesized Discord-like sound effects using Web Audio API.
// Pure Web Audio: zero external assets, instant playback, zero network latency.
import { useVoiceStore } from '../stores/voice'

let sharedAudioContext = null

function getAudioContext() {
  if (typeof window === 'undefined') return null
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  if (!AudioCtx) return null
  try {
    if (!sharedAudioContext || sharedAudioContext.state === 'closed') {
      sharedAudioContext = new AudioCtx()
    }
    if (sharedAudioContext.state === 'suspended') {
      sharedAudioContext.resume().catch(() => {})
    }
    return sharedAudioContext
  } catch {
    return null
  }
}

/**
 * Helper to play an oscillator tone with envelope.
 */
function playTone(ctx, dest, { freq, endFreq, startTime, duration, type = 'sine', gain = 1 }) {
  try {
    const osc = ctx.createOscillator()
    if (!osc) return
    const g = ctx.createGain()
    if (!g) return
    osc.type = type
    if (typeof osc.frequency?.setValueAtTime === 'function') {
      osc.frequency.setValueAtTime(freq, startTime)
      if (endFreq && endFreq !== freq && typeof osc.frequency?.exponentialRampToValueAtTime === 'function') {
        osc.frequency.exponentialRampToValueAtTime(Math.max(1, endFreq), startTime + duration)
      }
    } else if (osc.frequency) {
      osc.frequency.value = freq
    }

    if (typeof g.gain?.setValueAtTime === 'function') {
      g.gain.setValueAtTime(0.0001, startTime)
      const attack = Math.min(0.01, duration * 0.1)
      if (typeof g.gain?.linearRampToValueAtTime === 'function') {
        g.gain.linearRampToValueAtTime(gain, startTime + attack)
      }
      if (typeof g.gain?.exponentialRampToValueAtTime === 'function') {
        g.gain.exponentialRampToValueAtTime(0.0001, startTime + duration)
      }
    } else if (g.gain) {
      g.gain.value = gain
    }

    osc.connect?.(g)
    g.connect?.(dest)
    osc.start?.(startTime)
    osc.stop?.(startTime + duration + 0.02)
  } catch {
    // Audio node failed or scheduled in past
  }
}

/**
 * Play a synthesized sound effect by name.
 * Supported names: 'join', 'leave', 'mute', 'unmute', 'deafen', 'undeafen', 'user_join', 'user_leave'
 */
export function playSound(name, overrideVolume = null, force = false) {
  let voiceStore = null
  try {
    voiceStore = useVoiceStore()
  } catch {
    // Outside Pinia context
  }

  if (voiceStore && !force) {
    if (!voiceStore.soundEffectsEnabled) return
    if (voiceStore.soundEvents && voiceStore.soundEvents[name] === false) return
  }

  const masterVol = typeof overrideVolume === 'number'
    ? overrideVolume
    : ((voiceStore?.soundEffectsVolume ?? 80) / 100)

  if (masterVol <= 0) return

  const ctx = getAudioContext()
  if (!ctx) return

  const now = ctx.currentTime || 0
  const masterGain = ctx.createGain()
  if (!masterGain) return
  const clampedVol = Math.min(1, Math.max(0, masterVol))
  if (typeof masterGain.gain?.setValueAtTime === 'function') {
    masterGain.gain.setValueAtTime(clampedVol, now)
  } else if (masterGain.gain) {
    masterGain.gain.value = clampedVol
  }
  masterGain.connect?.(ctx.destination)

  switch (name) {
    case 'join': {
      // Discord voice connect: ascending 3-note melody (G4 -> C5 -> E5 + C6)
      playTone(ctx, masterGain, { freq: 392.0, startTime: now, duration: 0.07, gain: 0.45 })
      playTone(ctx, masterGain, { freq: 523.25, startTime: now + 0.07, duration: 0.07, gain: 0.45 })
      playTone(ctx, masterGain, { freq: 659.25, startTime: now + 0.14, duration: 0.28, gain: 0.4 })
      playTone(ctx, masterGain, { freq: 1046.5, startTime: now + 0.14, duration: 0.28, gain: 0.15 })
      break
    }

    case 'leave': {
      // Discord voice disconnect: descending 3-note chime (E5 -> C5 -> G4)
      playTone(ctx, masterGain, { freq: 659.25, startTime: now, duration: 0.07, gain: 0.45 })
      playTone(ctx, masterGain, { freq: 523.25, startTime: now + 0.07, duration: 0.07, gain: 0.45 })
      playTone(ctx, masterGain, { freq: 392.0, startTime: now + 0.14, duration: 0.25, gain: 0.4 })
      break
    }

    case 'mute': {
      // Discord mic mute: quick snappy two-tone drop (460 -> 340 Hz)
      playTone(ctx, masterGain, { freq: 460, endFreq: 340, startTime: now, duration: 0.09, gain: 0.5 })
      break
    }

    case 'unmute': {
      // Discord mic unmute: quick snappy two-tone rise (340 -> 480 Hz)
      playTone(ctx, masterGain, { freq: 340, endFreq: 480, startTime: now, duration: 0.09, gain: 0.5 })
      break
    }

    case 'deafen': {
      // Discord deafen: muffled low-frequency drop
      const filter = ctx.createBiquadFilter?.()
      if (filter) {
        filter.type = 'lowpass'
        if (typeof filter.frequency?.setValueAtTime === 'function') {
          filter.frequency.setValueAtTime(1000, now)
        }
        filter.connect?.(masterGain)
        playTone(ctx, filter, { freq: 360, endFreq: 220, startTime: now, duration: 0.12, gain: 0.55 })
      } else {
        playTone(ctx, masterGain, { freq: 360, endFreq: 220, startTime: now, duration: 0.12, gain: 0.55 })
      }
      break
    }

    case 'undeafen': {
      // Discord undeafen: bright two-tone rise
      playTone(ctx, masterGain, { freq: 240, endFreq: 440, startTime: now, duration: 0.12, gain: 0.5 })
      break
    }

    case 'user_join': {
      // Soft gentle two-note chime for remote user joining
      playTone(ctx, masterGain, { freq: 523.25, startTime: now, duration: 0.06, gain: 0.25 })
      playTone(ctx, masterGain, { freq: 659.25, startTime: now + 0.06, duration: 0.18, gain: 0.25 })
      break
    }

    case 'user_leave': {
      // Soft gentle two-note chime for remote user leaving
      playTone(ctx, masterGain, { freq: 659.25, startTime: now, duration: 0.06, gain: 0.25 })
      playTone(ctx, masterGain, { freq: 523.25, startTime: now + 0.06, duration: 0.18, gain: 0.25 })
      break
    }

    case 'ptt_start': {
      // Subtle ascending PTT activate chirp
      playTone(ctx, masterGain, { freq: 650, endFreq: 850, startTime: now, duration: 0.04, gain: 0.25 })
      break
    }

    case 'ptt_stop': {
      // Subtle descending PTT deactivate chirp
      playTone(ctx, masterGain, { freq: 850, endFreq: 650, startTime: now, duration: 0.04, gain: 0.25 })
      break
    }

    default:
      break
  }
}

export const playSoundEffect = playSound
