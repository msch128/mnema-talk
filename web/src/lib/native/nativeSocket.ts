import { t } from '../../i18n'
import {
  encodedLength, isRecord, nativeSession, onRetireSockets, readPort, uuidText, validReply,
  type NativeNotice,
} from './nativeSession'

interface SocketMessage { data: string }
const SERVER_EVENTS = new Set(['server_info', 'system_update', 'pong', 'presence_snapshot', 'presence_update', 'channels_changed', 'member_joined', 'user_update', 'user_stats'])
const sockets = new Set<NativeSocket>()

function socketHandle(body: unknown): string | null {
  if (!isRecord(body)) return null
  const handle = body['handle']
  if (!uuidText(handle)) return null
  return handle
}

function serverEventPayload(payload: unknown): boolean {
  if (!isRecord(payload)) return false
  const type = payload['type']
  if (typeof type !== 'string') return false
  return SERVER_EVENTS.has(type)
}

function parseAction(data: string): unknown {
  try {
    return JSON.parse(data)
  } catch {
    throw new Error('Native socket action unavailable')
  }
}

function socketAction(data: string): { action: Record<string, unknown>; payload: Record<string, unknown> } {
  const action = parseAction(data)
  if (!isRecord(action) || Object.keys(action).length !== 2) throw new Error('Native socket action unavailable')
  const payload = action['payload']
  if (!isRecord(payload)) throw new Error('Native socket action unavailable')
  return { action, payload }
}

function pingPayload(type: unknown, payload: Record<string, unknown>): boolean {
  if (type !== 'ping' || Object.keys(payload).length !== 1) return false
  const tick = payload['t']
  return Number.isSafeInteger(tick) && Number(tick) >= 0
}

function idlePayload(type: unknown, payload: Record<string, unknown>): boolean {
  if (type !== 'presence_idle' || Object.keys(payload).length !== 1) return false
  return typeof payload['idle'] === 'boolean'
}

function knownSocketAction(type: unknown, payload: Record<string, unknown>): boolean {
  const ping = pingPayload(type, payload)
  const idle = idlePayload(type, payload)
  return ping || idle
}

function retireSockets(): void {
  for (const socket of [...sockets]) socket.close()
}

onRetireSockets(retireSockets)

