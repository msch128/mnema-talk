import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import {
  PAGE_SIZE, WINDOW_CAP, emptyWindow, fromLatest, fromAround,
  prependOlder, appendNewer, appendLive, removeMessage
} from '../lib/messageWindow'
import { markPreviewEdited, markPreviewDeleted } from '../lib/replies'

const MAX_PENDING_LIVE = 200

export const useChatStore = defineStore('chat', () => {
  const categories = ref([])
  const uncategorized = ref([])
  const activeChannel = ref(null)
  // Windowed history of the active channel (see lib/messageWindow.js).
  const messages = ref([])
  const hasMoreBefore = ref(false)
  const hasMoreAfter = ref(false)
  const isLoadingBefore = ref(false)
  const isLoadingAfter = ref(false)
  const isLoadingWindow = ref(false)
  // Live messages that could not join the window (it doesn't reach the newest end).
  const missedLiveCount = ref(0)
  // Change signals for the view: what caused the last window update.
  const liveAppendSeq = ref(0)
  const latestLoadSeq = ref(0)
  const jumpTarget = ref(null)
  const toast = ref(null)
  const activeThread = ref(null)
  const threadReplies = ref([])
  const isThreadLoading = ref(false)
  const ws = ref(null)
  const isConnected = ref(false)

  // Community members & real-time presence
  const members = ref([])
  const onlineUserIds = ref(new Set())
  const showMemberList = ref(true)
  const selectedUserProfile = ref(null)
  const pendingMention = ref('')

  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()

  let pingTimer = null
  let reconnectTimer = null
  let reconnectDelay = 1000
  let webrtcOfferHandler = null
  let webrtcCandidateHandler = null

  function setWebRTCHandlers({ onOffer, onCandidate }) {
    webrtcOfferHandler = onOffer
    webrtcCandidateHandler = onCandidate
  }

  const allChannels = computed(() => [
    ...categories.value.flatMap(c => c.channels || []),
    ...uncategorized.value
  ])

  async function fetchChannels() {
    try {
      const data = await api('/api/channels')
      categories.value = data.categories || []
      uncategorized.value = data.uncategorized || []

      const stillExists = activeChannel.value && allChannels.value.some(c => c.id === activeChannel.value.id)
      if (!stillExists) {
        const next = allChannels.value.find(c => c.type === 'text') || allChannels.value[0]
        if (next) {
          selectChannel(next)
        } else {
          activeChannel.value = null
          resetWindow()
          setWindow(emptyWindow())
        }
      }
    } catch (e) {
      console.error('Failed to fetch channels:', e)
    }
  }

  async function fetchMembers() {
    try {
      members.value = await api('/api/members')
    } catch (e) {
      console.error('Failed to fetch members:', e)
    }
  }

  const onlineMembers = computed(() => members.value.filter(m => onlineUserIds.value.has(m.id)))
  const offlineMembers = computed(() => members.value.filter(m => !onlineUserIds.value.has(m.id)))

  async function selectChannel(channel) {
    if (!channel) return
    activeChannel.value = channel
    activeThread.value = null
    threadReplies.value = []
    pendingLive = []
    resetWindow()
    setWindow(emptyWindow())
    await fetchMessages(channel.id)
  }

  // ---- Message window (infinite scroll in both directions) ----

  // Bumped whenever the window is replaced; responses from an older
  // generation (other channel, earlier jump) are ignored.
  let windowGen = 0
  let pendingLive = []
  let jumpSeq = 0
  let toastTimer = null

  function getWindow() {
    return { messages: messages.value, hasMoreBefore: hasMoreBefore.value, hasMoreAfter: hasMoreAfter.value }
  }

  function setWindow(win) {
    messages.value = win.messages
    hasMoreBefore.value = win.hasMoreBefore
    hasMoreAfter.value = win.hasMoreAfter
    if (!win.hasMoreAfter) flushPendingLive()
  }

  // Live messages buffered while the window was not at the newest end join
  // once it is (they are newer than anything a page could have returned).
  function flushPendingLive() {
    missedLiveCount.value = 0
    if (!pendingLive.length) return
    const queued = pendingLive
    pendingLive = []
    let win = getWindow()
    for (const msg of queued) {
      if (msg.channel_id !== activeChannel.value?.id) continue
      win = appendLive(win, msg, { cap: WINDOW_CAP }).window
    }
    messages.value = win.messages
    hasMoreBefore.value = win.hasMoreBefore
  }

  function resetWindow() {
    windowGen++
    isLoadingBefore.value = false
    isLoadingAfter.value = false
    isLoadingWindow.value = false
    missedLiveCount.value = 0
  }

  function messagesUrl(channelId, params = {}) {
    const q = new URLSearchParams({ limit: String(PAGE_SIZE), ...params })
    return `/api/channels/${channelId}/messages?${q}`
  }

  function showToast(text) {
    const id = Date.now() + Math.random()
    toast.value = { id, text }
    clearTimeout(toastTimer)
    toastTimer = setTimeout(() => {
      if (toast.value?.id === id) toast.value = null
    }, 3000)
  }

  /** Loads the newest page of a channel and replaces the window. */
  async function fetchMessages(channelId) {
    resetWindow()
    const gen = windowGen
    isLoadingWindow.value = true
    try {
      const data = await api(messagesUrl(channelId))
      // Ignore a late response for a channel the user already left.
      if (gen !== windowGen || activeChannel.value?.id !== channelId) return false
      setWindow(fromLatest(data, PAGE_SIZE))
      latestLoadSeq.value++
      return true
    } catch (e) {
      console.error('Failed to fetch messages:', e)
      if (gen === windowGen && activeChannel.value?.id === channelId) setWindow(emptyWindow())
      return false
    } finally {
      if (gen === windowGen) isLoadingWindow.value = false
    }
  }

  /** "Jump to present": reload the newest page. */
  async function jumpToLatest() {
    if (!activeChannel.value) return false
    return fetchMessages(activeChannel.value.id)
  }

  async function loadOlder() {
    const channelId = activeChannel.value?.id
    if (!channelId || isLoadingWindow.value || isLoadingBefore.value || !hasMoreBefore.value) return false
    const anchorId = messages.value[0]?.id
    if (!anchorId) return false
    const gen = windowGen
    isLoadingBefore.value = true
    try {
      const data = await api(messagesUrl(channelId, { before: anchorId }))
      if (gen !== windowGen) return false
      setWindow(prependOlder(getWindow(), data, { anchorId, limit: PAGE_SIZE, cap: WINDOW_CAP }))
      return true
    } catch (e) {
      console.error('Failed to load older messages:', e)
      return false
    } finally {
      if (gen === windowGen) isLoadingBefore.value = false
    }
  }

  async function loadNewer() {
    const channelId = activeChannel.value?.id
    if (!channelId || isLoadingWindow.value || isLoadingAfter.value || !hasMoreAfter.value) return false
    const anchorId = messages.value[messages.value.length - 1]?.id
    if (!anchorId) return false
    const gen = windowGen
    isLoadingAfter.value = true
    try {
      const data = await api(messagesUrl(channelId, { after: anchorId }))
      if (gen !== windowGen) return false
      setWindow(appendNewer(getWindow(), data, { anchorId, limit: PAGE_SIZE, cap: WINDOW_CAP }))
      return true
    } catch (e) {
      console.error('Failed to load newer messages:', e)
      return false
    } finally {
      if (gen === windowGen) isLoadingAfter.value = false
    }
  }

  /**
   * Scrolls the view to a root message, loading the history around it when it
   * is outside the window. Resolves to false (and shows a toast) if it's gone.
   */
  async function jumpToMessage(id) {
    const channelId = activeChannel.value?.id
    if (!id || !channelId) return false
    if (messages.value.some(m => m.id === id)) {
      jumpTarget.value = { id, seq: ++jumpSeq }
      return true
    }
    resetWindow()
    const gen = windowGen
    isLoadingWindow.value = true
    try {
      const data = await api(messagesUrl(channelId, { around: id }))
      if (gen !== windowGen || activeChannel.value?.id !== channelId) return false
      setWindow(fromAround(data, id, PAGE_SIZE))
      // Same tick as the window swap so the view handles both in one render.
      jumpTarget.value = { id, seq: ++jumpSeq }
      return true
    } catch (e) {
      if (gen === windowGen) {
        showToast(e?.status === 404 ? 'Nachricht nicht gefunden' : 'Nachricht konnte nicht geladen werden')
      }
      return false
    } finally {
      if (gen === windowGen) isLoadingWindow.value = false
    }
  }

  function insertMessage(msg) {
    if (msg.parent_id) {
      if (activeThread.value?.id === msg.parent_id && !threadReplies.value.some(m => m.id === msg.id)) {
        threadReplies.value.push(msg)
      }
      return
    }
    if (activeChannel.value?.id !== msg.channel_id) return
    if (isLoadingWindow.value || hasMoreAfter.value) {
      // The window doesn't reach the newest end; appending would leave a gap.
      if (!messages.value.some(m => m.id === msg.id) && !pendingLive.some(m => m.id === msg.id)) {
        pendingLive.push(msg)
        if (pendingLive.length > MAX_PENDING_LIVE) pendingLive.shift()
        if (!isLoadingWindow.value) missedLiveCount.value++
      }
      return
    }
    const { window: win, status } = appendLive(getWindow(), msg, { cap: WINDOW_CAP })
    if (status !== 'appended') return
    messages.value = win.messages
    hasMoreBefore.value = win.hasMoreBefore
    liveAppendSeq.value++
  }

  function removeFromWindow(id) {
    const win = removeMessage(getWindow(), id)
    if (win.messages !== messages.value) messages.value = win.messages
    pendingLive = pendingLive.filter(m => m.id !== id)
  }

  // A reply reaches us twice (HTTP response and WebSocket echo); count it once.
  const countedReplies = new Set()
  function countReply(msg) {
    if (!msg?.parent_id || countedReplies.has(msg.id)) return
    countedReplies.add(msg.id)
    bumpReplyCount(msg.parent_id, 1)
  }

  function bumpReplyCount(parentId, delta) {
    const root = messages.value.find(m => m.id === parentId)
    if (root) root.reply_count = Math.max(0, (root.reply_count || 0) + delta)
    if (activeThread.value?.id === parentId) {
      activeThread.value.reply_count = Math.max(0, (activeThread.value.reply_count || 0) + delta)
    }
  }

  async function sendMessage(content, parentId = null, replyToId = null) {
    if (!activeChannel.value || !content.trim()) return null
    const json = { content: content.trim() }
    if (parentId) json.parent_id = parentId
    if (replyToId) json.reply_to_id = replyToId
    const msg = await api(`/api/channels/${activeChannel.value.id}/messages`, { method: 'POST', json })
    // The WebSocket echo is de-duplicated by insertMessage / countReply.
    countReply(msg)
    insertMessage(msg)
    return msg
  }

  async function uploadMedia(file, content = '', parentId = null, replyToId = null) {
    if (!activeChannel.value || !file) return null
    const form = new FormData()
    // Text fields go before the file so the server reads them first.
    if (content) form.append('content', content)
    if (parentId) form.append('parent_id', parentId)
    if (replyToId) form.append('reply_to_id', replyToId)
    form.append('file', file)
    const msg = await api(`/api/channels/${activeChannel.value.id}/upload`, { method: 'POST', form })
    countReply(msg)
    insertMessage(msg)
    return msg
  }

  async function openThread(msg) {
    if (!msg) return
    activeThread.value = msg
    threadReplies.value = []
    isThreadLoading.value = true
    try {
      const data = await api(`/api/messages/${msg.id}/thread`)
      activeThread.value = data.root || msg
      threadReplies.value = Array.isArray(data.replies) ? data.replies : []
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

  async function sendThreadReply(content, replyToId = null) {
    if (!activeThread.value) return null
    return sendMessage(content, activeThread.value.id, replyToId)
  }

  async function uploadThreadMedia(file, content = '', replyToId = null) {
    if (!activeThread.value) return null
    return uploadMedia(file, content, activeThread.value.id, replyToId)
  }

  // ---- WebSocket ----

  function startPingHeartbeat() {
    stopPingHeartbeat()
    pingTimer = setInterval(() => sendWSEvent('ping', { t: Date.now() }), 5000)
  }

  function stopPingHeartbeat() {
    if (pingTimer) {
      clearInterval(pingTimer)
      pingTimer = null
    }
  }

  // Bumped on every successful reconnect; voice and other views watch it to
  // restore their server-side state.
  const reconnectCount = ref(0)
  let hadConnection = false

  // After a reconnect, everything that changed while the socket was down is
  // fetched again: channels, members and the newest page of the open channel
  // (unless the user is reading older history, which stays as it is).
  function resyncAfterReconnect() {
    reconnectCount.value++
    fetchChannels()
    fetchMembers()
    const channel = activeChannel.value
    if (channel && channel.type === 'text' && !hasMoreAfter.value) fetchMessages(channel.id)
    if (activeThread.value) openThread(activeThread.value)
  }

  function scheduleReconnect() {
    clearTimeout(reconnectTimer)
    reconnectTimer = setTimeout(async () => {
      const signedIn = await authStore.checkAuth()
      if (signedIn === true) initWebSocket()
      else if (signedIn === null) scheduleReconnect() // server unreachable: keep trying
      // false: the session is gone; the login screen takes over.
    }, reconnectDelay)
    reconnectDelay = Math.min(reconnectDelay * 2, 30000)
  }

  function initWebSocket() {
    if (ws.value || !authStore.isAuthenticated) return

    // The session cookie authenticates the upgrade; no token in the URL.
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/ws`)

    socket.onopen = () => {
      isConnected.value = true
      reconnectDelay = 1000
      startPingHeartbeat()
      sendWSEvent('ping', { t: Date.now() })
      if (hadConnection) resyncAfterReconnect()
      hadConnection = true
    }

    socket.onmessage = event => {
      try {
        handleWSEvent(JSON.parse(event.data))
      } catch (err) {
        console.error('WS parse error:', err)
      }
    }

    socket.onclose = () => {
      isConnected.value = false
      stopPingHeartbeat()
      ws.value = null
      if (!authStore.isAuthenticated) return
      // Exponential backoff; a revoked session (401) stops the loop.
      scheduleReconnect()
    }

    ws.value = socket
  }

  function closeWebSocket() {
    hadConnection = false
    clearTimeout(reconnectTimer)
    stopPingHeartbeat()
    if (ws.value) {
      ws.value.onclose = null
      ws.value.close()
      ws.value = null
    }
    isConnected.value = false
  }

  function updateUserEverywhere(updated) {
    const idx = members.value.findIndex(m => m.id === updated.id)
    if (idx !== -1) members.value[idx] = { ...members.value[idx], ...updated }
    for (const list of [messages.value, threadReplies.value]) {
      list.forEach(m => {
        if (m.user_id === updated.id) {
          m.avatar_url = updated.avatar_url
          m.display_name = updated.display_name
        }
        if (m.reply_to?.user_id === updated.id) {
          m.reply_to.avatar_url = updated.avatar_url
          m.reply_to.display_name = updated.display_name
        }
      })
    }
    if (authStore.user?.id === updated.id) authStore.user = { ...authStore.user, ...updated }
    if (selectedUserProfile.value?.id === updated.id) {
      selectedUserProfile.value = { ...selectedUserProfile.value, ...updated }
    }
  }

  function applyToMessage(id, fn) {
    const m = messages.value.find(x => x.id === id)
    if (m) fn(m)
    const t = threadReplies.value.find(x => x.id === id)
    if (t) fn(t)
    if (activeThread.value?.id === id) fn(activeThread.value)
  }

  function loadedLists() {
    return [messages.value, threadReplies.value, activeThread.value ? [activeThread.value] : []]
  }

  // Keep "replied to" previews in sync with their original message.
  function onOriginalEdited(original) {
    if (original?.id) markPreviewEdited(loadedLists(), original)
  }

  function onOriginalDeleted(id) {
    markPreviewDeleted(loadedLists(), id)
  }

  function handleWSEvent(event) {
    const p = event.payload
    switch (event.type) {
      case 'pong':
        if (p?.t) voiceStore.recordPing(Date.now() - p.t)
        break

      case 'presence_snapshot':
        onlineUserIds.value = new Set(p || [])
        break

      case 'presence_update': {
        const next = new Set(onlineUserIds.value)
        if (p?.status === 'online') next.add(p.user_id)
        else if (p?.status === 'offline') next.delete(p.user_id)
        onlineUserIds.value = next
        break
      }

      case 'member_joined':
        fetchMembers()
        break

      case 'channels_changed':
        fetchChannels()
        break

      case 'message_create':
        if (!p) break
        countReply(p)
        insertMessage(p)
        break

      case 'user_update':
        if (p) updateUserEverywhere(p)
        break

      case 'message_update':
        if (!p) break
        applyToMessage(p.id, m => Object.assign(m, p))
        onOriginalEdited(p)
        break

      case 'message_delete':
        if (!p?.id) break
        removeFromWindow(p.id)
        threadReplies.value = threadReplies.value.filter(m => m.id !== p.id)
        onOriginalDeleted(p.id)
        if (p.parent_id) bumpReplyCount(p.parent_id, -1)
        if (activeThread.value?.id === p.id) closeThread()
        break

      case 'message_reaction':
        if (p?.message_id) applyToMessage(p.message_id, m => { m.reactions = p.reactions || [] })
        break

      case 'webrtc_offer':
        webrtcOfferHandler?.(p)
        break

      case 'webrtc_candidate':
        webrtcCandidateHandler?.(p)
        break

      case 'voice_snapshot':
        voiceStore.setVoiceSnapshot(p)
        break

      case 'voice_state_update':
        voiceStore.handleVoiceStateUpdate(p)
        break

      case 'voice_speaking':
        voiceStore.handleSpeakingEvent(p)
        break
    }
  }

  function sendWSEvent(type, payload) {
    if (ws.value && isConnected.value) {
      ws.value.send(JSON.stringify({ type, payload }))
    }
  }

  // ---- Admin: channels & categories ----

  async function createChannel({ categoryId, name, type, topic, sortOrder = 0 }) {
    const channel = await api('/api/admin/channels', {
      method: 'POST',
      json: { category_id: categoryId || null, name, type: type || 'text', topic: topic || '', sort_order: sortOrder }
    })
    await fetchChannels()
    selectChannel(channel)
    return channel
  }

  async function deleteChannel(channelId) {
    await api(`/api/admin/channels/${channelId}`, { method: 'DELETE' })
    if (activeChannel.value?.id === channelId) activeChannel.value = null
    await fetchChannels()
  }

  async function createCategory(name, sortOrder = 0) {
    const category = await api('/api/admin/categories', { method: 'POST', json: { name, sort_order: sortOrder } })
    await fetchChannels()
    return category
  }

  async function deleteCategory(categoryId) {
    await api(`/api/admin/categories/${categoryId}`, { method: 'DELETE' })
    await fetchChannels()
  }

  // ---- Profiles & mentions ----

  async function openUserProfile(userOrMessage) {
    const userId = userOrMessage?.user_id || userOrMessage?.id
    if (!userId) return
    const known = members.value.find(m => m.id === userId)
    selectedUserProfile.value = {
      id: userId,
      username: userOrMessage.username || known?.username || '',
      display_name: userOrMessage.display_name || known?.display_name || userOrMessage.username || '',
      avatar_url: userOrMessage.avatar_url || known?.avatar_url || '',
      role: userOrMessage.role || known?.role || 'user',
      created_at: userOrMessage.created_at || known?.created_at || null
    }
    try {
      const full = await api(`/api/users/${userId}`)
      selectedUserProfile.value = { ...selectedUserProfile.value, ...full }
    } catch (e) {
      console.warn('Failed to fetch full user profile:', e)
    }
  }

  function closeUserProfile() {
    selectedUserProfile.value = null
  }

  function insertMention(username) {
    if (username) pendingMention.value = username
  }

  // ---- Message actions ----

  async function editMessage(channelId, messageId, content) {
    if (!content.trim()) return null
    const updated = await api(`/api/channels/${channelId}/messages/${messageId}`, {
      method: 'PUT',
      json: { content: content.trim() }
    })
    applyToMessage(messageId, m => Object.assign(m, updated))
    onOriginalEdited(updated)
    return updated
  }

  async function deleteMessage(channelId, messageId) {
    await api(`/api/channels/${channelId}/messages/${messageId}`, { method: 'DELETE' })
    removeFromWindow(messageId)
    threadReplies.value = threadReplies.value.filter(m => m.id !== messageId)
    onOriginalDeleted(messageId)
    if (activeThread.value?.id === messageId) closeThread()
  }

  async function toggleReaction(messageId, emoji) {
    const data = await api(`/api/messages/${messageId}/reactions`, { method: 'POST', json: { emoji } })
    applyToMessage(messageId, m => { m.reactions = data.reactions || [] })
    return data.reactions
  }

  return {
    reconnectCount,
    categories,
    uncategorized,
    activeChannel,
    messages,
    hasMoreBefore,
    hasMoreAfter,
    isLoadingBefore,
    isLoadingAfter,
    isLoadingWindow,
    missedLiveCount,
    liveAppendSeq,
    latestLoadSeq,
    jumpTarget,
    toast,
    showToast,
    loadOlder,
    loadNewer,
    jumpToLatest,
    jumpToMessage,
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
    closeWebSocket,
    sendWSEvent,
    setWebRTCHandlers,
    handleWSEvent
  }
})
