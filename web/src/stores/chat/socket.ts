// The chat store's side of the WebSocket: it is the host of the transport
// (stores/socket.ts). It says who is signed in, routes every event into the
// chat modules and reads the state again after each (re)connect.
import { decodeServerEvent } from '../../types/events'
import type { ServerEventPayloads, ClientEventPayloads } from '../../types/events'
import { useEmojiStore } from '../emoji'
import { usePermissionsStore } from '../permissions'
import { useBlocksStore } from '../blocks'
import { useReportsOpenStore } from '../reportsOpen'
import { useServerFeaturesStore } from '../serverFeatures'
import { closeSocket, installSocketRetry, openSocket, retrySocketNow, sendSocketEvent, type SocketHost, type SocketState } from '../socket'
import { installGlobalSearch, uninstallGlobalSearch } from '../../chat/lib/globalSearch'
import type { Cx } from './state'
import { fetchChannels } from './channels'
import { fetchDms } from './dms'
import { handleWSEvent } from './events'
import { fetchMembers } from './members'
import { refreshLatest } from './messageWindow'
import { fetchReadState } from './readState'
import { fetchNotificationSettings } from './notifySettings'
import { fetchFollowedThreads } from './threadFollow'
import { loadNotifyPreview } from '../../shared/lib/notifyPreview'
import { openThread } from './threads'
import { outboxReconnected, restoreOutbox } from './outbox'

// Refresh after every socket opens, including the first one: the initial
// HTTP snapshots were fetched before broadcasts could reach this client.
// Older history stays where the user is reading it.
function resyncState(cx: Cx) {
  const { cs } = cx
  const permissions = usePermissionsStore()
  permissions.load()
  permissions.loadRoles()
  void useReportsOpenStore().load()
  void useEmojiStore().load()
  // Not yet read (the server was restarting at start): sends need the list to carry their nonce. A socket that reopens may talk to a
  // restarted server: what it says about itself (its features, storage that is down) is read again.
  const features = useServerFeaturesStore()
  void (cs.reconnectCount.value > 0 ? features.refresh() : features.load())
  fetchChannels(cx)
  fetchMembers(cx)
  fetchReadState(cx)
  fetchNotificationSettings(cx)
  // Read after the socket is open (and every time it reopens), so no conversation created in between is missed.
  void fetchDms(cx)
  // Blocks only reach this member's own sessions; a first open follows the sign-in snapshot by an instant.
  if (cs.reconnectCount.value > 0) void useBlocksStore().load()
  void fetchFollowedThreads(cx)
  void loadNotifyPreview()
  const channel = cs.activeChannel.value
  if (channel && channel.type === 'text' && !cs.hasMoreAfter.value) void refreshLatest(cx, channel.id)
  if (cs.activeThread.value) openThread(cx, cs.activeThread.value)
  // Messages written while the link was down go out now, in order.
  outboxReconnected(cx)
}

function socketUrl(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  // Conversations are only sent to sockets that ask for them (dm=1), and only a server that has them understands the question.
  const dm = useServerFeaturesStore().dm ? '?dm=1' : ''
  return `${protocol}//${window.location.host}/api/ws${dm}`
}

function handleSocketMessage(cx: Cx, data: string) {
  try {
    const decoded = decodeServerEvent(JSON.parse(data))
    if (decoded) handleWSEvent(cx, decoded)
  } catch (err) {
    console.error('WS parse error:', err)
  }
}

function hostOf(cx: Cx): SocketHost {
  const { cs, authStore } = cx
  return {
    accountId: () => authStore.user?.id,
    isAuthenticated: () => authStore.isAuthenticated,
    checkAuth: () => authStore.checkAuth(),
    url: socketUrl,
    opening: () => {
      restoreOutbox(cx)
      installGlobalSearch()
    },
    connected: () => { if (cs.isIdle) sendWSEvent(cx, 'presence_idle', { idle: true }) },
    resync: () => resyncState(cx),
    message: data => handleSocketMessage(cx, data),
    dropped: () => cs.suppressedPresence.clear(),
    closing: () => {
      cs.suppressedPresence.clear()
      uninstallGlobalSearch()
    }
  }
}

const hosted = new WeakSet<SocketState>()

/** The transport of this chat state, with the chat store attached as its host. */
function transportOf(cx: Cx): SocketState {
  const socket = cx.cs.socket
  if (!hosted.has(socket)) {
    hosted.add(socket)
    socket.host = hostOf(cx)
  }
  return socket
}

/** The member asked for it: skip the wait and reconnect immediately. */
export function retryNow(cx: Cx) {
  retrySocketNow(transportOf(cx))
}

export function initWebSocket(cx: Cx) {
  openSocket(transportOf(cx))
}

export function closeWebSocket(cx: Cx) {
  closeSocket(transportOf(cx))
}

export function sendWSEvent<K extends keyof ClientEventPayloads>(cx: Cx, type: K, payload: ClientEventPayloads[K]) {
  sendSocketEvent(transportOf(cx), type, payload)
}

export function setWebRTCHandlers(cx: Cx, { onOffer, onCandidate, onKicked, onMedia, onTalkMoved }: {
  onOffer: (payload: ServerEventPayloads['webrtc_offer']) => void | Promise<void>
  onCandidate: (payload: ServerEventPayloads['webrtc_candidate']) => void | Promise<void>
  onKicked?: (payload: ServerEventPayloads['voice_kicked']) => void | Promise<void>
  onMedia?: (payload: ServerEventPayloads['media_handshake']) => void | Promise<void>
  onTalkMoved?: (payload: ServerEventPayloads['talk_moved']) => void | Promise<void>
}) {
  const { cs } = cx
  cs.webrtcOfferHandler = onOffer
  cs.webrtcCandidateHandler = onCandidate
  cs.voiceKickedHandler = onKicked || null
  cs.mediaHandshakeHandler = onMedia || null
  cs.talkMovedHandler = onTalkMoved || null
}

export function installVisibilityRetry(cx: Cx) {
  installSocketRetry(transportOf(cx))
}
