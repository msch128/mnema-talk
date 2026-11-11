// The WebSocket transport: one connection to the server with its heartbeat,
// reconnect backoff and the retry on visibility and network changes. It knows
// nothing about chat or voice: what an event means, and what is refreshed
// after a reconnect, is the host's business (stores/chat/socket.ts).
import { defineStore } from 'pinia'
import { ref, shallowRef } from 'vue'
import { platform, type PlatformSocket } from '../shared/lib/platform'
import { visibleInterval } from '../shared/lib/pageVisibility'
import type { ClientEventPayloads } from '../types/events'

type Timer = ReturnType<typeof setTimeout>

const PING_INTERVAL_MS = 5000
// A hidden tab only needs to learn that the socket died before the next push is missed; the server pings every 30 s itself.
const PING_HIDDEN_INTERVAL_MS = 30000
// A socket that answered no ping for this long is dead, even if the browser still reports it open.
const PONG_TIMEOUT_MS = 15000
// A connect that has not opened by now is stuck (half-open TCP after a network change); the browser's own give-up takes minutes.
const CONNECT_TIMEOUT_MS = 10000

/** Who the connection belongs to and what happens around it; the transport calls it, never the other way round. */
export interface SocketHost {
  /** The signed-in account, or undefined: no account, no socket. */
  accountId(): string | undefined
  isAuthenticated(): boolean
  /** true: still signed in; false: the session is gone; null: the server cannot be reached. */
  checkAuth(): Promise<boolean | null>
  url(): string
  /** A connect starts. */
  opening(): void
  /** The socket is open and the first ping is out. */
  connected(): void
  /** After every open, the first one included: read what broadcasts could not deliver. */
  resync(): void
  /** One frame of the current connection. */
  message(data: string): void
  /** The connection was lost. */
  dropped(): void
  /** The socket is closed on purpose (sign-out, community reset). */
  closing(): void
}

/** Everything the transport keeps: the socket, what the connection banner shows, generations and timers. */
export function createSocketState() {
  return {
    // Attached by the store that routes the events; without one nothing connects.
    host: undefined as SocketHost | undefined,
    ws: shallowRef<PlatformSocket | null>(null),
    isConnected: ref(false),
    // Bumped on every successful reconnect; voice and other views watch it to
    // restore their server-side state.
    reconnectCount: ref(0),
    hadConnection: false,
    // For the connection banner: true once the socket has been up at least once
    // in this session, how many reconnects were tried, and when the next one runs.
    wasConnected: ref(false),
    reconnectAttempt: ref(0),
    nextRetryAt: ref(0),
    socketGeneration: 0,
    reconnectCheckSeq: 0,
    stopPing: undefined as (() => void) | undefined,
    lastPongAt: 0,
    lastPingAt: 0,
    reconnectTimer: undefined as Timer | undefined,
    connectTimer: undefined as Timer | undefined,
    connectStartedAt: 0,
    reconnectDelay: 1000
  }
}

export type SocketState = ReturnType<typeof createSocketState>

interface SocketSession {
  socket: PlatformSocket
  generation: number
  accountId: string
}

/** Stops listening to a socket and closes it: close handshakes never finish on a dead connection. */
function detachSocket(socket: PlatformSocket) {
  socket.onopen = null
  socket.onmessage = null
  socket.onclose = null
  socket.close()
}

function startPingHeartbeat(s: SocketState, session: SocketSession) {
  stopPingHeartbeat(s)
  s.lastPongAt = Date.now()
  s.lastPingAt = 0
  s.stopPing = visibleInterval(() => {
    // Judge the last ping actually sent: throttled timers (hidden tab, sleep) stretch the gap between ticks.
    if (s.lastPingAt > s.lastPongAt && Date.now() - s.lastPingAt > PONG_TIMEOUT_MS) {
      // Close handshakes never finish on a half-open connection: drop the socket and reconnect now.
      detachSocket(session.socket)
      handleSocketClose(s, session)
      return
    }
    const now = Date.now()
    // Keep the oldest unanswered ping as the reference so the wait keeps growing until a pong arrives.
    if (s.lastPingAt <= s.lastPongAt) s.lastPingAt = now
    sendSocketEvent(s, 'ping', { t: now })
  }, PING_INTERVAL_MS, { hiddenMs: PING_HIDDEN_INTERVAL_MS, catchUp: false })
}

function stopPingHeartbeat(s: SocketState) {
  s.stopPing?.()
  s.stopPing = undefined
}

async function attemptReconnect(s: SocketState) {
  const accountId = s.host?.accountId()
  if (!accountId) return
  const generation = s.socketGeneration
  const check = ++s.reconnectCheckSeq
  const signedIn = await s.host?.checkAuth()
  if (generation !== s.socketGeneration || check !== s.reconnectCheckSeq || s.host?.accountId() !== accountId) return
  if (signedIn === true) openSocket(s)
  else if (signedIn === null) scheduleReconnect(s) // server unreachable: keep trying
  // false: the session is gone; the login screen takes over.
}

function scheduleReconnect(s: SocketState) {
  clearTimeout(s.reconnectTimer)
  s.reconnectAttempt.value++
  s.nextRetryAt.value = Date.now() + s.reconnectDelay
  s.reconnectTimer = setTimeout(() => attemptReconnect(s), s.reconnectDelay)
  s.reconnectDelay = Math.min(s.reconnectDelay * 2, 30000)
}

/** The member asked for it: skip the wait and reconnect immediately. */
export function retrySocketNow(s: SocketState) {
  reconnectNow(s, true)
}

/**
 * Visibility and network events reconnect too, but must not restart a connect
 * that is likely still going well: a socket that has been connecting for less
 * than half the connect timeout is left alone. The user's own request always wins.
 */
