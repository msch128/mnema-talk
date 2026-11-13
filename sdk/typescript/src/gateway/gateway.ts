// The gateway connection: HELLO, IDENTIFY or RESUME, heartbeats, dispatches
// with their sequence numbers, and reconnects with backoff (doc/bots.md).
import { GatewayOp } from '../constants.ts'
import { Emitter, toError } from '../emitter.ts'
import type { GatewayFrame, PresenceData, Ready } from '../types/gateway.ts'
import type { Logger } from '../util.ts'
import { VERSION } from '../version.ts'
import { Heartbeat } from './heartbeat.ts'
import { Outbox } from './outbox.ts'
import {
  backoffDelay,
  closeAction,
  closeMeaning,
  GatewayError,
  isProtocolError,
  ZOMBIE_CLOSE_CODE,
  type BackoffOptions,
} from './policy.ts'
import { presenceFrame } from './presence.ts'
import { defaultWebSocketFactory, frameText, type WebSocketFactory, type WebSocketLike } from './socket.ts'

export interface GatewayOptions {
  token: string
  /** The sum of the intent bits, or a list of them. */
  intents: number | readonly number[]
  /** The gateway address, or a function that finds it (for example through GET /gateway). */
  url: string | (() => Promise<string>)
  /** The status to start with, sent in IDENTIFY. */
  presence?: PresenceData
  /** IDENTIFY's `properties`. */
  properties?: Record<string, string>
  logger?: Logger
  webSocketFactory?: WebSocketFactory
  reconnect?: BackoffOptions
  /** How long after a drop a RESUME is tried; the server keeps sessions 2 minutes. Default 110000 ms. */
  resumeWindowMs?: number
  /** Randomness of jitter and backoff (tests make it fixed). */
  random?: () => number
}

export interface GatewayCloseInfo {
  code: number
  reason: string
  /** Whether the gateway connects again by itself. */
  reconnect: boolean
}

export type GatewayEvents = {
  ready: [ready: Ready]
  resumed: []
  dispatch: [name: string, data: unknown, seq: number]
  close: [info: GatewayCloseInfo]
  error: [error: Error]
  debug: [message: string]
}

interface Pending {
  promise: Promise<Ready>
  resolve: (r: Ready) => void
  reject: (e: Error) => void
  settled: boolean
}

function intentBits(i: number | readonly number[]): number {
  return typeof i === 'number' ? i : i.reduce((a, b) => a | b, 0)
}

export class Gateway extends Emitter<GatewayEvents> {
  private readonly o: GatewayOptions
  private readonly backoff: Required<BackoffOptions>
  private readonly outbox = new Outbox()
  private readonly heartbeat: Heartbeat
  private ws: WebSocketLike | undefined
  private sessionId: string | undefined
  private resumeUrl: string | undefined
  private seq: number | null = null
  private droppedAt: number | undefined
  private attempts = 0
  /** Counts connect() and close() calls, so a stale open() does nothing. */
  private generation = 0
  private identified = false
  private stopped = true
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined
  private presence: PresenceData | undefined
  private pending: Pending | undefined

  constructor(options: GatewayOptions) {
    super()
    this.o = options
    this.presence = options.presence
    this.backoff = {
      minDelayMs: options.reconnect?.minDelayMs ?? 1000,
      maxDelayMs: options.reconnect?.maxDelayMs ?? 60_000,
      rateLimitedDelayMs: options.reconnect?.rateLimitedDelayMs ?? 30_000,
    }
    this.heartbeat = new Heartbeat(() => this.sendHeartbeat(), () => this.onZombie(), options.random)
  }

  /** The session id of READY, while there is one. */
  get session(): { id: string; seq: number | null } | undefined {
    return this.sessionId ? { id: this.sessionId, seq: this.seq } : undefined
  }

  /** Connects; resolves with the first READY, rejects on a fatal close or close(). */
  connect(): Promise<Ready> {
    if (this.pending) return this.pending.promise
    let resolve!: (r: Ready) => void
    let reject!: (e: Error) => void
    const promise = new Promise<Ready>((res, rej) => {
      resolve = res
      reject = rej
    })
    this.pending = { resolve, reject, promise, settled: false }
    this.stopped = false
    this.generation++
    void this.open()
    return promise
  }

  /** Closes the connection for good: no reconnect, no timer stays. */
  close(code = 1000): Promise<void> {
    this.stopped = true
    this.generation++
    clearTimeout(this.reconnectTimer)
    this.reconnectTimer = undefined
    this.heartbeat.stop()
    this.outbox.reset()
    this.settle(new Error('the gateway was closed'))
    const ws = this.ws
    this.ws = undefined
    if (!ws) return Promise.resolve()
    this.emit('close', { code, reason: 'closed by the bot', reconnect: false })
    if (ws.readyState >= 2) return Promise.resolve()
    return new Promise((resolve) => {
      const done = setTimeout(resolve, 2000)
      ws.addEventListener('close', () => {
        clearTimeout(done)
        resolve()
      })
      ws.close(code)
    })
  }

