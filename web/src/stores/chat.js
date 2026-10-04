import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'

export const useChatStore = defineStore('chat', () => {
  const categories = ref([])
  const uncategorized = ref([])
  const activeChannel = ref(null)
  const messages = ref([])
  const activeThread = ref(null)
  const threadReplies = ref([])
  const isThreadLoading = ref(false)
  const ws = ref(null)
  const isConnected = ref(false)

  // Community Members & Real-time Presence
  const members = ref([])
  const onlineUserIds = ref(new Set())
  const showMemberList = ref(true)
  const selectedUserProfile = ref(null)
  const pendingMention = ref('')

  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()

  let pingTimer = null
  let webrtcOfferHandler = null
  let webrtcCandidateHandler = null

  function setWebRTCHandlers({ onOffer, onCandidate }) {
    webrtcOfferHandler = onOffer
    webrtcCandidateHandler = onCandidate
  }

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

  async function fetchMembers() {
    try {
      const res = await fetch('/api/members', {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        members.value = await res.json()
      }
    } catch (e) {
      console.error('Failed to fetch members:', e)
    }
  }

  const onlineMembers = computed(() => {
    return members.value.filter(m => onlineUserIds.value.has(m.id))
  })

  const offlineMembers = computed(() => {
    return members.value.filter(m => !onlineUserIds.value.has(m.id))
  })

  async function selectChannel(channel) {
    if (!channel) return
    activeChannel.value = channel
    activeThread.value = null
    threadReplies.value = []
    await fetchMessages(channel.id)
  }

  async function fetchMessages(channelId) {
    try {
      const res = await fetch(`/api/channels/${channelId}/messages`, {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        const data = await res.json()
        messages.value = Array.isArray(data) ? data : []
      } else {
        messages.value = []
      }
    } catch (e) {
      console.error('Failed to fetch messages:', e)
      messages.value = []
    }
  }

  async function sendMessage(content, parentId = null) {
    if (!activeChannel.value || !content.trim()) return null
    try {
      const payload = { content: content.trim() }
      if (parentId) {
        payload.parent_id = parentId
      }

      const res = await fetch(`/api/channels/${activeChannel.value.id}/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${authStore.token}`
        },
        body: JSON.stringify(payload)
      })

      if (!res.ok) {
        const err = await res.json().catch(() => ({}))
        throw new Error(err.error || 'Nachricht konnte nicht gesendet werden')
      }

      const newMsg = await res.json()

      // Optimistic / direct insertion to ensure instant UI responsiveness
      if (parentId) {
        if (!threadReplies.value.some(m => m.id === newMsg.id)) {
          threadReplies.value.push(newMsg)
        }
        const root = messages.value.find(m => m.id === parentId)
        if (root) root.reply_count = (root.reply_count || 0) + 1
      } else {
        if (!messages.value.some(m => m.id === newMsg.id)) {
          messages.value.push(newMsg)
        }
      }

      return newMsg
    } catch (e) {
      console.error('Failed to send message:', e)
      throw e
    }
  }

  async function uploadMedia(file, content = '', parentId = null) {
    if (!activeChannel.value || !file) return null
    const formData = new FormData()
    formData.append('file', file)
    formData.append('channel_id', activeChannel.value.id)
    if (content) formData.append('content', content)
    if (parentId) formData.append('parent_id', parentId)

    const res = await fetch(`/api/channels/${activeChannel.value.id}/upload`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${authStore.token}` },
      body: formData
    })

    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Upload fehlgeschlagen')
    }

    const newMsg = await res.json()
    if (newMsg && newMsg.id) {
      if (parentId) {
        if (!threadReplies.value.some(m => m.id === newMsg.id)) {
          threadReplies.value.push(newMsg)
        }
        const root = messages.value.find(m => m.id === parentId)
        if (root) root.reply_count = (root.reply_count || 0) + 1
      } else {
        if (!messages.value.some(m => m.id === newMsg.id)) {
          messages.value.push(newMsg)
        }
      }
    }

    return newMsg
  }

  async function openThread(msg) {
    if (!msg) return
    activeThread.value = msg
    threadReplies.value = []
    isThreadLoading.value = true

    try {
      const res = await fetch(`/api/messages/${msg.id}/thread`, {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        const data = await res.json()
        activeThread.value = data.root || msg
        threadReplies.value = Array.isArray(data.replies) ? data.replies : []
      }
    } catch (e) {
      console.error('Failed to load thread:', e)
    } finally {
      isThreadLoading.value = false
    }
  }

  function closeThread() {
    activeThread.value = null
    threadReplies.value = []
  }

  async function sendThreadReply(content) {
    if (!activeThread.value) return null
    return await sendMessage(content, activeThread.value.id)
  }

  async function uploadThreadMedia(file, content = '') {
    if (!activeThread.value) return null
    return await uploadMedia(file, content, activeThread.value.id)
  }

  function startPingHeartbeat() {
    stopPingHeartbeat()
    pingTimer = setInterval(() => {
      if (isConnected.value && ws.value) {
        sendWSEvent('ping', { t: Date.now() })
      }
    }, 2000)
  }

  function stopPingHeartbeat() {
    if (pingTimer) {
      clearInterval(pingTimer)
      pingTimer = null
    }
  }

  function initWebSocket() {
    if (ws.value || !authStore.token) return

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/api/ws?token=${authStore.token}`
    const socket = new WebSocket(wsUrl)

    socket.onopen = () => {
      isConnected.value = true
      startPingHeartbeat()
      // Send immediate first ping
      sendWSEvent('ping', { t: Date.now() })
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
      stopPingHeartbeat()
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
      case 'pong':
        if (event.payload?.t) {
          const rtt = Date.now() - event.payload.t
          voiceStore.recordPing(rtt)
        }
        break

      case 'presence_snapshot':
        onlineUserIds.value = new Set(event.payload || [])
        break

      case 'presence_update': {
        const { user_id, status } = event.payload || {}
        if (status === 'online') {
          onlineUserIds.value.add(user_id)
        } else if (status === 'offline') {
          onlineUserIds.value.delete(user_id)
        }
        onlineUserIds.value = new Set(onlineUserIds.value)
        break
      }

      case 'message_create': {
        const msg = event.payload
        if (!msg) break

        if (msg.parent_id) {
          // Thread reply received
          const root = messages.value.find(m => m.id === msg.parent_id)
          if (root) {
            root.reply_count = (root.reply_count || 0) + 1
          }
          if (activeThread.value && activeThread.value.id === msg.parent_id) {
            if (!threadReplies.value.some(m => m.id === msg.id)) {
              threadReplies.value.push(msg)
            }
          }
        } else {
          // Root channel message
          if (activeChannel.value && msg.channel_id === activeChannel.value.id) {
            if (!messages.value.some(m => m.id === msg.id)) {
              messages.value.push(msg)
            }
          }
        }
        break
      }

      case 'user_update': {
        const updated = event.payload
        if (!updated) break
        // Update members list
        const idx = members.value.findIndex(m => m.id === updated.id)
        if (idx !== -1) {
          members.value[idx] = { ...members.value[idx], ...updated }
        }
        // Update messages in current channel
        messages.value.forEach(m => {
          if (m.user_id === updated.id) {
            if (updated.avatar_url !== undefined) m.avatar_url = updated.avatar_url
            if (updated.display_name !== undefined) m.display_name = updated.display_name
          }
        })
        // Update thread replies
        threadReplies.value.forEach(m => {
          if (m.user_id === updated.id) {
            if (updated.avatar_url !== undefined) m.avatar_url = updated.avatar_url
            if (updated.display_name !== undefined) m.display_name = updated.display_name
          }
        })
        // If current auth user updated, update authStore.user as well
        if (authStore.user?.id === updated.id) {
          authStore.user = { ...authStore.user, ...updated }
        }
        // If selectedUserProfile is this user, update it
        if (selectedUserProfile.value?.id === updated.id) {
          selectedUserProfile.value = { ...selectedUserProfile.value, ...updated }
        }
        break
      }

      case 'message_update': {
        const updated = event.payload
        if (!updated) break
        const mIdx = messages.value.findIndex(m => m.id === updated.id)
        if (mIdx !== -1) {
          messages.value[mIdx] = { ...messages.value[mIdx], ...updated }
        }
        const tIdx = threadReplies.value.findIndex(m => m.id === updated.id)
        if (tIdx !== -1) {
          threadReplies.value[tIdx] = { ...threadReplies.value[tIdx], ...updated }
        }
        if (activeThread.value && activeThread.value.id === updated.id) {
          activeThread.value = { ...activeThread.value, ...updated }
        }
        break
      }

      case 'message_delete': {
        const { id } = event.payload || {}
        if (!id) break
        messages.value = messages.value.filter(m => m.id !== id)
        threadReplies.value = threadReplies.value.filter(m => m.id !== id)
        if (activeThread.value && activeThread.value.id === id) {
          closeThread()
        }
        break
      }

      case 'message_reaction': {
        const { message_id, reactions } = event.payload || {}
        if (!message_id) break
        const m = messages.value.find(item => item.id === message_id)
        if (m) m.reactions = reactions || []
        const t = threadReplies.value.find(item => item.id === message_id)
        if (t) t.reactions = reactions || []
        if (activeThread.value && activeThread.value.id === message_id) {
          activeThread.value.reactions = reactions || []
        }
        break
      }

      case 'webrtc_offer':
        if (webrtcOfferHandler) {
          webrtcOfferHandler(event.payload)
        }
        break

      case 'webrtc_candidate':
        if (webrtcCandidateHandler) {
          webrtcCandidateHandler(event.payload)
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

  async function openUserProfile(userOrMessage) {
    if (!userOrMessage) return
    const userId = userOrMessage.user_id || userOrMessage.id
    if (!userId) return

    const existingMember = members.value.find(m => m.id === userId)

    const base = {
      id: userId,
      username: userOrMessage.username || existingMember?.username || '',
      display_name: userOrMessage.display_name || existingMember?.display_name || userOrMessage.username || '',
      avatar_url: userOrMessage.avatar_url || existingMember?.avatar_url || '',
      role: userOrMessage.role || existingMember?.role || 'member',
      created_at: userOrMessage.created_at || existingMember?.created_at || null
    }
    selectedUserProfile.value = base

    try {
      const res = await fetch(`/api/users/${userId}`, {
        headers: { 'Authorization': `Bearer ${authStore.token}` }
      })
      if (res.ok) {
        const full = await res.json()
        selectedUserProfile.value = { ...selectedUserProfile.value, ...full }
      }
    } catch (e) {
      console.warn('Failed to fetch full user profile:', e)
    }
  }

  function closeUserProfile() {
    selectedUserProfile.value = null
  }

  function insertMention(username) {
    if (!username) return
    pendingMention.value = username
  }

  async function editMessage(channelId, messageId, content) {
    if (!content.trim()) return null
    const res = await fetch(`/api/channels/${channelId}/messages/${messageId}`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${authStore.token}`
      },
      body: JSON.stringify({ content: content.trim() })
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Bearbeiten der Nachricht')
    }
    const updated = await res.json()
    const mIdx = messages.value.findIndex(m => m.id === messageId)
    if (mIdx !== -1) messages.value[mIdx] = { ...messages.value[mIdx], ...updated }
    const tIdx = threadReplies.value.findIndex(m => m.id === messageId)
    if (tIdx !== -1) threadReplies.value[tIdx] = { ...threadReplies.value[tIdx], ...updated }
    if (activeThread.value && activeThread.value.id === messageId) {
      activeThread.value = { ...activeThread.value, ...updated }
    }
    return updated
  }

  async function deleteMessage(channelId, messageId) {
    const res = await fetch(`/api/channels/${channelId}/messages/${messageId}`, {
      method: 'DELETE',
      headers: { 'Authorization': `Bearer ${authStore.token}` }
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Löschen der Nachricht')
    }
    messages.value = messages.value.filter(m => m.id !== messageId)
    threadReplies.value = threadReplies.value.filter(m => m.id !== messageId)
    if (activeThread.value && activeThread.value.id === messageId) {
      closeThread()
    }
  }

  async function toggleReaction(messageId, emoji) {
    const res = await fetch(`/api/messages/${messageId}/reactions`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${authStore.token}`
      },
      body: JSON.stringify({ emoji })
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || 'Fehler beim Reagieren')
    }
    const data = await res.json()
    const m = messages.value.find(item => item.id === messageId)
    if (m) m.reactions = data.reactions || []
    const t = threadReplies.value.find(item => item.id === messageId)
    if (t) t.reactions = data.reactions || []
    if (activeThread.value && activeThread.value.id === messageId) {
      activeThread.value.reactions = data.reactions || []
    }
    return data.reactions
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
    members,
    onlineUserIds,
    onlineMembers,
    offlineMembers,
    showMemberList,
    selectedUserProfile,
    openUserProfile,
    closeUserProfile,
    pendingMention,
    insertMention,
    fetchChannels,
    fetchMembers,
    selectChannel,
    fetchMessages,
    sendMessage,
    editMessage,
    deleteMessage,
    toggleReaction,
    uploadMedia,
    createChannel,
    deleteChannel,
    createCategory,
    deleteCategory,
    activeThread,
    threadReplies,
    isThreadLoading,
    openThread,
    closeThread,
    sendThreadReply,
    uploadThreadMedia,
    initWebSocket,
    sendWSEvent,
    setWebRTCHandlers
  }
})