export class NativeSocket {
  readonly OPEN = 1
  readyState = 0
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: SocketMessage) => void) | null = null
  private readonly owner = nativeSession.context
  private readonly generation = nativeSession.profileGeneration
  private readonly expectedProfile = nativeSession.profileIntent
  private readonly expectedAuthentication = nativeSession.authenticationIntent
  private readonly currentPort = readPort()
  private handle: string | null = null
  private sequence = 0
  private pending: NativeNotice[] = []
  private readonly requestId = crypto.randomUUID()
  private retired = false

  constructor() {
    sockets.add(this)
    if (!this.owner || !this.expectedProfile || !this.expectedAuthentication) {
      queueMicrotask(() => { this.close() })
      return
    }
    this.bindChannel()
  }

  private bindChannel(): void {
    const channel = this.currentPort.channel()
    channel.onmessage = notice => { this.receive(notice) }
    void this.currentPort.invoke('native_socket_open', {
      context: this.owner,
      profileIntent: this.expectedProfile,
      authenticationIntent: this.expectedAuthentication,
      requestId: this.requestId,
      onEvent: channel,
    }).then(value => { this.opened(value) }).catch(() => { this.close() })
  }

  private openHandle(value: unknown): string | null {
    if (this.retired || !this.current()) return null
    if (!validReply(value, this.owner) || value.status !== 200) return null
    return socketHandle(value.body)
  }

  private closeAcceptedHandle(value: unknown): void {
    if (!validReply(value, this.owner) || value.status !== 200) return
    const handle = socketHandle(value.body)
    if (!handle) return
    this.invokeSocketClose(handle)
  }

  private opened(value: unknown): void {
    const handle = this.openHandle(value)
    if (!handle) {
      this.closeAcceptedHandle(value)
      this.close()
      return
    }
    this.handle = handle
    this.deliverPending()
  }

  private deliverPending(): void {
    const pending = this.pending
    this.pending = []
    for (const notice of pending) this.receive(notice)
  }

  private current(): boolean {
    if (this.owner !== nativeSession.context || this.generation !== nativeSession.profileGeneration) return false
    return this.expectedProfile === nativeSession.profileIntent && this.expectedAuthentication === nativeSession.authenticationIntent
  }

  private acceptedNotice(notice: NativeNotice): boolean {
    if (!isRecord(notice) || notice.context !== this.owner) return false
    if (!uuidText(notice.handle)) return false
    if (!Number.isSafeInteger(notice.sequence)) return false
    return notice.sequence >= 1
  }

  private queuePending(notice: NativeNotice): boolean {
    if (this.handle) return false
    if (this.pending.length >= 32) {
      this.close()
      return true
    }
    this.pending.push(notice)
    return true
  }

  private consumeOpen(notice: NativeNotice): boolean {
    if (notice.kind !== 'opened' || this.readyState !== 0) return false
    if (notice.payload !== null) return false
    this.readyState = 1
    this.onopen?.()
    return true
  }

  private acceptedMessage(notice: NativeNotice): boolean {
    if (notice.kind !== 'message' || this.readyState !== 1) return false
    return serverEventPayload(notice.payload)
  }

  private deliverMessage(payload: unknown): void {
    const data = JSON.stringify(payload)
    if (encodedLength(data) > 65536) {
      this.close()
      return
    }
    this.onmessage?.({ data })
  }

  private dispatchNotice(notice: NativeNotice): void {
    if (this.consumeOpen(notice)) return
    if (notice.kind === 'closed') {
      this.close()
      return
    }
    if (!this.acceptedMessage(notice)) {
      this.close()
      return
    }
    this.deliverMessage(notice.payload)
  }

  private receive(notice: NativeNotice): void {
    if (this.retired || !this.current()) return
    if (!this.acceptedNotice(notice)) {
      this.close()
      return
    }
    if (this.queuePending(notice)) return
    if (notice.handle !== this.handle) return
    if (notice.sequence <= this.sequence) {
      this.close()
      return
    }
    this.sequence = notice.sequence
    this.dispatchNotice(notice)
  }

  private assertSendable(data: string): void {
    if (this.retired || !this.current()) throw new Error('Native socket unavailable')
    if (this.readyState !== this.OPEN || !this.handle) throw new Error('Native socket unavailable')
    if (encodedLength(data) > 4096) throw new Error('Native socket action unavailable')
  }

  private finishSend(reply: unknown): void {
    if (!validReply(reply, this.owner) || reply.status !== 204) this.close()
  }

  private sendAction(action: Record<string, unknown>): void {
    void this.currentPort.invoke('native_socket_send', {
      context: this.owner,
      profileIntent: this.expectedProfile,
      authenticationIntent: this.expectedAuthentication,
      handle: this.handle,
      action,
    }).then(reply => { this.finishSend(reply) }).catch(() => { this.close() })
  }

  send(data: string): void {
    this.assertSendable(data)
    const parsed = socketAction(data)
    if (!knownSocketAction(parsed.action['type'], parsed.payload)) throw new Error(t('nativeDesktop.featureUnavailable'))
    this.sendAction(parsed.action)
  }

  private invokeSocketClose(handle: string): void {
    void this.currentPort.invoke('native_socket_close', {
      context: this.owner,
      profileIntent: this.expectedProfile,
      authenticationIntent: this.expectedAuthentication,
      handle,
    }).catch(() => {})
  }

  private cancelOpen(): void {
    if (!this.owner || !this.expectedProfile || !this.expectedAuthentication) return
    void this.currentPort.invoke('native_request_cancel', {
      context: this.owner,
      profileIntent: this.expectedProfile,
      authenticationIntent: this.expectedAuthentication,
      requestId: this.requestId,
    }).catch(() => {})
  }

  private releaseSocket(): void {
    if (this.handle) {
      this.invokeSocketClose(this.handle)
      return
    }
    this.cancelOpen()
  }

  close(): void {
    if (this.retired) return
    this.retired = true
    this.readyState = 3
    this.pending = []
    sockets.delete(this)
    this.releaseSocket()
    this.onclose?.()
  }
}
