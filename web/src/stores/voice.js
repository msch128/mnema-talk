import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useVoiceStore = defineStore('voice', () => {
  const currentChannelId = ref(null)
  const isMuted = ref(false)
  const isDeafened = ref(false)
  const isScreenSharing = ref(false)
  const isConnected = ref(false)
  const ping = ref(14)

  // Map of channelId -> Map of userId -> User object
  const channelUsers = ref({})
  // Map of userId -> boolean (true if speaking)
  const speakingUsers = ref({})

  function setVoiceSnapshot(snapshot) {
    channelUsers.value = snapshot || {}
  }

  function handleVoiceStateUpdate(update) {
    const { action, channel_id, user, user_id } = update
    if (!channelUsers.value[channel_id]) {
      channelUsers.value[channel_id] = {}
    }

    if (action === 'join' && user) {
      channelUsers.value[channel_id][user.id] = user
    } else if (action === 'leave' && user_id) {
      delete channelUsers.value[channel_id][user_id]
      delete speakingUsers.value[user_id]
    }
  }

  function handleSpeakingEvent(event) {
    const { user_id, active } = event
    if (active) {
      speakingUsers.value[user_id] = true
    } else {
      delete speakingUsers.value[user_id]
    }
  }

  function toggleMute() {
    isMuted.value = !isMuted.value
  }

  function toggleDeafen() {
    isDeafened.value = !isDeafened.value
    if (isDeafened.value) isMuted.value = true
  }

  function setChannel(channelId) {
    currentChannelId.value = channelId
    isConnected.value = !!channelId
  }

  function disconnect() {
    currentChannelId.value = null
    isConnected.value = false
    isScreenSharing.value = false
  }

  return {
    currentChannelId,
    isMuted,
    isDeafened,
    isScreenSharing,
    isConnected,
    ping,
    channelUsers,
    speakingUsers,
    setVoiceSnapshot,
    handleVoiceStateUpdate,
    handleSpeakingEvent,
    toggleMute,
    toggleDeafen,
    setChannel,
    disconnect
  }
})