  /** Sets the bot's status and activity (op 3); kept for the IDENTIFY of later connections. */
  setPresence(presence: PresenceData): void {
    const frame = presenceFrame(presence)
    this.presence = presence
    if (this.identified) this.outbox.send({ op: GatewayOp.PresenceUpdate, d: frame })
  }

  /** Sends a raw frame, within the frame size and rate limits. */
  send(op: number, d: unknown): void {
    this.outbox.send({ op, d })
  }

  protected override onListenerError(err: unknown, event: string): void {
    if (event !== 'error' && this.listenerCount('error') > 0) this.emit('error', toError(err))
    else super.onListenerError(err, event)
  }

  /** Emits an error, or logs it when nobody listens. */
  private report(err: Error): void {
    if (this.listenerCount('error') > 0) this.emit('error', err)
    else this.logError(err)
  }

  private debug(message: string): void {
    this.o.logger?.debug?.(message)
    this.emit('debug', message)
  }

  private async open(): Promise<void> {
    const generation = this.generation
    let ws: WebSocketLike
    try {
      const url = this.canResume() && this.resumeUrl ? this.resumeUrl : await this.baseUrl()
      if (this.stopped || generation !== this.generation) return
      this.debug(`connecting to ${url}`)
      ws = (this.o.webSocketFactory ?? defaultWebSocketFactory)(url, { Authorization: `Bot ${this.o.token}` })
    } catch (err) {
      if (this.stopped || generation !== this.generation) return
      this.report(toError(err))
      this.scheduleReconnect('resume')
      return
    }
    this.ws = ws
    ws.addEventListener('message', (ev) => {
      if (this.ws === ws) this.onFrame(frameText(ev.data))
    })
    ws.addEventListener('close', (ev) => {
      if (this.ws === ws) this.onClose(ev.code, ev.reason)
    })
    ws.addEventListener('error', () => this.debug('websocket error'))
  }

  private async baseUrl(): Promise<string> {
    return typeof this.o.url === 'string' ? this.o.url : this.o.url()
  }

  private canResume(): boolean {
    if (!this.sessionId || this.seq === null) return false
    const since = this.droppedAt === undefined ? 0 : Date.now() - this.droppedAt
    return since < (this.o.resumeWindowMs ?? 110_000)
  }

  // Nothing the server sends may throw out of the socket's listener: Node
  // rethrows listener errors and the bot's process would end. A frame that is
  // not an object with a numeric op is dropped; a HELLO or READY without a
  // usable payload drops the connection, which then resumes or identifies.
  private onFrame(text: string): void {
    let f: unknown
    try {
      f = JSON.parse(text)
    } catch {
      this.debug('undecodable frame from the gateway')
      return
    }
    if (!isRecord(f) || typeof f.op !== 'number') {
      this.debug('frame without an op from the gateway')
      return
    }
    try {
      this.handleFrame(f as unknown as GatewayFrame)
    } catch (err) {
      this.report(toError(err))
    }
  }

  private handleFrame(f: GatewayFrame): void {
    if (typeof f.s === 'number') this.seq = f.s
    switch (f.op) {
      case GatewayOp.Hello:
        return this.onHello(f.d)
      case GatewayOp.HeartbeatAck:
        return this.heartbeat.ack()
      case GatewayOp.Heartbeat:
        return this.heartbeat.now()
      case GatewayOp.InvalidSession:
        return this.onInvalidSession()
      case GatewayOp.Reconnect:
        return this.dropConnection(ZOMBIE_CLOSE_CODE, 'server asked to reconnect')
      case GatewayOp.Dispatch:
        return this.onDispatch(typeof f.t === 'string' ? f.t : '', f.d, typeof f.s === 'number' ? f.s : 0)
    }
  }

  private onHello(d: unknown): void {
    const ws = this.ws
    if (!ws) return
    const interval = heartbeatInterval(d)
    if (interval === undefined) {
      this.dropConnection(ZOMBIE_CLOSE_CODE, 'HELLO without a heartbeat interval')
      return
    }
    this.outbox.attach((text) => ws.send(text))
    this.heartbeat.start(interval)
    if (this.canResume()) this.sendResume()
    else this.sendIdentify()
  }

  private sendIdentify(): void {
    this.sessionId = undefined
    this.resumeUrl = undefined
    this.seq = null
    this.debug('identifying')
    const d: Record<string, unknown> = {
      token: this.o.token,
      intents: intentBits(this.o.intents),
      properties: this.o.properties ?? { os: process.platform, browser: `mnema-talk-sdk/${VERSION}`, device: 'mnema-talk-sdk' },
    }
    if (this.presence) d.presence = presenceFrame(this.presence)
    this.outbox.send({ op: GatewayOp.Identify, d })
  }

