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

        // Default to first available text channel if active is unset or removed
        const allChannels = [
          ...categories.value.flatMap(c => c.channels || []),
          ...uncategorized.value
        ]
        const stillExists = activeChannel.value && allChannels.some(c => c.id === activeChannel.value.id)
        if (!stillExists) {
          const firstText = allChannels.find(c => c.type === 'text')
          if (firstText) {
            selectChannel(firstText)
          } else if (allChannels.length > 0) {
            selectChannel(allChannels[0])
          } else {
            activeChannel.value = null
            messages.value = []
          }
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

  async function createChannel({ categoryId, name, type, topic, sortOrder = 0 }) {
    const res = await fetch('/api/admin/channels', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${authStore.token}`
      },
      body: JSON.stringify({
        category_id: categoryId || null,
        name,
        type: type || 'text',
        topic: topic || '',
        sort_order: sortOrder
      })
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Erstellen des Kanals')
    }

    const newChannel = await res.json()
    await fetchChannels()
    selectChannel(newChannel)
    return newChannel
  }

  async function deleteChannel(channelId) {
    const res = await fetch(`/api/admin/channels/${channelId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Löschen des Kanals')
    }

    if (activeChannel.value?.id === channelId) {
      activeChannel.value = null
    }
    await fetchChannels()
  }

  async function createCategory(name, sortOrder = 0) {
    const res = await fetch('/api/admin/categories', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${authStore.token}`
      },
      body: JSON.stringify({ name, sort_order: sortOrder })
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Erstellen der Kategorie')
    }

    const cat = await res.json()
    await fetchChannels()
    return cat
  }

  async function deleteCategory(categoryId) {
    const res = await fetch(`/api/admin/categories/${categoryId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Löschen der Kategorie')
    }

    await fetchChannels()
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
    createChannel,
    deleteChannel,
    createCategory,
    deleteCategory,
    initWebSocket,
    sendWSEvent
  }
})

