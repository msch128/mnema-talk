import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'
import { useAuthStore } from './auth'
import { useVoiceStore } from './voice'
import { useToastStore } from './toast'
import { t } from '../i18n'
import {
  PAGE_SIZE, WINDOW_CAP, emptyWindow, fromLatest, fromAround,
  prependOlder, appendNewer, appendLive, removeMessage
} from '../lib/messageWindow'
import { markPreviewEdited, markPreviewDeleted } from '../lib/replies'
import { currentRoute, navigate } from '../lib/router'
import { mentionsUser, shouldNotify, createKeyedThrottle } from '../lib/chatLogic'
import { snapshotToMap, applyPresenceUpdate, isQuiet, IDLE_AFTER_MS } from '../lib/presence'
import { installGlobalSearch, uninstallGlobalSearch } from '../lib/globalSearch'

const MAX_PENDING_LIVE = 200
// A channel is marked read on the server at most this often while messages stream in.
export const MARK_READ_INTERVAL_MS = 2000
// Typing notices expire when the user stops (the server relays one per 3 s).
export const TYPING_EXPIRY_MS = 5000
const TYPING_THROTTLE_MS = 3000

// Back in the tab (visible / focused): the store catches up on reading. One
// pair of listeners for the page; the live store registers its handler.
let onReturnToTab = null
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => onReturnToTab?.())
  window.addEventListener('focus', () => onReturnToTab?.())
}

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
  const activeThread = ref(null)
  const threadReplies = ref([])
  const isThreadLoading = ref(false)
  const ws = ref(null)
  const isConnected = ref(false)

  // Community members & real-time presence
  const members = ref([])
  // Live status of every connected user: user_id → online | away | dnd | focus.
  const presenceById = ref({})
  const onlineUserIds = computed(() => new Set(Object.keys(presenceById.value)))
  /** Live status of a user; 'offline' when not connected. */
  function presenceOf(userId) {
    return presenceById.value[userId] || 'offline'
  }
  const showMemberList = ref(true)
  const selectedUserProfile = ref(null)
  const pendingMention = ref('')

  // Read states per channel: channel_id -> { channel_id, unread_count, mention_count, last_read_at, notify_level }
  const readStates = ref({})
  const activeChannelLastReadAt = ref(null)

  // Typing state per channel: channel_id -> Array<{ user_id, username, display_name }>
  const typingByChannel = ref({})
  const typingTimers = new Map()
  // channel_id -> when my last typing notice for it went out.
  const lastTypingSentAt = new Map()

  const authStore = useAuthStore()
  const voiceStore = useVoiceStore()

  // Desktop notification permission: 'default' | 'granted' | 'denied' | 'unsupported'
  const notificationPermission = ref(
    typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
  )
  // Thread to open once the jump to its root message has landed (search / notification targets).
  let pendingThreadOpen = null

  let pingTimer = null
  let reconnectTimer = null
  let reconnectDelay = 1000
  let webrtcOfferHandler = null
  let webrtcCandidateHandler = null
  let voiceKickedHandler = null

  function setWebRTCHandlers({ onOffer, onCandidate, onKicked }) {
    webrtcOfferHandler = onOffer
    webrtcCandidateHandler = onCandidate
    voiceKickedHandler = onKicked || null
  }

  const allChannels = computed(() => [
    ...categories.value.flatMap(c => c.channels || []),
    ...uncategorized.value
  ])

  /** Whether channelId is a known voice channel. */
  function isVoiceChannel(channelId) {
    return allChannels.value.some(c => c.id === channelId && c.type === 'voice')
  }

  async function fetchChannels() {
    try {
      const data = await api('/api/channels')
      categories.value = data.categories || []
      uncategorized.value = data.uncategorized || []

      const fresh = activeChannel.value && allChannels.value.find(c => c.id === activeChannel.value.id)
      if (fresh) {
        // Same channel, new object: a rename or topic change shows in the header.
        activeChannel.value = fresh
      } else {
        const routeCh = allChannels.value.find(c => c.id === currentRoute.value?.channelId)
        const next = routeCh || allChannels.value.find(c => c.type === 'text') || allChannels.value[0]
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

  function countMessageOf(userId) {
    const m = members.value.find(x => x.id === userId)
    if (m) m.message_count = (m.message_count || 0) + 1
  }

  // A deletion may take thread replies of several people with it: reload the
  // members' totals once things calm down.
  let memberStatsTimer = null
  function refreshMemberStats() {
    clearTimeout(memberStatsTimer)
    memberStatsTimer = setTimeout(fetchMembers, 3000)
  }

  const onlineMembers = computed(() => members.value.filter(m => onlineUserIds.value.has(m.id)))
  const offlineMembers = computed(() => members.value.filter(m => !onlineUserIds.value.has(m.id)))

  async function selectChannel(channel) {
    if (!channel) return
    if (activeChannel.value?.id !== channel.id) suppressAutoReadFor = null
    activeChannel.value = channel
    closeThread()
    pendingLive = []
    resetWindow()
    setWindow(emptyWindow())
    activeChannelLastReadAt.value = readStates.value[channel.id]?.last_read_at || null
    await fetchMessages(channel.id)
    if (channel.type !== 'voice') {
      await markChannelRead(channel.id)
    }
  }

  // ---- Message window (infinite scroll in both directions) ----

  // Bumped whenever the window is replaced; responses from an older
  // generation (other channel, earlier jump) are ignored.
  let windowGen = 0
  let pendingLive = []
  let jumpSeq = 0

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
    useToastStore().info(text)
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
    const ok = await fetchMessages(activeChannel.value.id)
    if (ok) markActiveChannelReadIfReading()
    return ok
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
      markActiveChannelReadIfReading()
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
    const ok = await jumpToRootMessage(id)
    const pending = pendingThreadOpen
    if (pending && pending.rootId === id) {
      pendingThreadOpen = null
      if (ok && activeChannel.value?.id === pending.channelId) openThread(pending.rootId)
    }
    return ok
  }

  /**
   * Navigates to any message (search result, notification): thread replies
   * open their thread on top of the root message, the only valid jump anchor.
   */
  function goToMessage(msg) {
    if (!msg?.id || !msg.channel_id) return
    const rootId = msg.parent_id || msg.id
    pendingThreadOpen = msg.parent_id ? { channelId: msg.channel_id, rootId } : null
    navigate(`/c/${msg.channel_id}/m/${rootId}`)
  }

  async function jumpToRootMessage(id) {
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
        showToast(e?.status === 404 ? t('chat.messageNotFound') : t('chat.messageLoadFailed'))
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
        // My own messages are not news to me.
        if (!isLoadingWindow.value && !isOwnMessage(msg)) missedLiveCount.value++
      }
      return
    }
    const { window: win, status } = appendLive(getWindow(), msg, { cap: WINDOW_CAP })
    if (status !== 'appended') return
    messages.value = win.messages
    hasMoreBefore.value = win.hasMoreBefore
    liveAppendSeq.value++
  }

  function isOwnMessage(msg) {
    const me = authStore.user?.id
    return !!me && msg?.user_id === me
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

  // Bumped by every thread open and close: a load that finishes after
  // another thread was opened (or the thread was closed) is dropped.
  let threadGen = 0

  async function openThread(msg) {
    if (!msg) return
    const id = typeof msg === 'string' ? msg : msg.id
    if (!id) return
    const gen = ++threadGen
    activeThread.value = typeof msg === 'object' ? msg : { id }
    threadReplies.value = []
    isThreadLoading.value = true
    try {
      const data = await api(`/api/messages/${id}/thread`)
      if (gen !== threadGen || activeThread.value?.id !== id) return
      activeThread.value = data.root || (typeof msg === 'object' ? msg : { id })
      threadReplies.value = Array.isArray(data.replies) ? data.replies : []
    } catch (e) {
      if (gen === threadGen) console.error('Failed to load thread:', e)
    } finally {
      if (gen === threadGen) isThreadLoading.value = false
    }
  }

  function closeThread() {
    threadGen++
    activeThread.value = null
    threadReplies.value = []
    isThreadLoading.value = false
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
  // For the connection banner: true once the socket has been up at least once
  // in this session, how many reconnects were tried, and when the next one runs.
  const wasConnected = ref(false)
  const reconnectAttempt = ref(0)
  const nextRetryAt = ref(0)

  // After a reconnect, everything that changed while the socket was down is
  // fetched again: channels, members and the newest page of the open channel
  // (unless the user is reading older history, which stays as it is).
  function resyncAfterReconnect() {
    reconnectCount.value++
    fetchChannels()
    fetchMembers()
    fetchReadState()
    const channel = activeChannel.value
    if (channel && channel.type === 'text' && !hasMoreAfter.value) fetchMessages(channel.id)
    if (activeThread.value) openThread(activeThread.value)
  }

  async function attemptReconnect() {
    const signedIn = await authStore.checkAuth()
    if (signedIn === true) initWebSocket()
    else if (signedIn === null) scheduleReconnect() // server unreachable: keep trying
    // false: the session is gone; the login screen takes over.
  }

  function scheduleReconnect() {
    clearTimeout(reconnectTimer)
    reconnectAttempt.value++
    nextRetryAt.value = Date.now() + reconnectDelay
    reconnectTimer = setTimeout(attemptReconnect, reconnectDelay)
    reconnectDelay = Math.min(reconnectDelay * 2, 30000)
  }

  /** "Jetzt versuchen": skip the wait and reconnect immediately. */
  function retryNow() {
    if (isConnected.value || ws.value) return
    clearTimeout(reconnectTimer)
    nextRetryAt.value = Date.now()
    attemptReconnect()
  }

  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        if (authStore.isAuthenticated && (!ws.value || !isConnected.value)) {
          retryNow()
        } else if (isConnected.value) {
          sendWSEvent('ping', { t: Date.now() })
        }
      }
    })
  }

  function initWebSocket() {
    if (ws.value || !authStore.isAuthenticated) return
    installGlobalSearch()

    // The session cookie authenticates the upgrade; no token in the URL.
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/ws`)

    socket.onopen = () => {
      isConnected.value = true
      wasConnected.value = true
      reconnectAttempt.value = 0
      nextRetryAt.value = 0
      reconnectDelay = 1000
      startPingHeartbeat()
      sendWSEvent('ping', { t: Date.now() })
      if (isIdle) sendWSEvent('presence_idle', { idle: true })
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
    uninstallGlobalSearch()
    hadConnection = false
    wasConnected.value = false
    reconnectAttempt.value = 0
    clearTimeout(reconnectTimer)
    stopPingHeartbeat()
    if (ws.value) {
      ws.value.onclose = null
      ws.value.close()
      ws.value = null
    }
    isConnected.value = false
  }

  // Author fields that messages and reply previews carry a copy of.
  const AUTHOR_FIELDS = ['avatar_url', 'display_name']

  function updateUserEverywhere(updated) {
    if (!updated?.id) return
    const idx = members.value.findIndex(m => m.id === updated.id)
    if (idx !== -1) members.value[idx] = { ...members.value[idx], ...updated }
    // Only fields the update carries: a partial one (e.g. {id, disabled})
    // must not blank the names and avatars.
    const fields = AUTHOR_FIELDS.filter(f => f in updated)
    if (fields.length) {
      const copy = target => { for (const f of fields) target[f] = updated[f] }
      for (const list of loadedLists()) {
        for (const m of list) {
          if (m.user_id === updated.id) copy(m)
          if (m.reply_to?.user_id === updated.id) copy(m.reply_to)
        }
      }
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
        presenceById.value = snapshotToMap(p)
        break

      case 'presence_update':
        presenceById.value = applyPresenceUpdate(presenceById.value, p)
        break

      case 'member_joined':
        fetchMembers()
        break

      case 'channels_changed':
        fetchChannels()
        break

      case 'typing':
        if (p) handleTypingEvent(p)
        break

      case 'read_state':
        if (!p?.channel_id) break
        if (p.refresh) {
          fetchReadState()
        } else {
          const prev = readStates.value[p.channel_id] || { channel_id: p.channel_id, notify_level: 'all' }
          readStates.value = {
            ...readStates.value,
            [p.channel_id]: {
              ...prev,
              ...p
            }
          }
        }
        break

      case 'message_create': {
        if (!p) break
        countReply(p)
        insertMessage(p)
        countMessageOf(p.user_id)

        clearTypingForUser(p.channel_id, p.user_id)

        const isOwn = p.user_id === authStore.user?.id
        const isCurrentChannel = p.channel_id === activeChannel.value?.id
        const isMention = messageMentionsMe(p)

        // Read right now: open channel, visible tab, window at the newest end.
        if (isCurrentChannel && isReadingActiveChannel()) {
          if (!isOwn) markReadThrottled.call(p.channel_id)
        } else if (!isOwn && p.channel_id) {
          const state = readStates.value[p.channel_id] || {
            channel_id: p.channel_id,
            unread_count: 0,
            mention_count: 0,
            notify_level: 'all'
          }
          readStates.value = {
            ...readStates.value,
            [p.channel_id]: {
              ...state,
              unread_count: (state.unread_count || 0) + 1,
              mention_count: isMention ? (state.mention_count || 0) + 1 : (state.mention_count || 0)
            }
          }
        }

        if (!isOwn && p.channel_id) {
          triggerBrowserNotification(p, isMention)
        }
        break
      }

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
        refreshMemberStats()
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

      case 'voice_rooms':
        voiceStore.setVoiceRooms(p)
        break

      case 'user_stats':
        if (p?.user_id) {
          const m = members.value.find(x => x.id === p.user_id)
          if (m) m.voice_seconds = p.voice_seconds
        }
        break

      case 'webrtc_media_state':
        voiceStore.handleMediaState(p)
        break

      case 'voice_speaking':
        voiceStore.handleSpeakingEvent(p)
        break

      case 'voice_mute_state':
        voiceStore.handleMuteState(p)
        break

      case 'voice_kicked':
        // An admin removed me from voice; the call ends locally.
        if (voiceKickedHandler) voiceKickedHandler(p)
        else if (voiceStore.currentChannelId) voiceStore.disconnect()
        break
    }
  }

  // ---- Presence: idle detection and the user's own choice ----

  // After IDLE_AFTER_MS without input the server shows "online" as "away".
  let isIdle = false
  let idleTimer = null
  function markActive() {
    clearTimeout(idleTimer)
    idleTimer = setTimeout(() => {
      isIdle = true
      sendWSEvent('presence_idle', { idle: true })
    }, IDLE_AFTER_MS)
    if (isIdle) {
      isIdle = false
      sendWSEvent('presence_idle', { idle: false })
    }
  }
  let lastActivityAt = 0
  function onActivity() {
    // Input events fire constantly; re-arming once a second is plenty.
    const now = Date.now()
    if (now - lastActivityAt < 1000 && !isIdle) return
    lastActivityAt = now
    markActive()
  }
  if (typeof window !== 'undefined') {
    for (const ev of ['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart']) {
      window.addEventListener(ev, onActivity, { passive: true })
    }
    markActive()
  }

  /** Sets the signed-in user's presence (online, away, dnd, focus). */
  async function setMyPresence(presence) {
    const me = authStore.user
    if (!me) return
    const previous = me.presence
    authStore.user = { ...me, presence }
    if (presenceById.value[me.id]) {
      presenceById.value = applyPresenceUpdate(presenceById.value, {
        user_id: me.id,
        status: presence === 'online' && isIdle ? 'away' : presence
      })
    }
    try {
      authStore.user = await api('/api/users/me/presence', { method: 'PUT', json: { presence } })
    } catch (e) {
      authStore.user = { ...authStore.user, presence: previous }
      throw e
    }
  }

  /** Sets a status line: the user's own, or (admins) anyone's. */
  async function setStatusText(userId, text) {
    const own = userId === authStore.user?.id
    const updated = await api(own ? '/api/users/me/status' : `/api/admin/users/${userId}/status`, {
      method: 'PUT',
      json: { status_text: text }
    })
    updateUserEverywhere(updated)
    return updated
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

  async function updateChannel(channelId, { name, topic }) {
    const updated = await api(`/api/admin/channels/${channelId}`, {
      method: 'PATCH',
      json: { name, topic }
    })
    await fetchChannels()
    return updated
  }

  async function updateCategory(categoryId, { name }) {
    const updated = await api(`/api/admin/categories/${categoryId}`, {
      method: 'PATCH',
      json: { name }
    })
    await fetchChannels()
    return updated
  }

  // ---- Read state & Notifications ----

  async function fetchReadState() {
    try {
      const data = await api('/api/read-state')
      const map = {}
      if (Array.isArray(data)) {
        for (const item of data) {
          if (item.channel_id) {
            map[item.channel_id] = {
              channel_id: item.channel_id,
              unread_count: item.unread_count || 0,
              mention_count: item.mention_count || 0,
              last_read_at: item.last_read_at || null,
              notify_level: item.notify_level || 'all'
            }
          }
        }
      }
      readStates.value = map
      // The server may still hold counts for the channel being read right now.
      markActiveChannelReadIfReading()
      return map
    } catch (e) {
      console.warn('Failed to fetch read-state:', e)
      return {}
    }
  }

  async function markChannelRead(channelId) {
    if (!channelId || isVoiceChannel(channelId)) return
    if (suppressAutoReadFor === channelId) suppressAutoReadFor = null

    const prev = readStates.value[channelId] || { channel_id: channelId, notify_level: 'all' }
    readStates.value = {
      ...readStates.value,
      [channelId]: {
        ...prev,
        unread_count: 0,
        mention_count: 0,
        last_read_at: new Date().toISOString()
      }
    }
    try {
      await api(`/api/channels/${channelId}/read`, { method: 'POST' })
    } catch (e) {
      console.warn('Failed to mark channel read:', e)
    }
  }

  let suppressAutoReadFor = null
  const markReadThrottled =createKeyedThrottle(id => { markChannelRead(id) }, MARK_READ_INTERVAL_MS)

  function isTabVisible() {
    return typeof document === 'undefined' || !document.hidden
  }

  // The user can see the newest messages of the open text channel.
  function isReadingActiveChannel() {
    const ch = activeChannel.value
    return !!ch && ch.type !== 'voice' && ch.id !== suppressAutoReadFor && isTabVisible() && !hasMoreAfter.value && !isLoadingWindow.value
  }

  // Catch up after the tab becomes visible again, a jump to the present or a
  // read-state refresh: clears whatever piled up while nobody was looking.
  function markActiveChannelReadIfReading() {
    if (!isReadingActiveChannel()) return
    const id = activeChannel.value.id
    const st = readStates.value[id]
    if (st && (st.unread_count > 0 || st.mention_count > 0)) markReadThrottled.call(id)
  }

  onReturnToTab = markActiveChannelReadIfReading

  async function markChannelUnread(channelId, messageId) {
    if (!channelId || !messageId || isVoiceChannel(channelId)) return

    // Keep the channel unread even though it is open: no auto-read until the
    // user marks it read (Esc) or switches channel.
    if (channelId === activeChannel.value?.id) suppressAutoReadFor = channelId
    markReadThrottled.cancel(channelId)
    try {
      await api(`/api/channels/${channelId}/unread`, {
        method: 'POST',
        json: { message_id: messageId }
      })
    } catch (e) {
      if (suppressAutoReadFor === channelId) suppressAutoReadFor = null
      throw e
    }
    await fetchReadState()
    if (channelId === activeChannel.value?.id) {
      // The "new since" divider moves to the chosen message.
      activeChannelLastReadAt.value = readStates.value[channelId]?.last_read_at || null
    }
  }

  async function setNotificationLevel(channelId, level) {
    if (!channelId || !level || isVoiceChannel(channelId)) return

    const prev = readStates.value[channelId] || { channel_id: channelId, unread_count: 0, mention_count: 0, last_read_at: null }
    readStates.value = {
      ...readStates.value,
      [channelId]: {
        ...prev,
        notify_level: level
      }
    }
    try {
      await api(`/api/channels/${channelId}/notifications`, {
        method: 'PUT',
        json: { level }
      })
    } catch (e) {
      console.warn('Failed to set notification level:', e)
    }
  }

  /** 'all' | 'mentions' | 'mute' for a channel (default 'all'). */
  function notificationLevel(channelId) {
    return readStates.value[channelId]?.notify_level || 'all'
  }

  // ---- Typing indicators ----

  function sendTyping(channelId) {
    const id = channelId || activeChannel.value?.id
    if (!id || isVoiceChannel(id)) return

    // Per channel: typing in a thread of another channel right after this one
    // still announces itself there.
    const now = Date.now()
    if (now - (lastTypingSentAt.get(id) ?? -Infinity) < TYPING_THROTTLE_MS) return
    lastTypingSentAt.set(id, now)
    sendWSEvent('typing', { channel_id: id })
  }

  function handleTypingEvent(payload) {
    if (!payload?.channel_id || !payload?.user_id) return
    const { channel_id, user_id } = payload
    if (user_id === authStore.user?.id) return

    const member = members.value.find(m => m.id === user_id)
    const typer = {
      user_id,
      username: payload.username || member?.username || '',
      display_name: payload.display_name || member?.display_name || member?.username || ''
    }

    const current = typingByChannel.value[channel_id] || []
    const filtered = current.filter(u => u.user_id !== user_id)
    typingByChannel.value = {
      ...typingByChannel.value,
      [channel_id]: [...filtered, typer]
    }

    const key = `${channel_id}:${user_id}`
    if (typingTimers.has(key)) {
      clearTimeout(typingTimers.get(key))
    }
    const timer = setTimeout(() => {
      typingTimers.delete(key)
      const list = typingByChannel.value[channel_id] || []
      typingByChannel.value = {
        ...typingByChannel.value,
        [channel_id]: list.filter(u => u.user_id !== user_id)
      }
    }, TYPING_EXPIRY_MS)
    typingTimers.set(key, timer)
  }

  function clearTypingForUser(channelId, userId) {
    if (!channelId || !userId) return
    const key = `${channelId}:${userId}`
    if (typingTimers.has(key)) {
      clearTimeout(typingTimers.get(key))
      typingTimers.delete(key)
    }
    if (typingByChannel.value[channelId]) {
      const list = typingByChannel.value[channelId].filter(u => u.user_id !== userId)
      typingByChannel.value = {
        ...typingByChannel.value,
        [channelId]: list
      }
    }
  }

  /** Whether msg mentions the signed-in user (@username, @all, @here) or replies to them. */
  function messageMentionsMe(msg) {
    const me = authStore.user
    if (!me || !msg) return false
    if (msg.reply_to?.user_id === me.id) return true
    if (Array.isArray(msg.mentions)) return msg.mentions.includes(me.id)
    return mentionsUser(msg.content || '', me.username)
  }

  function triggerBrowserNotification(msg, isMention) {
    if (typeof Notification === 'undefined') return
    // Do not disturb and focus silence everything.
    if (isQuiet(authStore.user?.presence)) return
    if (!shouldNotify({
      permission: Notification.permission,
      level: notificationLevel(msg.channel_id),
      isMention,
      hidden: !isTabVisible(),
      isCurrentChannel: msg.channel_id === activeChannel.value?.id
    })) return

    const channel = allChannels.value.find(c => c.id === msg.channel_id)
    const channelName = channel ? `#${channel.name}` : ''
    const sender = msg.display_name || msg.username || ''
    const title = channelName ? `${sender} (${channelName})` : sender

    try {
      const n = new Notification(title, {
        body: msg.content || (msg.attachments?.length ? t('chat.attachment') : ''),
        icon: '/favicon.svg',
        tag: msg.channel_id
      })
      n.onclick = () => {
        if (typeof window !== 'undefined') window.focus()
        goToMessage(msg)
        n.close()
      }
    } catch (e) {
      console.warn('Browser notification failed:', e)
    }
  }

  async function requestNotificationPermission() {
    if (typeof Notification === 'undefined') return 'unsupported'
    try {
      notificationPermission.value = await Notification.requestPermission()
    } catch {
      notificationPermission.value = Notification.permission
    }
    return notificationPermission.value
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
      // Closed, or another profile opened, while this one loaded.
      if (selectedUserProfile.value?.id !== userId) return
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
    wasConnected,
    reconnectAttempt,
    nextRetryAt,
    retryNow,
    categories,
    allChannels,
    isVoiceChannel,
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
    showToast,
    loadOlder,
    loadNewer,
    jumpToLatest,
    jumpToMessage,
    isConnected,
    members,
    presenceById,
    presenceOf,
    onlineUserIds,
    setMyPresence,
    setStatusText,
    messageMentionsMe,
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
    updateChannel,
    createCategory,
    deleteCategory,
    updateCategory,
    readStates,
    activeChannelLastReadAt,
    fetchReadState,
    markChannelRead,
    markChannelUnread,
    setNotificationLevel,
    notificationLevel,
    notificationPermission,
    goToMessage,
    typingByChannel,
    sendTyping,
    requestNotificationPermission,
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