  private sendResume(): void {
    this.debug(`resuming session ${this.sessionId} at seq ${this.seq}`)
    this.outbox.send({ op: GatewayOp.Resume, d: { token: this.o.token, session_id: this.sessionId, seq: this.seq } })
  }

  private sendHeartbeat(): void {
    try {
      this.outbox.send({ op: GatewayOp.Heartbeat, d: this.seq }, true)
    } catch (err) {
      this.report(toError(err))
    }
  }

  private onInvalidSession(): void {
    this.debug('session invalid: identifying again')
    this.identified = false
    this.sendIdentify()
  }

  private onDispatch(name: string, data: unknown, seq: number): void {
    if (name === 'READY') {
      if (!isRecord(data) || typeof data.session_id !== 'string') {
        this.dropConnection(ZOMBIE_CLOSE_CODE, 'READY without a session')
        return
      }
      this.onReady(data as unknown as Ready)
    }
    else if (name === 'RESUMED') this.onResumed()
    this.emit('dispatch', name, data, seq)
  }

  private onReady(ready: Ready): void {
    this.sessionId = ready.session_id
    this.resumeUrl = ready.resume_gateway_url || undefined
    this.markAlive()
    if (this.pending && !this.pending.settled) {
      this.pending.settled = true
      this.pending.resolve(ready)
    }
    this.emit('ready', ready)
  }

  private onResumed(): void {
    this.markAlive()
    this.debug('resumed')
    this.emit('resumed')
  }

  private markAlive(): void {
    this.identified = true
    this.attempts = 0
    this.droppedAt = undefined
  }

  private onZombie(): void {
    this.dropConnection(ZOMBIE_CLOSE_CODE, closeMeaning(ZOMBIE_CLOSE_CODE))
  }

  /** Leaves a connection without waiting for its close handshake. */
  private dropConnection(code: number, reason: string): void {
    const ws = this.ws
    this.ws = undefined
    try {
      ws?.close(code, reason)
    } catch {
      // Closing an already closing socket throws in some implementations.
    }
    this.onClose(code, reason)
  }

  private onClose(code: number, reason: string): void {
    this.ws = undefined
    this.identified = false
    this.heartbeat.stop()
    this.outbox.reset()
    if (this.stopped) return
    const action = closeAction(code)
    const meaning = reason || closeMeaning(code)
    this.debug(`closed with ${code} (${meaning}): ${action}`)
    this.emit('close', { code, reason: meaning, reconnect: action !== 'fatal' })
    if (action === 'fatal') return this.fail(new GatewayError(code, reason))
    if (isProtocolError(code)) this.report(new GatewayError(code, reason))
    this.droppedAt ??= Date.now()
    this.scheduleReconnect(action)
  }

  private fail(err: GatewayError): void {
    this.stopped = true
    this.sessionId = undefined
    const awaited = this.pending !== undefined && !this.pending.settled
    if (this.listenerCount('error') > 0 || !awaited) this.report(err)
    this.settle(err)
  }

  private logError(err: Error): void {
    if (this.o.logger?.error) this.o.logger.error(err.message)
    else console.error(`mnema-talk-sdk: ${err.message}`)
  }

  /** Rejects a pending connect(); a settled one stays as it is. */
  private settle(err: Error): void {
    const p = this.pending
    this.pending = undefined
    p?.promise.catch(() => {})
    p?.reject(err)
  }

  private scheduleReconnect(action: 'identify' | 'resume' | 'rateLimited'): void {
    if (action === 'identify') this.sessionId = undefined
    let delay = backoffDelay(this.attempts, this.backoff, this.o.random)
    if (action === 'rateLimited') delay = Math.max(delay, this.backoff.rateLimitedDelayMs)
    this.attempts++
    this.debug(`reconnecting in ${delay} ms`)
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = undefined
      if (!this.stopped) void this.open()
    }, delay)
  }
}

// Bounds of the heartbeat interval the SDK accepts from HELLO: a server that
// says 0 must not make it beat in a tight loop, one that says a day must not
// leave a dead connection undetected.
const MIN_HEARTBEAT_MS = 1_000
const MAX_HEARTBEAT_MS = 300_000

/** The HELLO interval clamped to sane bounds, or undefined when there is none. */
export function heartbeatInterval(d: unknown): number | undefined {
  if (!isRecord(d) || typeof d.heartbeat_interval !== 'number' || !Number.isFinite(d.heartbeat_interval)) return undefined
  return Math.min(MAX_HEARTBEAT_MS, Math.max(MIN_HEARTBEAT_MS, d.heartbeat_interval))
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}