function reconnectNow(s: SocketState, replaceConnecting: boolean) {
  if (s.isConnected.value) return
  if (s.ws.value) {
    if (!replaceConnecting && Date.now() - s.connectStartedAt < CONNECT_TIMEOUT_MS / 2) return
    // Not connected, yet a socket is set: it is still connecting and stuck, or the caller would not be asking. Replace it.
    clearTimeout(s.connectTimer)
    ++s.socketGeneration
    detachSocket(s.ws.value)
    s.ws.value = null
  }
  clearTimeout(s.reconnectTimer)
  s.nextRetryAt.value = Date.now()
  attemptReconnect(s)
}

function isCurrentSession(s: SocketState, session: SocketSession): boolean {
  return s.ws.value === session.socket && s.socketGeneration === session.generation && s.host?.accountId() === session.accountId
}

function handleSocketOpen(s: SocketState, session: SocketSession) {
  if (!isCurrentSession(s, session)) return
  clearTimeout(s.connectTimer)
  s.isConnected.value = true
  s.wasConnected.value = true
  s.reconnectAttempt.value = 0
  s.nextRetryAt.value = 0
  s.reconnectDelay = 1000
  startPingHeartbeat(s, session)
  sendSocketEvent(s, 'ping', { t: Date.now() })
  s.host?.connected()
  if (s.hadConnection) s.reconnectCount.value++
  s.host?.resync()
  s.hadConnection = true
}

function handleSocketClose(s: SocketState, session: SocketSession, event?: { code?: number }) {
  if (s.ws.value !== session.socket || s.socketGeneration !== session.generation) return
  s.host?.dropped()
  s.isConnected.value = false
  clearTimeout(s.connectTimer)
  stopPingHeartbeat(s)
  s.ws.value = null
  if (s.host?.accountId() !== session.accountId) return

  // The server closed this tab for a newer one of the same account (too
  // many connections). Reconnecting at once would evict another tab in
  // turn; this one reconnects when it is shown again (visibilitychange).
  if (event?.code === 4001) {
    s.reconnectAttempt.value = 0
    s.nextRetryAt.value = 0
    return
  }
  // Exponential backoff; attemptReconnect checks the session first and ends the loop when it is gone.
  scheduleReconnect(s)
}

export function openSocket(s: SocketState) {
  const host = s.host
  const accountId = host?.accountId()
  if (s.ws.value || !host || !accountId) return
  host.opening()
  const generation = ++s.socketGeneration

  // The session cookie authenticates the upgrade; no token in the URL.
  // The trusted desktop client cannot reach the server from its own origin:
  // its host opens the socket (doc/desktop-trusted-client.md).
  const socket = platform.openSocket(host.url())
  const session: SocketSession = { socket, generation, accountId }

  socket.onopen = () => handleSocketOpen(s, session)
  socket.onmessage = (event: { data: string }) => { if (isCurrentSession(s, session)) s.host?.message(event.data) }
  socket.onclose = (event?: { code?: number }) => handleSocketClose(s, session, event)

  s.ws.value = socket
  s.connectStartedAt = Date.now()
  clearTimeout(s.connectTimer)
  s.connectTimer = setTimeout(() => {
    if (!isCurrentSession(s, session) || s.isConnected.value) return
    detachSocket(socket)
    handleSocketClose(s, session)
  }, CONNECT_TIMEOUT_MS)
}

export function closeSocket(s: SocketState) {
  ++s.socketGeneration
  s.host?.closing()
  s.hadConnection = false
  s.wasConnected.value = false
  s.reconnectAttempt.value = 0
  clearTimeout(s.reconnectTimer)
  clearTimeout(s.connectTimer)
  stopPingHeartbeat(s)
  if (s.ws.value) {
    s.ws.value.onclose = null
    s.ws.value.close()
    s.ws.value = null
  }
  s.isConnected.value = false
}

export function sendSocketEvent<K extends keyof ClientEventPayloads>(s: SocketState, type: K, payload: ClientEventPayloads[K]) {
  if (s.ws.value && s.isConnected.value) {
    s.ws.value.send(JSON.stringify({ type, payload }))
  }
}

/** Reconnects, or asks an open socket whether it still works, when the page is shown again or the network returns. */
export function installSocketRetry(s: SocketState) {
  const wake = () => {
    if (!s.host?.isAuthenticated()) return false
    if (!s.ws.value || !s.isConnected.value) reconnectNow(s, false)
    else sendSocketEvent(s, 'ping', { t: Date.now() })
    return true
  }
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState !== 'visible') return
      if (!wake() && s.isConnected.value) sendSocketEvent(s, 'ping', { t: Date.now() })
    })
  }
  // The network is back: reconnect at once, or find out whether a socket that looks open still works.
  if (typeof window !== 'undefined') window.addEventListener('online', () => { wake() })
}

/**
 * The transport of this session. The chat store attaches itself as the host
 * and keeps exposing the connection state, so its callers do not change.
 */
export const useSocketStore = defineStore('socket', () => {
  const state = createSocketState()
  return {
    isConnected: state.isConnected,
    wasConnected: state.wasConnected,
    reconnectCount: state.reconnectCount,
    reconnectAttempt: state.reconnectAttempt,
    nextRetryAt: state.nextRetryAt,
    /** The transport state itself, for the host that routes its events. */
    transport: () => state,
    open: () => openSocket(state),
    close: () => closeSocket(state),
    retryNow: () => retrySocketNow(state),
    send: <K extends keyof ClientEventPayloads>(type: K, payload: ClientEventPayloads[K]) => sendSocketEvent(state, type, payload)
  }
})
