import { t } from '../i18n'
import { invoke, Channel } from '@tauri-apps/api/core'
import type { ApiTransport } from './api'
import { createTrustPort, type StatusChannel } from './trustPort'
import { mapAdminOperation } from './nativeAdmin'
import { registerThenLogin } from './nativeRegistration'
import { NativeTypedChatApi, isNativeTypedChatPath } from './nativeChatApi'

export interface NativeReply { context: string; status: number; body: unknown }
export type NativeInvoke = (command: string, args?: Record<string, unknown>) => Promise<unknown>
export interface NativeNotice { context: string; handle: string; sequence: number; kind: 'opened' | 'message' | 'closed'; payload: unknown }
export interface NativeChannel { onmessage: (notice: NativeNotice) => void }
export interface NativePort { invoke: NativeInvoke; channel: () => NativeChannel; trustChannel?: () => StatusChannel; chatChannel?: () => StatusChannel }
const realPort: NativePort = { invoke, channel: () => new Channel<NativeNotice>(), trustChannel: () => new Channel<unknown>(), chatChannel: () => new Channel<unknown>() }
let port: NativePort = realPort
let context: string | null = null
let profileIntent: string | null = null
let authenticationIntent: string | null = null
let authenticationUncertain = false
let profileGeneration = 0
let authenticationRevision = 0
const typedChatApi = new NativeTypedChatApi()
const sockets = new Set<NativeSocket>()
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
function canonicalId(value: unknown): value is string { return typeof value === 'string' && UUID.test(value) && value !== '00000000-0000-0000-0000-000000000000' }
const cancelled = () => new DOMException('Native operation cancelled', 'AbortError')
const unavailable = () => ({ status: 503, body: { error: { code: 'UNAVAILABLE', message: t('nativeDesktop.featureUnavailable') } } })
function object(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value) }
function validReply(value: unknown, expected: string): value is NativeReply {
  return object(value) && value['context'] === expected && Number.isInteger(value['status']) && Number(value['status']) >= 100 && Number(value['status']) <= 599
}
function retireSockets() { for (const socket of [...sockets]) socket.close() }
export function installNativePort(next: NativePort) {
  typedChatApi.clear(); retireSockets(); profileGeneration++; context = null; profileIntent = null; authenticationIntent = null; authenticationUncertain = false; port = next
}
export async function initializeNativeContext(): Promise<void> {
  const generation = profileGeneration
  const reply = await port.invoke('native_context')
  if (generation !== profileGeneration) throw cancelled()
  if (!object(reply) || !canonicalId(reply['context']) || reply['content_authorization'] !== 'unavailable' || reply['remembered_login'] !== false) throw new Error('Native context unavailable')
  if (reply['profile_intent'] !== null && !canonicalId(reply['profile_intent'])) throw new Error('Invalid native profile intent')
  if (reply['authentication_intent'] !== null && !canonicalId(reply['authentication_intent'])) throw new Error('Invalid native authentication intent')
  if (reply['profile_intent'] === null && reply['authentication_intent'] !== null) throw new Error('Invalid native authentication profile')
  if (context !== reply['context'] || profileIntent !== reply['profile_intent'] || authenticationIntent !== reply['authentication_intent']) { typedChatApi.clear(); authenticationRevision++; retireSockets() }
  authenticationIntent = typeof reply['authentication_intent'] === 'string' ? reply['authentication_intent'] : null
  authenticationUncertain = false
  context = reply['context']
  profileIntent = typeof reply['profile_intent'] === 'string' ? reply['profile_intent'] : null
}
async function recoverAuthenticationForExplicitLogin(signal?: AbortSignal) {
  if (!authenticationUncertain) return
  const owner = context; const profile = profileIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
  if (!owner || !profile || signal?.aborted) throw cancelled()
  const value = await captured.invoke('native_context')
  if (context !== owner || profileIntent !== profile || profileGeneration !== generation || authenticationRevision !== revision || port !== captured || signal?.aborted) throw cancelled()
  if (!object(value) || value['context'] !== owner || value['profile_intent'] !== profile || value['content_authorization'] !== 'unavailable' || value['remembered_login'] !== false || (value['authentication_intent'] !== null && !canonicalId(value['authentication_intent']))) throw new Error('Native authentication descriptor unavailable')
  // Only a new explicit login can recover a comparison selector. No password,
  // protected operation, or grant from an uncertain request is replayed here.
  authenticationIntent = typeof value['authentication_intent'] === 'string' ? value['authentication_intent'] : null
  authenticationUncertain = false
}
async function call(command: string, payload: Record<string, unknown> = {}, signal?: AbortSignal, selected = true): Promise<NativeReply> {
  const expectedProfile = profileIntent; const expectedAuthentication = authenticationIntent
  const owner = context; const generation = profileGeneration; const revision = authenticationRevision; const currentPort = port
  if (!owner) throw new Error('Native context unavailable')
  if (selected && !expectedProfile) throw new Error('Native profile unavailable')
  if (signal?.aborted) throw cancelled()
  const requestId = crypto.randomUUID()
  const abort = () => { void currentPort.invoke('native_request_cancel', { context: owner, profileIntent: expectedProfile, authenticationIntent: expectedAuthentication, requestId }).catch(() => {}) }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    const value = await currentPort.invoke(command, { context: owner, requestId, ...payload, ...(selected ? { profileIntent: expectedProfile, authenticationIntent: expectedAuthentication } : {}) })
    if (signal?.aborted || context !== owner || profileGeneration !== generation || port !== currentPort || authenticationRevision !== revision || (selected && (profileIntent !== expectedProfile || authenticationIntent !== expectedAuthentication))) throw cancelled()
    if (!validReply(value, owner)) throw new Error('Invalid native reply')
    if (value.status === 499) throw cancelled()
    return value
  } finally { signal?.removeEventListener('abort', abort) }
}
export async function connectNative(address: string, signal?: AbortSignal): Promise<NativeReply> {
  if (new TextEncoder().encode(address).length > 2048) throw new Error(t('nativeDesktop.addressTooLong'))
  if (profileIntent === null) {
    // A lost Connect response may have selected a native profile. Read its
    // public comparator for a new explicit connection attempt, never for login.
    const owner = context; const generation = profileGeneration; const captured = port
    const state = await captured.invoke('native_context')
    if (context !== owner || generation !== profileGeneration || captured !== port) throw cancelled()
    if (!object(state) || state['context'] !== owner || state['content_authorization'] !== 'unavailable' || state['remembered_login'] !== false || (state['profile_intent'] !== null && (typeof state['profile_intent'] !== 'string' || !UUID.test(state['profile_intent']) || state['profile_intent'] === '00000000-0000-0000-0000-000000000000'))) throw new Error('Native profile descriptor unavailable')
    if (state['authentication_intent'] !== null && !canonicalId(state['authentication_intent'])) throw new Error('Native authentication descriptor unavailable')
    profileIntent = typeof state['profile_intent'] === 'string' ? state['profile_intent'] : null
    authenticationIntent = typeof state['authentication_intent'] === 'string' ? state['authentication_intent'] : null
  }
  const previous = profileIntent; const previousAuthentication = authenticationIntent
  profileIntent = null; authenticationIntent = null; authenticationUncertain = false; typedChatApi.clear(); profileGeneration++; retireSockets()
  const reply = await call('native_connect', { address, profileIntent: previous, authenticationIntent: previousAuthentication }, signal, false)
  if (reply.status === 200) {
    if (!object(reply.body) || typeof reply.body['profile_intent'] !== 'string' || !UUID.test(reply.body['profile_intent']) || reply.body['profile_intent'] === '00000000-0000-0000-0000-000000000000') throw new Error('Invalid native profile intent')
    profileIntent = reply.body['profile_intent']
  }
  return reply
}
export async function disconnectNative(): Promise<void> {
  const expectedProfile = profileIntent; const expectedAuthentication = authenticationIntent
  profileIntent = null; authenticationIntent = null; authenticationUncertain = false; typedChatApi.clear(); profileGeneration++; retireSockets()
  const owner = context
  if (!owner) return
  const value = await port.invoke('native_disconnect', { context: owner, profileIntent: expectedProfile, authenticationIntent: expectedAuthentication })
  if (!validReply(value, owner) || value.status !== 204) throw new Error('Native disconnect unavailable')
}
export const nativeApiTransport: ApiTransport = async (path, request) => {
  if (request.form) return unavailable()
  if (path === '/api/auth/register' && request.method === 'POST' && object(request.json)) {
    const input = request.json
    if (Object.keys(input).length !== 4 || !['username', 'display_name', 'password', 'invite_code'].every(k => typeof input[k] === 'string')) return unavailable()
    const username = input['username']; const displayName = input['display_name']; const password = input['password']; const inviteCode = input['invite_code']
    if (typeof username !== 'string' || typeof displayName !== 'string' || typeof password !== 'string' || typeof inviteCode !== 'string') return unavailable()
    typedChatApi.clear(); authenticationRevision++; retireSockets()
    const owner = context; const selected = profileIntent; const lineage = authenticationIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
    if (!owner || !selected) throw new Error('Native profile unavailable')
    let acceptedLoginIntent: string | null = null
    let loginAttempted = false
    const current = () => context === owner && profileIntent === selected && authenticationIntent === lineage && profileGeneration === generation && authenticationRevision === revision && port === captured && !request.signal?.aborted
    const result = await registerThenLogin({
      isCurrent: current,
      call: async (command, values) => {
        if (!current()) throw cancelled()
        if (command === 'native_auth_login') loginAttempted = true
        const reply = await call(command, command === 'native_auth_register'
          ? { username: values['username'], displayName: values['display_name'], password: values['password'], inviteCode: values['invite_code'] }
          : values, request.signal)
        if (command === 'native_auth_login' && reply.status === 200) {
          if (!object(reply.body) || !canonicalId(reply.body['authentication_intent'])) throw new Error('Invalid native login acknowledgement')
          acceptedLoginIntent = reply.body['authentication_intent']
        }
        return reply
      },
      retireCaptured: async () => {
        if (!current()) throw cancelled()
        // Identity-mismatch cleanup targets exactly the just-acknowledged native
        // login family, not the old unauthenticated selector or a later login.
        if (!acceptedLoginIntent) throw new Error('Native captured session unavailable')
        const value = await captured.invoke('native_auth_logout', { context: owner, profileIntent: selected, authenticationIntent: acceptedLoginIntent, requestId: crypto.randomUUID() })
        if (!validReply(value, owner) || value.status !== 204) throw new Error('Native captured logout unavailable')
      },
      userId: body => {
        if (!object(body) || !object(body['user']) || typeof body['user']['id'] !== 'string' || !UUID.test(body['user']['id']) || body['user']['id'] === '00000000-0000-0000-0000-000000000000') throw new Error('Invalid native user acknowledgement')
        return body['user']['id']
      }
    }, { username, display_name: displayName, password, invite_code: inviteCode })
    if (!current()) throw cancelled()
    if (result.kind === 'authenticated') {
      if (!object(result.reply.body) || !canonicalId(result.reply.body['authentication_intent'])) throw new Error('Invalid native login acknowledgement')
      authenticationIntent = result.reply.body['authentication_intent']
      return { status: result.reply.status, body: result.reply.body }
    }
    if (result.kind === 'rejected') return { status: result.reply.status, body: result.reply.body }
    if (loginAttempted) { authenticationIntent = null; authenticationUncertain = true }
    return { status: 503, body: { error: { code: 'LOGIN_REQUIRED', message: t('nativeDesktop.loginRequired') } } }
  }
  if (path === '/api/auth/password' && request.method === 'PUT' && object(request.json)) {
    const input = request.json
    if (Object.keys(input).length !== 2 || typeof input['current_password'] !== 'string' || typeof input['new_password'] !== 'string') return unavailable()
    typedChatApi.clear(); authenticationRevision++; retireSockets()
    const oldIntent = authenticationIntent; const oldProfile = profileIntent; const oldContext = context; const oldGeneration = profileGeneration; const revision = authenticationRevision
    try {
      const reply = await call('native_auth_password', { currentPassword: input['current_password'], newPassword: input['new_password'] }, request.signal)
      if (reply.status === 200) {
        if (!object(reply.body) || Object.keys(reply.body).length !== 1 || !canonicalId(reply.body['authentication_intent'])) throw new Error('Invalid native password acknowledgement')
        authenticationIntent = reply.body['authentication_intent']
        authenticationUncertain = false
        return { status: 204, body: null }
      }
      authenticationIntent = null
      return { status: reply.status, body: reply.body }
    } catch (error) {
      if (context === oldContext && profileIntent === oldProfile && profileGeneration === oldGeneration && authenticationRevision === revision && authenticationIntent === oldIntent) { authenticationIntent = null; authenticationUncertain = true }
      throw error
    }
  }
  const admin = mapAdminOperation(path, request)
  if (admin) {
    const reply = await call('native_admin_request', { input: admin }, request.signal)
    return { status: reply.status, body: reply.body }
  }
  if (path === '/api/auth/login' && request.method === 'POST' && object(request.json)) {
    const { username, password } = request.json
    if (typeof username !== 'string' || typeof password !== 'string' || Object.keys(request.json).length !== 2) return unavailable()
    typedChatApi.clear(); authenticationRevision++; retireSockets()
    const selectedContext = context; const selectedProfile = profileIntent; const selectedGeneration = profileGeneration; const selectedRevision = authenticationRevision; const selectedPort = port
    if (authenticationUncertain) await recoverAuthenticationForExplicitLogin(request.signal)
    if (context !== selectedContext || profileIntent !== selectedProfile || profileGeneration !== selectedGeneration || authenticationRevision !== selectedRevision || port !== selectedPort) throw cancelled()
    const oldIntent = authenticationIntent; const oldProfile = profileIntent; const oldContext = context; const oldGeneration = profileGeneration; const revision = authenticationRevision
    try {
      const reply = await call('native_auth_login', { username, password }, request.signal)
      if (reply.status === 200) {
        if (!object(reply.body) || !canonicalId(reply.body['authentication_intent'])) throw new Error('Invalid native login acknowledgement')
        authenticationIntent = reply.body['authentication_intent']
        authenticationUncertain = false
      } else authenticationIntent = null
      return { status: reply.status, body: reply.body }
    } catch (error) {
      if (context === oldContext && profileIntent === oldProfile && profileGeneration === oldGeneration && authenticationRevision === revision && authenticationIntent === oldIntent) { authenticationIntent = null; authenticationUncertain = true }
      throw error
    }
  }
  if (path === '/api/auth/me' && request.method === 'GET') {
    const reply = await call('native_auth_me', {}, request.signal); return { status: reply.status, body: reply.body }
  }
  if (path === '/api/auth/logout' && request.method === 'POST') {
    typedChatApi.clear(); authenticationRevision++; retireSockets()
    const owner = context; const profile = profileIntent; const generation = profileGeneration; const revision = authenticationRevision; const intent = authenticationIntent; const captured = port
    const current = () => context === owner && profileIntent === profile && profileGeneration === generation && authenticationRevision === revision && authenticationIntent === intent && port === captured
    try {
      const reply = await call('native_auth_logout', {}, request.signal)
      if (current()) { authenticationIntent = null; authenticationUncertain = reply.status !== 204 }
      return { status: reply.status, body: reply.body }
    } catch (error) {
      // An unknown ACK may follow a committed native logout. Burn only this
      // captured comparison selector; a later explicit Login reads context.
      // Never replay Logout or clear a newer accepted account/session.
      if (current()) { authenticationIntent = null; authenticationUncertain = true }
      throw error
    }
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
  if (isNativeTypedChatPath(path)) {
    const owner = context; const expectedProfile = profileIntent; const expectedAuthentication = authenticationIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
    if (!owner || !expectedProfile || !expectedAuthentication || !captured.chatChannel || authenticationUncertain) return unavailable()
    const current = () => context === owner && profileIntent === expectedProfile && authenticationIntent === expectedAuthentication && profileGeneration === generation && authenticationRevision === revision && port === captured
    const reply = await typedChatApi.handle(path,request,{
      context:owner,isCurrent:current,channel:captured.chatChannel,
      call:(command,args,signal)=>{if(!current())throw cancelled();return call(command,args,signal)},
    })
    return reply ?? unavailable()
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
  private readonly expectedAuthentication = authenticationIntent
  private readonly currentPort = port
  private handle: string | null = null
  private sequence = 0
  private pending: NativeNotice[] = []
  private readonly requestId = crypto.randomUUID()
  private retired = false
  constructor() {
    sockets.add(this)
    if (!this.owner || !this.expectedProfile || !this.expectedAuthentication) { queueMicrotask(() => this.close()); return }
    const channel = this.currentPort.channel()
    channel.onmessage = notice => this.receive(notice)
    void this.currentPort.invoke('native_socket_open', { context: this.owner, profileIntent: this.expectedProfile, authenticationIntent: this.expectedAuthentication, requestId: this.requestId, onEvent: channel }).then(value => {
      if (this.retired || !this.current() || !validReply(value, this.owner!) || value.status !== 200 || !object(value.body) || typeof value.body['handle'] !== 'string' || !UUID.test(value.body['handle'])) {
        if (validReply(value, this.owner!) && value.status === 200 && object(value.body) && typeof value.body['handle'] === 'string' && UUID.test(value.body['handle'])) void this.currentPort.invoke('native_socket_close', { context: this.owner, profileIntent: this.expectedProfile, authenticationIntent: this.expectedAuthentication, handle: value.body['handle'] }).catch(() => {})
        this.close(); return
      }
      this.handle = value.body['handle']
      const pending = this.pending; this.pending = []
      for (const notice of pending) this.receive(notice)
    }).catch(() => this.close())
  }
  private current(): boolean { return this.owner === context && this.generation === profileGeneration && this.expectedProfile === profileIntent && this.expectedAuthentication === authenticationIntent }
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
    if (!ping && !idle) throw new Error(t('nativeDesktop.featureUnavailable'))
    void this.currentPort.invoke('native_socket_send', { context: this.owner, profileIntent: this.expectedProfile, authenticationIntent: this.expectedAuthentication, handle: this.handle, action }).then(reply => {
      if (!validReply(reply, this.owner!) || reply.status !== 204) this.close()
    }).catch(() => this.close())
  }
  close() {
    if (this.retired) return
    this.retired = true; this.readyState = 3; this.pending = []; sockets.delete(this)
    if (this.handle) void this.currentPort.invoke('native_socket_close', { context: this.owner, profileIntent: this.expectedProfile, authenticationIntent: this.expectedAuthentication, handle: this.handle }).catch(() => {})
    else if (this.owner && this.expectedProfile && this.expectedAuthentication) void this.currentPort.invoke('native_request_cancel', { context: this.owner, profileIntent: this.expectedProfile, authenticationIntent: this.expectedAuthentication, requestId: this.requestId }).catch(() => {})
    this.onclose?.()
  }
}

/** Public presentation captured once; native scope and OS dialog grant trust. */
export function captureNativeTrustPresentation() {
  const owner = context; const expectedProfile = profileIntent; const expectedAuthentication = authenticationIntent; const generation = profileGeneration; const revision = authenticationRevision; const captured = port
  if (!owner || !expectedProfile || !expectedAuthentication || !captured.trustChannel) throw new Error('Native trust presentation unavailable')
  return createTrustPort({
    context: owner,
    isCurrent: () => context === owner && expectedProfile === profileIntent && expectedAuthentication === authenticationIntent && generation === profileGeneration && revision === authenticationRevision && captured === port,
    invoke: (command, args) => captured.invoke(command, { ...args, profileIntent: expectedProfile, authenticationIntent: expectedAuthentication }),
    channel: captured.trustChannel
  })
}
