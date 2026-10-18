// The voice settings a member keeps in this browser: how the microphone opens,
// noise handling, volumes, devices and sound effects. Built inside the voice store.
import { computed, ref, watch } from 'vue'
import { readJson, writeJson } from '../lib/voicePlayback'

export type InputMode = 'activity' | 'ptt'
export type NoiseMode = 'ai' | 'ai-lite' | 'browser' | 'off'
export const NOISE_MODES: readonly NoiseMode[] = ['ai', 'ai-lite', 'browser', 'off']

export const SOUND_EVENTS = [
  'join',
  'leave',
  'user_join',
  'user_leave',
  'mute',
  'unmute',
  'deafen',
  'undeafen',
  'ptt_start',
  'ptt_stop'
] as const
export type SoundEvent = typeof SOUND_EVENTS[number]

function defaultSoundEvents() {
  return {
    join: true,
    leave: true,
    user_join: true,
    user_leave: true,
    mute: true,
    unmute: true,
    deafen: true,
    undeafen: true,
    ptt_start: true,
    ptt_stop: true
  }
}

// 'ai' (DeepFilterNet3), 'ai-lite' (GTCRN, see lib/noiseSuppressor),
// 'browser' (getUserMedia noiseSuppression) or 'off'. Older clients stored
// only 'true'/'false'.
function savedNoiseMode(): NoiseMode {
  const stored = localStorage.getItem('mnema_noise')
  if (stored === 'ai' || stored === 'ai-lite' || stored === 'browser' || stored === 'off') return stored
  return stored === 'false' ? 'off' : 'ai'
}

export function useVoiceSettings() {
  // Voice activity and sensitivity (noise gate)
  const inputMode = ref<InputMode>(localStorage.getItem('mnema_input_mode') === 'ptt' ? 'ptt' : 'activity')
  const pttKey = ref(localStorage.getItem('mnema_ptt_key') || 'Space')
  const autoSensitivity = ref(localStorage.getItem('mnema_auto_sens') === 'true')
  const sensitivityThreshold = ref(parseInt(localStorage.getItem('mnema_sens_threshold') || '30', 10)) // 0 to 100
  const hangoverMs = ref(parseInt(localStorage.getItem('mnema_hangover_ms') || '250', 10))

  // Advanced audio and network settings
  const warnNoAudioDetected = ref(localStorage.getItem('mnema_warn_no_audio') !== 'false')
  const warnSwitchChannel = ref(localStorage.getItem('mnema_warn_switch_channel') !== 'false')
  const qosHighPriority = ref(localStorage.getItem('mnema_qos_priority') !== 'false')
  const soundEffectsEnabled = ref(localStorage.getItem('mnema_sounds_enabled') !== 'false')
  const soundEffectsVolume = ref(parseInt(localStorage.getItem('mnema_sounds_volume') || '80', 10))
  const soundEvents = ref({
    ...defaultSoundEvents(),
    ...readJson('mnema_sound_events')
  })

  // Audio processing
  const noiseMode = ref<NoiseMode>(savedNoiseMode())
  const noiseCancelling = computed(() => noiseMode.value !== 'off')
  const autoGainControl = ref(localStorage.getItem('mnema_agc') === 'true') // Default false to avoid boosting background voices
  const echoCancellation = ref(localStorage.getItem('mnema_echo') !== 'false')
  const inputVolume = ref(parseInt(localStorage.getItem('mnema_input_volume') || '100', 10))
  const outputVolume = ref(parseInt(localStorage.getItem('mnema_output_volume') || '100', 10))

  const selectedInputDeviceId = ref(localStorage.getItem('mnema_input_dev') || '')
  const selectedOutputDeviceId = ref(localStorage.getItem('mnema_output_dev') || '')

  function saveSettings() {
    localStorage.setItem('mnema_input_mode', inputMode.value)
    localStorage.setItem('mnema_ptt_key', pttKey.value)
    localStorage.setItem('mnema_auto_sens', String(autoSensitivity.value))
    localStorage.setItem('mnema_sens_threshold', String(sensitivityThreshold.value))
    localStorage.setItem('mnema_hangover_ms', String(hangoverMs.value))
    localStorage.setItem('mnema_warn_no_audio', String(warnNoAudioDetected.value))
    localStorage.setItem('mnema_warn_switch_channel', String(warnSwitchChannel.value))
    localStorage.setItem('mnema_qos_priority', String(qosHighPriority.value))
    localStorage.setItem('mnema_sounds_enabled', String(soundEffectsEnabled.value))
    localStorage.setItem('mnema_sounds_volume', String(soundEffectsVolume.value))
    writeJson('mnema_sound_events', soundEvents.value)
    localStorage.setItem('mnema_noise', noiseMode.value)
    localStorage.setItem('mnema_agc', String(autoGainControl.value))
    localStorage.setItem('mnema_echo', String(echoCancellation.value))
    localStorage.setItem('mnema_input_volume', String(inputVolume.value))
    localStorage.setItem('mnema_output_volume', String(outputVolume.value))
    localStorage.setItem('mnema_input_dev', selectedInputDeviceId.value)
    localStorage.setItem('mnema_output_dev', selectedOutputDeviceId.value)
  }

  function toggleSoundEvent(eventName: SoundEvent) {
    soundEvents.value = {
      ...soundEvents.value,
      [eventName]: soundEvents.value[eventName] === false
    }
    saveSettings()
  }

  // Volumes and the output device are remembered as soon as they change.
  watch([inputVolume, outputVolume, selectedOutputDeviceId], () => saveSettings())

  return {
    inputMode, pttKey, autoSensitivity, sensitivityThreshold, hangoverMs,
    warnNoAudioDetected, warnSwitchChannel, qosHighPriority,
    soundEffectsEnabled, soundEffectsVolume, soundEvents,
    noiseMode, noiseCancelling, autoGainControl, echoCancellation, inputVolume, outputVolume,
    selectedInputDeviceId, selectedOutputDeviceId,
    saveSettings, toggleSoundEvent
  }
}
