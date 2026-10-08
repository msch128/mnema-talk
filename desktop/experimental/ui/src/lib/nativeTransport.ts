import { invoke, Channel } from '@tauri-apps/api/core'
import type { ApiTransport } from './api'
import { createTrustPort, type StatusChannel } from './trustPort'
import { createNativeChatPresentation } from './nativeChat'
import { NativeChatDisplayCache } from './nativeChatCache'

export interface NativeReply { context: string; status: number; body: unknown }
export type NativeInvoke = (command: string, args?: Record<string, unknown>) => Promise<unknown>
export interface NativeNotice { context: string; handle: string; sequence: number; kind: 'opened' | 'message' | 'closed'; payload: unknown }
export interface NativeChannel { onmessage: (notice: NativeNotice) => void }
export interface NativePort { invoke: NativeInvoke; channel: () => NativeChannel; trustChannel?: () => StatusChannel; chatChannel?: () => StatusChannel }
const realPort: NativePort = { invoke, channel: () => new Channel<NativeNotice>(), trustChannel: () => new Channel<unknown>(), chatChannel: () => new Channel<unknown>() }
let port: NativePort = realPort
let context: string | null = null
let profileIntent: string | null = null
let profileGeneration = 0
let authenticationRevision = 0
const chatDisplayCache = new NativeChatDisplayCache()
const sockets = new Set<NativeSocket>()
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const cancelled = () => new DOMException('Native operation cancelled', 'AbortError')
const unavailable = () => ({ status: 503, body: { error: { code: 'UNAVAILABLE', message: 'Diese Funktion ist in dieser Testversion noch nicht verfügbar.' } } })
function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function validReply(value: unknown, expected: string): value is NativeReply {
  return object(value) && value['context'] === expected && Number.isInteger(value['status']) && Number(value['status']) >= 100 && Number(value['status']) <= 599
}
function retireSockets() { for (const socket of [...sockets]) socket.close() }
export function installNativePort(next: NativePort) {
  chatDisplayCache.clear(); retireSockets(); profileGeneration++; context = null; profileIntent = null; port = next
}
export async function initializeNativeContext(): Promise<void> {
  const generation = profileGeneration
  const reply = await port.invoke('native_context')
  if (generation !== profileGeneration) throw cancelled()
  if (!object(reply) || typeof reply['context'] !== 'string' || !UUID.test(reply['context']) || reply['content_authorization'] !== 'unavailable' || reply['remembered_login'] !== false) throw new Error('Native context unavailable')
  if (reply['profile_intent'] !== undefined && reply['profile_intent'] !== null && (typeof reply['profile_intent'] !== 'string' || !UUID.test(reply['profile_intent']) || reply['profile_intent'] === '00000000-0000-0000-0000-000000000000')) throw new Error('Invalid native profile intent')
  context = reply['context']
  profileIntent = typeof reply['profile_intent'] === 'string' ? reply['profile_intent'] : null
}
async function call(command: string, payload: Record<string, unknown> = {}, signal?: AbortSignal, selected = true): Promise<NativeReply> {
  const expectedProfile = profileIntent
  const owner = context; const generation = profileGeneration; const currentPort = port
  if (!owner) throw new Error('Native context unavailable')
  if (selected && !expectedProfile) throw new Error('Native profile unavailable')
  if (signal?.aborted) throw cancelled()
  const requestId = crypto.randomUUID()
  const abort = () => { void currentPort.invoke('native_request_cancel', { context: owner, profileIntent: expectedProfile, requestId }).catch(() => {}) }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    const value = await currentPort.invoke(command, { context: owner, requestId, ...payload, ...(selected ? { profileIntent: expectedProfile } : {}) })
    if (signal?.aborted || context !== owner || profileGeneration !== generation || (selected && profileIntent !== expectedProfile)) throw cancelled()
    if (!validReply(value, owner)) throw new Error('Invalid native reply')
    if (value.status === 499) throw cancelled()
    return value
  } finally { signal?.removeEventListener('abort', abort) }
}
export async function connectNative(address: string, signal?: AbortSignal): Promise<NativeReply> {
  if (new TextEncoder().encode(address).length > 2048) throw new Error('Serveradresse zu lang')
  if (profileIntent === null) {
    // A lost Connect response may have selected a native profile. Read its
    // public comparator for a new explicit connection attempt, never for login.
    const owner = context; const generation = profileGeneration; const captured = port
    const state = await captured.invoke('native_context')
    if (context !== owner || generation !== profileGeneration || captured !== port) throw cancelled()
    if (!object(state) || state['context'] !== owner || state['content_authorization'] !== 'unavailable' || state['remembered_login'] !== false || (state['profile_intent'] !== null && (typeof state['profile_intent'] !== 'string' || !UUID.test(state['profile_intent']) || state['profile_intent'] === '00000000-0000-0000-0000-000000000000'))) throw new Error('Native profile descriptor unavailable')
    profileIntent = typeof state['profile_intent'] === 'string' ? state['profile_intent'] : null
  }
  const previous = profileIntent
  profileIntent = null; chatDisplayCache.clear(); profileGeneration++; retireSockets()
  const reply = await call('native_connect', { address, profileIntent: previous }, signal, false)
  if (reply.status === 200) {
    if (!object(reply.body) || typeof reply.body['profile_intent'] !== 'string' || !UUID.test(reply.body['profile_intent']) || reply.body['profile_intent'] === '00000000-0000-0000-0000-000000000000') throw new Error('Invalid native profile intent')
    profileIntent = reply.body['profile_intent']
  }
  return reply
}
export async function disconnectNative(): Promise<void> {
  const expectedProfile = profileIntent
  profileIntent = null; chatDisplayCache.clear(); profileGeneration++; retireSockets()
  const owner = context
  if (!owner) return
  const value = await port.invoke('native_disconnect', { context: owner, profileIntent: expectedProfile })
  if (!validReply(value, owner) || value.status !== 204) throw new Error('Native disconnect unavailable')
}
export const nativeApiTransport: ApiTransport = async (path, request) => {
  if (request.form) return unavailable()
  if (path === '/api/auth/login' && request.method === 'POST' && object(request.json)) {
    const { username, password } = request.json
    if (typeof username !== 'string' || typeof password !== 'string' || Object.keys(request.json).length !== 2) return unavailable()
    chatDisplayCache.clear(); authenticationRevision++; retireSockets()
    const reply = await call('native_auth_login', { username, password }, request.signal)
    return { status: reply.status, body: reply.body }
  }
  if (path === '/api/auth/me' && request.method === 'GET') {
    const reply = await call('native_auth_me', {}, request.signal); return { status: reply.status, body: reply.body }
  }
  if (path === '/api/auth/logout' && request.method === 'POST') {
    chatDisplayCache.clear(); authenticationRevision++; retireSockets()
    const reply = await call('native_auth_logout', {}, request.signal); return { status: reply.status, body: reply.body }
  }
  let personal: Record<string, unknown> | null = null
  const userPath = /^\/api\/users\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/.exec(path)
  if (userPath?.[1] && request.method === 'GET' && request.json === undefined) personal = { operation: 'user', user_id: userPath[1] }
  if (request.method === 'PUT' && object(request.json)) {
    const input = request.json
    const keys = Object.keys(input)
    const text = (name: string, maximum: number) => typeof input[name] === 'string' && Array.from(input[name]).length <= maximum
    if (path === '/api/users/me/profile' && keys.length === 2 && text('display_name', 24) && text('bio', 250)) personal = { operation: 'profile', display_name: input['display_name'], bio: input['bio'] }
    if (path === '/api/users/me/locale' && keys.length === 1 && (input['locale'] === 'de' || input['locale'] === 'en')) personal = { operation: 'locale', locale: input['locale'] }
    if (path === '/api/users/me/presence' && keys.length === 1 && typeof input['presence'] === 'string' && ['online', 'away', 'dnd', 'focus'].includes(input['presence'])) personal = { operation: 'presence', presence: input['presence'] }
    if (path === '/api/users/me/status' && keys.length === 1 && text('status_text', 32)) personal = { operation: 'status', status_text: input['status_text'] }
  }
  if (personal) {
    const reply = await call('native_personal_metadata_request', { input: personal }, request.signal)
    return { status: reply.status, body: reply.body }
  }
  const metadata: Readonly<Record<string, string>> = {
    '/api/channels': 'channels', '/api/members': 'members',
    '/api/read-state': 'read_state', '/api/health': 'health', '/api/legal': 'legal'
  }
  const resource = metadata[path]
  if (resource && request.method === 'GET' && request.json === undefined) {
    const command = resource === 'legal' || resource === 'health' ? 'native_public_metadata_request' : 'native_metadata_request'
    const reply = await call(command, { resource }, request.signal)
    return { status: reply.status, body: reply.body }
  }
  return unavailable()
}

