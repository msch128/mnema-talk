import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from './useWebRTC'

/**
 * Applies changed input settings to what is running. Saving preferences must
 * not acquire a microphone outside a call or a mic test.
 */
export function useActiveAudioSettings() {
  const voiceStore = useVoiceStore()
  const { applyAudioSettings } = useWebRTC()
  return function applyActiveAudioSettings() {
    if (voiceStore.localAudioStream || voiceStore.isMicTesting) return applyAudioSettings()
  }
}
