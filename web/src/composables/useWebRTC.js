import { ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'

export function useWebRTC() {
  const voiceStore = useVoiceStore()
  const chatStore = useChatStore()

  const localAudioStream = ref(null)
  const localScreenStream = ref(null)
  const audioContext = ref(null)
  const analyser = ref(null)
  let speakingInterval = null

  async function joinVoiceChannel(channelId) {
    voiceStore.setChannel(channelId)
    chatStore.sendWSEvent('voice_join', { channel_id: channelId })

    try {
      // 1. Request microphone access
      localAudioStream.value = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true
        }
      })

      // 2. Setup Web Audio API volume analyzer for the green speaking ring
      setupSpeakingDetection(localAudioStream.value)
    } catch (err) {
      console.warn('Microphone access denied or unavailable:', err)
    }
  }

  function setupSpeakingDetection(stream) {
    try {
      const AudioCtx = window.AudioContext || window.webkitAudioContext
      audioContext.value = new AudioCtx()
      analyser.value = audioContext.value.createAnalyser()
      analyser.value.fftSize = 512

      const source = audioContext.value.createMediaStreamSource(stream)
      source.connect(analyser.value)

      const buffer = new Uint8Array(analyser.value.frequencyBinCount)
      let wasSpeaking = false

      speakingInterval = setInterval(() => {
        if (!analyser.value || voiceStore.isMuted) {
          if (wasSpeaking) {
            wasSpeaking = false
            chatStore.sendWSEvent('voice_speaking', { active: false })
          }
          return
        }

        analyser.value.getByteFrequencyData(buffer)
        let sum = 0
        for (let i = 0; i < buffer.length; i++) {
          sum += buffer[i]
        }
        const average = sum / buffer.length

        // Volume threshold
        const isSpeaking = average > 18
        if (isSpeaking !== wasSpeaking) {
          wasSpeaking = isSpeaking
          chatStore.sendWSEvent('voice_speaking', { active: isSpeaking })
        }
      }, 50)
    } catch (err) {
      console.warn('AudioContext speaking detector setup error:', err)
    }
  }

  // 4K 60 FPS Screen Sharing Pipeline
  async function startScreenShare() {
    try {
      localScreenStream.value = await navigator.mediaDevices.getDisplayMedia({
        video: {
          frameRate: { ideal: 60, max: 60 },
          width: { ideal: 3840, max: 3840 },
          height: { ideal: 2160, max: 2160 }
        },
        audio: true
      })

      // Hint to browser encoder to prioritize detail & smoothness
      const videoTrack = localScreenStream.value.getVideoTracks()[0]
      if (videoTrack && 'contentHint' in videoTrack) {
        videoTrack.contentHint = 'detail'
      }

      voiceStore.isScreenSharing = true

      videoTrack.onended = () => {
        stopScreenShare()
      }
    } catch (err) {
      console.warn('Screen share canceled or failed:', err)
    }
  }

  function stopScreenShare() {
    if (localScreenStream.value) {
      localScreenStream.value.getTracks().forEach(t => t.stop())
      localScreenStream.value = null
    }
    voiceStore.isScreenSharing = false
  }

  function leaveVoiceChannel() {
    if (speakingInterval) {
      clearInterval(speakingInterval)
      speakingInterval = null
    }

    if (localAudioStream.value) {
      localAudioStream.value.getTracks().forEach(t => t.stop())
      localAudioStream.value = null
    }

    if (audioContext.value) {
      audioContext.value.close().catch(() => {})
      audioContext.value = null
    }

    stopScreenShare()
    chatStore.sendWSEvent('voice_leave', {})
    voiceStore.disconnect()
  }

  return {
    localAudioStream,
    localScreenStream,
    joinVoiceChannel,
    leaveVoiceChannel,
    startScreenShare,
    stopScreenShare
  }
}
