// Synthesized Discord-like sound effects using Web Audio API.
// Pure Web Audio: zero external assets, instant playback, zero network latency.
import { useVoiceStore } from '../stores/voice'
import { applyOutputDevice } from './audioOutput'

let sharedAudioContext: AudioContext | null = null

function getAudioContext(outputDeviceId: string = '') {
  if (typeof window === 'undefined') return null
  const AudioCtx = window.AudioContext || window.webkitAudioContext
  if (!AudioCtx) return null
  try {
    if (!sharedAudioContext || sharedAudioContext.state === 'closed') {
      sharedAudioContext = new AudioCtx()
    }
    // Same speakers or headset as the voices in a call.
    applyOutputDevice(sharedAudioContext, outputDeviceId)
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
interface Tone { freq: number; endFreq?: number; startTime: number; duration: number; type?: OscillatorType; gain?: number }

function playTone(ctx: AudioContext, dest: AudioNode, { freq, endFreq, startTime, duration, type = 'sine', gain = 1 }: Tone) {
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
export type SoundEvent = 'join' | 'leave' | 'mute' | 'unmute' | 'deafen' | 'undeafen' | 'user_join' | 'user_leave' | 'ptt_start' | 'ptt_stop'

/** One tone of an effect: `at` seconds after it starts. */
type ToneSpec = Omit<Tone, 'startTime'> & { at: number }

const SOUND_TONES: Record<SoundEvent, ToneSpec[]> = {
  // Discord voice connect: ascending 3-note melody (G4 -> C5 -> E5 + C6)
  join: [
    { freq: 392.0, at: 0, duration: 0.07, gain: 0.45 },
    { freq: 523.25, at: 0.07, duration: 0.07, gain: 0.45 },
    { freq: 659.25, at: 0.14, duration: 0.28, gain: 0.4 },
    { freq: 1046.5, at: 0.14, duration: 0.28, gain: 0.15 }
  ],
  // Discord voice disconnect: descending 3-note chime (E5 -> C5 -> G4)
  leave: [
    { freq: 659.25, at: 0, duration: 0.07, gain: 0.45 },
    { freq: 523.25, at: 0.07, duration: 0.07, gain: 0.45 },
    { freq: 392.0, at: 0.14, duration: 0.25, gain: 0.4 }
  ],
  // Discord mic mute: quick snappy two-tone drop (460 -> 340 Hz)
  mute: [{ freq: 460, endFreq: 340, at: 0, duration: 0.09, gain: 0.5 }],
  // Discord mic unmute: quick snappy two-tone rise (340 -> 480 Hz)
  unmute: [{ freq: 340, endFreq: 480, at: 0, duration: 0.09, gain: 0.5 }],
  // Discord deafen: muffled low-frequency drop (through a low-pass filter, see playEffectTones)
  deafen: [{ freq: 360, endFreq: 220, at: 0, duration: 0.12, gain: 0.55 }],
  // Discord undeafen: bright two-tone rise
  undeafen: [{ freq: 240, endFreq: 440, at: 0, duration: 0.12, gain: 0.5 }],
  // Soft gentle two-note chime for remote user joining
  user_join: [
    { freq: 523.25, at: 0, duration: 0.06, gain: 0.25 },
    { freq: 659.25, at: 0.06, duration: 0.18, gain: 0.25 }
  ],
  // Soft gentle two-note chime for remote user leaving
  user_leave: [
    { freq: 659.25, at: 0, duration: 0.06, gain: 0.25 },
    { freq: 523.25, at: 0.06, duration: 0.18, gain: 0.25 }
  ],
  // Subtle ascending PTT activate chirp
  ptt_start: [{ freq: 650, endFreq: 850, at: 0, duration: 0.04, gain: 0.25 }],
  // Subtle descending PTT deactivate chirp
  ptt_stop: [{ freq: 850, endFreq: 650, at: 0, duration: 0.04, gain: 0.25 }]
}

/** The muffling low-pass the deafen sound plays through, or null if the browser has none. */
function deafenFilter(ctx: AudioContext, destination: AudioNode, now: number) {
  const filter = ctx.createBiquadFilter?.()
  if (!filter) return null
  filter.type = 'lowpass'
  if (typeof filter.frequency?.setValueAtTime === 'function') {
    filter.frequency.setValueAtTime(1000, now)
  }
  filter.connect?.(destination)
  return filter
}

function playEffectTones(ctx: AudioContext, masterGain: AudioNode, name: SoundEvent, now: number) {
  const dest = name === 'deafen' ? deafenFilter(ctx, masterGain, now) ?? masterGain : masterGain
  for (const { at, ...tone } of SOUND_TONES[name] ?? []) playTone(ctx, dest, { ...tone, startTime: now + at })
}

/** The master gain every effect goes through, at the (clamped) volume. */
function createMasterGain(ctx: AudioContext, volume: number, now: number) {
  const masterGain = ctx.createGain()
  if (!masterGain) return null
  const clampedVol = Math.min(1, Math.max(0, volume))
  if (typeof masterGain.gain?.setValueAtTime === 'function') {
    masterGain.gain.setValueAtTime(clampedVol, now)
  } else if (masterGain.gain) {
    masterGain.gain.value = clampedVol
  }
  masterGain.connect?.(ctx.destination)
  return masterGain
}

export function playSoundEffect(name: SoundEvent, overrideVolume: number | null = null, force = false) {
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

  const ctx = getAudioContext(voiceStore?.selectedOutputDeviceId)
  if (!ctx) return

  const now = ctx.currentTime || 0
  const masterGain = createMasterGain(ctx, masterVol, now)
  if (!masterGain) return
  playEffectTones(ctx, masterGain, name, now)
}
