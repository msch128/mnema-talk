// The voice settings a member keeps in this browser: how the microphone opens,
// noise handling, volumes, devices and sound effects. Built inside the voice store.
import { computed, ref, watch } from 'vue'
import { readItem, readJson, writeItem, writeJson } from '../lib/voicePlayback'
import { normalizeMessageSoundMode, type MessageSoundMode } from '../lib/messageSound'

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
  'ptt_stop',
  'call_ring'
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
    ptt_stop: true,
    call_ring: true
  }
}

const VALID_NOISE_MODES = new Set<string>(['ai', 'ai-lite', 'browser', 'off'])

function isNoiseMode(value: string | null): value is NoiseMode {
  return value !== null && VALID_NOISE_MODES.has(value)
}

// 'ai' (DeepFilterNet3), 'ai-lite' (GTCRN, see lib/noiseSuppressor),
// 'browser' (getUserMedia noiseSuppression) or 'off'. Older clients stored
// only 'true'/'false'.
function savedNoiseMode(): NoiseMode {
  const stored = readItem('mnema_noise')
  if (isNoiseMode(stored)) return stored
  return stored === 'false' ? 'off' : 'ai'
}

function loadVoiceInputSettings() {
  const inputMode = ref<InputMode>(readItem('mnema_input_mode') === 'ptt' ? 'ptt' : 'activity')
  // No key until the member picks one (a default would collide with typing). A
  // push-to-talk setup from before that never stored a key keeps the old
  // Space; a stored empty key stays empty.
  const pttKey = ref(readItem('mnema_ptt_key') ?? (inputMode.value === 'ptt' ? 'Space' : ''))
  const autoSensitivity = ref(readItem('mnema_auto_sens') === 'true')
  const sensitivityThreshold = ref(parseInt(readItem('mnema_sens_threshold') || '30', 10))
  const hangoverMs = ref(parseInt(readItem('mnema_hangover_ms') || '250', 10))
  return { inputMode, pttKey, autoSensitivity, sensitivityThreshold, hangoverMs }
}

function loadFeedbackSettings() {
  const warnNoAudioDetected = ref(readItem('mnema_warn_no_audio') !== 'false')
  const warnSwitchChannel = ref(readItem('mnema_warn_switch_channel') !== 'false')
  const qosHighPriority = ref(readItem('mnema_qos_priority') !== 'false')
  const soundEffectsEnabled = ref(readItem('mnema_sounds_enabled') !== 'false')
  const soundEffectsVolume = ref(parseInt(readItem('mnema_sounds_volume') || '80', 10))
  const soundEvents = ref({
    ...defaultSoundEvents(),
    ...readJson('mnema_sound_events')
  })
  const messageSounds = ref<MessageSoundMode>(normalizeMessageSoundMode(readItem('mnema_message_sounds')))
  return {
    warnNoAudioDetected, warnSwitchChannel, qosHighPriority,
    soundEffectsEnabled, soundEffectsVolume, soundEvents, messageSounds
  }
}

function loadAudioProcessingSettings() {
  const noiseMode = ref<NoiseMode>(savedNoiseMode())
  const noiseCancelling = computed(() => noiseMode.value !== 'off')
  const autoGainControl = ref(readItem('mnema_agc') === 'true')
  const echoCancellation = ref(readItem('mnema_echo') !== 'false')
  // Opus DTX: send almost nothing while silent. Off for music and steady sounds, which the encoder may take for silence.
  const silenceSaving = ref(readItem('mnema_dtx') !== 'false')
  const inputVolume = ref(parseInt(readItem('mnema_input_volume') || '100', 10))
  const outputVolume = ref(parseInt(readItem('mnema_output_volume') || '100', 10))
  const selectedInputDeviceId = ref(readItem('mnema_input_dev') || '')
  const selectedOutputDeviceId = ref(readItem('mnema_output_dev') || '')
  const selectedCameraDeviceId = ref(readItem('mnema_camera_dev') || '')
  return {
    noiseMode, noiseCancelling, autoGainControl, echoCancellation, silenceSaving,
    inputVolume, outputVolume, selectedInputDeviceId, selectedOutputDeviceId, selectedCameraDeviceId
  }
}

function persistVoiceSettings(
  input: ReturnType<typeof loadVoiceInputSettings>,
  feedback: ReturnType<typeof loadFeedbackSettings>,
  audio: ReturnType<typeof loadAudioProcessingSettings>
) {
  writeItem('mnema_input_mode', input.inputMode.value)
  writeItem('mnema_ptt_key', input.pttKey.value)
  writeItem('mnema_auto_sens', String(input.autoSensitivity.value))
  writeItem('mnema_sens_threshold', String(input.sensitivityThreshold.value))
  writeItem('mnema_hangover_ms', String(input.hangoverMs.value))
  writeItem('mnema_warn_no_audio', String(feedback.warnNoAudioDetected.value))
  writeItem('mnema_warn_switch_channel', String(feedback.warnSwitchChannel.value))
  writeItem('mnema_qos_priority', String(feedback.qosHighPriority.value))
  writeItem('mnema_sounds_enabled', String(feedback.soundEffectsEnabled.value))
  writeItem('mnema_sounds_volume', String(feedback.soundEffectsVolume.value))
  writeJson('mnema_sound_events', feedback.soundEvents.value)
  writeItem('mnema_message_sounds', feedback.messageSounds.value)
  writeItem('mnema_noise', audio.noiseMode.value)
  writeItem('mnema_agc', String(audio.autoGainControl.value))
  writeItem('mnema_echo', String(audio.echoCancellation.value))
  writeItem('mnema_dtx', String(audio.silenceSaving.value))
  writeItem('mnema_input_volume', String(audio.inputVolume.value))
  writeItem('mnema_output_volume', String(audio.outputVolume.value))
  writeItem('mnema_input_dev', audio.selectedInputDeviceId.value)
  writeItem('mnema_output_dev', audio.selectedOutputDeviceId.value)
  writeItem('mnema_camera_dev', audio.selectedCameraDeviceId.value)
}

export function useVoiceSettings() {
  const input = loadVoiceInputSettings()
  const feedback = loadFeedbackSettings()
  const audio = loadAudioProcessingSettings()

  function saveSettings() {
    persistVoiceSettings(input, feedback, audio)
  }

  function toggleSoundEvent(eventName: SoundEvent) {
    feedback.soundEvents.value = {
      ...feedback.soundEvents.value,
      [eventName]: feedback.soundEvents.value[eventName] === false
    }
    saveSettings()
  }

  // Volumes and the output device are remembered as soon as they change.
  watch([audio.inputVolume, audio.outputVolume, audio.selectedOutputDeviceId], () => saveSettings())

  return {
    ...input,
    ...feedback,
    ...audio,
    saveSettings,
    toggleSoundEvent
  }
}
