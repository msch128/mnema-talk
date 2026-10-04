import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'

export const useChatStore = defineStore('chat', () => {
  const categories = ref([])
  const uncategorized = ref([])
  const activeChannel = ref(null)
  const messages = ref([])
  const ws = ref(null)
  const isConnected = ref(false)

  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()

  async function fetchChannels() {
    try {
      const res = await fetch('/api/channels', {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        const data = await res.json()
        categories.value = data.categories || []
        uncategorized.value = data.uncategorized || []

        // Default to first text channel if none active
        if (!activeChannel.value) {
          const firstCh = categories.value[0]?.channels?.find(c => c.type === 'text') ||
                          uncategorized.value.find(c => c.type === 'text')
          if (firstCh) selectChannel(firstCh)
        }
      }
    } catch (e) {
      console.error('Failed to fetch channels:', e)
    }
  }

  async function selectChannel(channel) {
    activeChannel.value = channel
    if (channel.type === 'text') {
      await fetchMessages(channel.id)
    }
  }

  async function fetchMessages(channelId) {
    try {
      const res = await fetch(`/api/channels/${channelId}/messages`, {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        messages.value = await res.json()
      }
    } catch (e) {
      console.error('Failed to fetch messages:', e)
    }
  }

  async function sendMessage(content) {
    if (!activeChannel.value || !content.trim()) return
    try {
      const res = await fetch(`/api/channels/${activeChannel.value.id}/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${authStore.token}`
        },
        body: JSON.stringify({ content })
      })
      if (res.ok) {
        // Message will also arrive via WebSocket for instant update
      }
    } catch (e) {
      console.error('Failed to send message:', e)
    }
  }

  async function uploadMedia(file) {
    if (!activeChannel.value || !file) return
    const formData = new FormData()
    formData.append('file', file)
    formData.append('channel_id', activeChannel.value.id)

    const res = await fetch(`/api/channels/${activeChannel.value.id}/upload`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${authStore.token}` },
      body: formData
    })

    if (!res.ok) {
      throw new Error('Upload fehlgeschlagen')
    }

    return await res.json()
  }

  function initWebSocket() {
    if (ws.value || !authStore.token) return

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/ws?token=${authStore.token}`
    const socket = new WebSocket(wsUrl)

    socket.onopen = () => {
      isConnected.value = true
    }

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        handleWSEvent(msg)
      } catch (err) {
        console.error('WS parse error:', err)
      }
    }

    socket.onclose = () => {
      isConnected.value = false
      ws.value = null
      // Auto-reconnect after 3 seconds
      setTimeout(() => {
        if (authStore.isAuthenticated) initWebSocket()
      }, 3000)
    }

    ws.value = socket
  }

  function handleWSEvent(event) {
    switch (event.type) {
      case 'message_create':
        if (activeChannel.value && event.payload.channel_id === activeChannel.value.id) {
          // Avoid duplicate messages if already present
          if (!messages.value.some(m => m.id === event.payload.id)) {
            messages.value.push(event.payload)
          }
        }
        break

      case 'voice_snapshot':
        voiceStore.setVoiceSnapshot(event.payload)
        break

      case 'voice_state_update':
        voiceStore.handleVoiceStateUpdate(event.payload)
        break

      case 'voice_speaking':
        voiceStore.handleSpeakingEvent(event.payload)
        break
    }
  }

  function sendWSEvent(type, payload) {
    if (ws.value && isConnected.value) {
      ws.value.send(JSON.stringify({ type, payload }))
    }
  }

  return {
    categories,
    uncategorized,
    activeChannel,
    messages,
    isConnected,
    fetchChannels,
    selectChannel,
    fetchMessages,
    sendMessage,
    uploadMedia,
    initWebSocket,
    sendWSEvent
  }
})