interface SocketMessage { data: string }
const SERVER_EVENTS = new Set(['server_info', 'system_update', 'pong', 'presence_snapshot', 'presence_update', 'channels_changed', 'member_joined', 'user_update', 'user_stats'])
export class NativeSocket {
  readonly OPEN = 1
  readyState = 0
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: SocketMessage) => void) | null = null
  private readonly owner = context
  private readonly generation = profileGeneration
  private readonly expectedProfile = profileIntent
  private readonly currentPort = port
  private handle: string | null = null
  private sequence = 0
  private pending: NativeNotice[] = []
  private readonly requestId = crypto.randomUUID()
  private retired = false
  constructor() {
    sockets.add(this)
    if (!this.owner || !this.expectedProfile) { queueMicrotask(() => this.close()); return }
    const channel = this.currentPort.channel()
    channel.onmessage = notice => this.receive(notice)
    void this.currentPort.invoke('native_socket_open', { context: this.owner, profileIntent: this.expectedProfile, requestId: this.requestId, onEvent: channel }).then(value => {
      if (this.retired || !this.current() || !validReply(value, this.owner!) || value.status !== 200 || !object(value.body) || typeof value.body['handle'] !== 'string' || !UUID.test(value.body['handle'])) {
        if (validReply(value, this.owner!) && value.status === 200 && object(value.body) && typeof value.body['handle'] === 'string' && UUID.test(value.body['handle'])) void this.currentPort.invoke('native_socket_close', { context: this.owner, profileIntent: this.expectedProfile, handle: value.body['handle'] }).catch(() => {})
        this.close(); return
      }
      this.handle = value.body['handle']
      const pending = this.pending; this.pending = []
      for (const notice of pending) this.receive(notice)
    }).catch(() => this.close())
  }
  private current(): boolean { return this.owner === context && this.generation === profileGeneration && this.expectedProfile === profileIntent }
  private receive(notice: NativeNotice) {
    if (this.retired || !this.current()) return
    if (!object(notice) || notice.context !== this.owner || typeof notice.handle !== 'string' || !UUID.test(notice.handle) || !Number.isSafeInteger(notice.sequence) || notice.sequence < 1) { this.close(); return }
    if (!this.handle) {
      if (this.pending.length >= 32) { this.close(); return }
      this.pending.push(notice); return
    }
    if (notice.handle !== this.handle) return
    if (notice.sequence <= this.sequence) { this.close(); return }
    this.sequence = notice.sequence
    if (notice.kind === 'opened' && this.readyState === 0 && notice.payload === null) { this.readyState = 1; this.onopen?.(); return }
    if (notice.kind === 'closed') { this.close(); return }
    if (notice.kind !== 'message' || this.readyState !== 1 || !object(notice.payload) || typeof notice.payload['type'] !== 'string' || !SERVER_EVENTS.has(notice.payload['type'])) { this.close(); return }
    const data = JSON.stringify(notice.payload)
    if (new TextEncoder().encode(data).length > 65536) { this.close(); return }
    this.onmessage?.({ data })
  }
  send(data: string) {
    if (this.retired || !this.current() || this.readyState !== this.OPEN || !this.handle) throw new Error('Native socket unavailable')
    if (new TextEncoder().encode(data).length > 4096) throw new Error('Native socket action unavailable')
    let action: unknown
    try { action = JSON.parse(data) } catch { throw new Error('Native socket action unavailable') }
    if (!object(action) || Object.keys(action).length !== 2 || !object(action['payload'])) throw new Error('Native socket action unavailable')
    const payload = action['payload']
    const ping = action['type'] === 'ping' && Object.keys(payload).length === 1 && Number.isSafeInteger(payload['t']) && Number(payload['t']) >= 0
    const idle = action['type'] === 'presence_idle' && Object.keys(payload).length === 1 && typeof payload['idle'] === 'boolean'
    if (!ping && !idle) throw new Error('Diese Funktion ist in dieser Testversion noch nicht verfügbar.')
    void this.currentPort.invoke('native_socket_send', { context: this.owner, profileIntent: this.expectedProfile, handle: this.handle, action }).then(reply => {
      if (!validReply(reply, this.owner!) || reply.status !== 204) this.close()
    }).catch(() => this.close())
  }
  close() {
    if (this.retired) return
    this.retired = true; this.readyState = 3; this.pending = []; sockets.delete(this)
    if (this.handle) void this.currentPort.invoke('native_socket_close', { context: this.owner, profileIntent: this.expectedProfile, handle: this.handle }).catch(() => {})
    else if (this.owner && this.expectedProfile) void this.currentPort.invoke('native_request_cancel', { context: this.owner, profileIntent: this.expectedProfile, requestId: this.requestId }).catch(() => {})
    this.onclose?.()
  }
}

