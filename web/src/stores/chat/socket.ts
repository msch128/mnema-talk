import { platform } from '../../lib/platform'
import { decodeServerEvent } from '../../types/events'
import type { ServerEventPayloads, ClientEventPayloads } from '../../types/events'
import { usePermissionsStore } from '../permissions'
import { installGlobalSearch, uninstallGlobalSearch } from '../../lib/globalSearch'
import type { Cx } from './state'
import { fetchChannels } from './channels'
import { handleWSEvent } from './events'
import { fetchMembers } from './members'
import { fetchMessages } from './messageWindow'
import { fetchReadState } from './readState'
import { openThread } from './threads'

function startPingHeartbeat(cx: Cx) {
  const { cs } = cx
  stopPingHeartbeat(cx)
  cs.pingTimer = setInterval(() => sendWSEvent(cx, 'ping', { t: Date.now() }), 5000)
}

function stopPingHeartbeat(cx: Cx) {
  const { cs } = cx
  if (cs.pingTimer) {
    clearInterval(cs.pingTimer)
    cs.pingTimer = undefined
  }
}

// Refresh after every socket opens, including the first one: the initial
// HTTP snapshots were fetched before broadcasts could reach this client.
// Older history stays where the user is reading it.
function resyncState(cx: Cx) {
  const { cs } = cx
  const permissions = usePermissionsStore()
  permissions.load()
  permissions.loadRoles()
  fetchChannels(cx)
  fetchMembers(cx)
  fetchReadState(cx)
  const channel = cs.activeChannel.value
  if (channel && channel.type === 'text' && !cs.hasMoreAfter.value) fetchMessages(cx, channel.id)
  if (cs.activeThread.value) openThread(cx, cs.activeThread.value)
}

async function attemptReconnect(cx: Cx) {
  const { cs, authStore } = cx
  const accountId = authStore.user?.id
  if (!accountId) return
  const generation = cs.socketGeneration
  const check = ++cs.reconnectCheckSeq
  const signedIn = await authStore.checkAuth()
  if (generation !== cs.socketGeneration || check !== cs.reconnectCheckSeq || authStore.user?.id !== accountId) return
  if (signedIn === true) initWebSocket(cx)
  else if (signedIn === null) scheduleReconnect(cx) // server unreachable: keep trying
  // false: the session is gone; the login screen takes over.
}

function scheduleReconnect(cx: Cx) {
  const { cs } = cx
  clearTimeout(cs.reconnectTimer)
  cs.reconnectAttempt.value++
  cs.nextRetryAt.value = Date.now() + cs.reconnectDelay
  cs.reconnectTimer = setTimeout(() => attemptReconnect(cx), cs.reconnectDelay)
  cs.reconnectDelay = Math.min(cs.reconnectDelay * 2, 30000)
}

/** "Jetzt versuchen": skip the wait and reconnect immediately. */
export function retryNow(cx: Cx) {
  const { cs } = cx
  if (cs.isConnected.value || cs.ws.value) return
  clearTimeout(cs.reconnectTimer)
  cs.nextRetryAt.value = Date.now()
  attemptReconnect(cx)
}

export function initWebSocket(cx: Cx) {
  const { cs, authStore } = cx
  const accountId = authStore.user?.id
  if (cs.ws.value || !accountId) return
  const generation = ++cs.socketGeneration
  installGlobalSearch()

  // The session cookie authenticates the upgrade; no token in the URL.
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  // The trusted desktop client cannot reach the server from its own origin:
  // its host opens the socket (doc/desktop-trusted-client.md).
  const socket = platform.openSocket(`${protocol}//${window.location.host}/api/ws`)

  const currentSocket = () => cs.ws.value === socket && cs.socketGeneration === generation
  const currentAccount = () => authStore.user?.id === accountId
  socket.onopen = () => {
    if (!currentSocket() || !currentAccount()) return
    cs.isConnected.value = true
    cs.wasConnected.value = true
    cs.reconnectAttempt.value = 0
    cs.nextRetryAt.value = 0
    cs.reconnectDelay = 1000
    startPingHeartbeat(cx)
    sendWSEvent(cx, 'ping', { t: Date.now() })
    if (cs.isIdle) sendWSEvent(cx, 'presence_idle', { idle: true })
    if (cs.hadConnection) cs.reconnectCount.value++
    resyncState(cx)
    cs.hadConnection = true
  }

  socket.onmessage = (event: { data: string }) => {
    if (!currentSocket() || !currentAccount()) return
    try {
      const decoded = decodeServerEvent(JSON.parse(event.data))
      if (decoded) handleWSEvent(cx, decoded)
    } catch (err) {
      console.error('WS parse error:', err)
    }
  }

  socket.onclose = (event?: { code?: number }) => {
    if (!currentSocket()) return
    cs.suppressedPresence.clear()
    cs.isConnected.value = false
    stopPingHeartbeat(cx)
    cs.ws.value = null
    if (!currentAccount()) return
    // The server closed this tab for a newer one of the same account (too
    // many connections). Reconnecting at once would evict another tab in
    // turn; this one reconnects when it is shown again (visibilitychange).
    if (event?.code === 4001) {
      cs.reconnectAttempt.value = 0
      cs.nextRetryAt.value = 0
      return
    }
    // Exponential backoff; a revoked session (401) stops the loop.
    scheduleReconnect(cx)
  }

  cs.ws.value = socket
}

export function closeWebSocket(cx: Cx) {
  const { cs } = cx
  ++cs.socketGeneration
  cs.suppressedPresence.clear()
  uninstallGlobalSearch()
  cs.hadConnection = false
  cs.wasConnected.value = false
  cs.reconnectAttempt.value = 0
  clearTimeout(cs.reconnectTimer)
  stopPingHeartbeat(cx)
  if (cs.ws.value) {
    cs.ws.value.onclose = null
    cs.ws.value.close()
    cs.ws.value = null
  }
  cs.isConnected.value = false
}

export function sendWSEvent<K extends keyof ClientEventPayloads>(cx: Cx, type: K, payload: ClientEventPayloads[K]) {
  const { cs } = cx
  if (cs.ws.value && cs.isConnected.value) {
    cs.ws.value.send(JSON.stringify({ type, payload }))
  }
}

export function setWebRTCHandlers(cx: Cx, { onOffer, onCandidate, onKicked, onMedia }: {
  onOffer: (payload: ServerEventPayloads['webrtc_offer']) => void | Promise<void>
  onCandidate: (payload: ServerEventPayloads['webrtc_candidate']) => void | Promise<void>
  onKicked?: (payload: ServerEventPayloads['voice_kicked']) => void | Promise<void>
  onMedia?: (payload: ServerEventPayloads['media_handshake']) => void | Promise<void>
}) {
  const { cs } = cx
  cs.webrtcOfferHandler = onOffer
  cs.webrtcCandidateHandler = onCandidate
  cs.voiceKickedHandler = onKicked || null
  cs.mediaHandshakeHandler = onMedia || null
}

export function installVisibilityRetry(cx: Cx) {
  const { cs, authStore } = cx
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') {
        if (authStore.isAuthenticated && (!cs.ws.value || !cs.isConnected.value)) {
          retryNow(cx)
        } else if (cs.isConnected.value) {
          sendWSEvent(cx, 'ping', { t: Date.now() })
        }
      }
    })
  }
}