/** Public presentation captured once; native scope and OS dialog grant trust. */
export function captureNativeTrustPresentation() {
  const owner = context; const expectedProfile = profileIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
  if (!owner || !expectedProfile || !captured.trustChannel) throw new Error('Native trust presentation unavailable')
  return createTrustPort({
    context: owner,
    isCurrent: () => context === owner && expectedProfile === profileIntent && generation === profileGeneration && revision === authenticationRevision && captured === port,
    invoke: (command, args) => captured.invoke(command, { ...args, profileIntent: expectedProfile }),
    channel: captured.trustChannel
  })
}

export function captureNativeChatPresentation(channelId: string, accountId: string) {
  const owner = context; const expectedProfile = profileIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
  if (!owner || !expectedProfile || !captured.chatChannel) throw new Error('Native chat presentation unavailable')
  return createNativeChatPresentation({
    context: owner,
    isCurrent: () => context === owner && expectedProfile === profileIntent && generation === profileGeneration && revision === authenticationRevision && captured === port,
    invoke: (command, args) => captured.invoke(command, { ...args, profileIntent: expectedProfile }),
    channel: captured.chatChannel
  }, channelId, accountId, chatDisplayCache.scope(`${owner}:${generation}:${revision}:${accountId}:${channelId}`, channelId))
}
