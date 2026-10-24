import type { ApiReply, ApiRequest, ApiTransport } from '../api'
import type { StatusChannel } from '../trustPort'
import { mapAdminOperation } from './nativeAdmin'
import { nativeLogin, nativeLogout, nativePassword, nativeRegister } from './nativeAuth'
import { isNativeTypedChatPath, type NativeTypedChatBridge } from './nativeChatApi'
import { call } from './nativeFlow'
import { cancelled, captureSelector, isRecord, nativeSession, selectorCurrent, unavailable, type CapturedSelector, type NativeReply } from './nativeSession'

const USER_PATH = /^\/api\/users\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/
const PRESENCE = ['online', 'away', 'dnd', 'focus']

function boundedText(input: Record<string, unknown>, name: string, maximum: number): boolean {
  const value = input[name]
  return typeof value === 'string' && Array.from(value).length <= maximum
}

function profileUpdate(path: string, input: Record<string, unknown>, keys: string[]): Record<string, unknown> | null {
  if (path !== '/api/users/me/profile' || keys.length !== 2) return null
  if (!boundedText(input, 'display_name', 24) || !boundedText(input, 'bio', 250)) return null
  return { operation: 'profile', display_name: input['display_name'], bio: input['bio'] }
}

function localeUpdate(path: string, input: Record<string, unknown>, keys: string[]): Record<string, unknown> | null {
  if (path !== '/api/users/me/locale' || keys.length !== 1) return null
  const locale = input['locale']
  if (locale !== 'de' && locale !== 'en') return null
  return { operation: 'locale', locale }
}

function knownPresence(value: unknown): value is string {
  return typeof value === 'string' && PRESENCE.includes(value)
}

function presenceUpdate(path: string, input: Record<string, unknown>, keys: string[]): Record<string, unknown> | null {
  if (path !== '/api/users/me/presence' || keys.length !== 1) return null
  const presence = input['presence']
  if (!knownPresence(presence)) return null
  return { operation: 'presence', presence }
}

function statusUpdate(path: string, input: Record<string, unknown>, keys: string[]): Record<string, unknown> | null {
  if (path !== '/api/users/me/status') return null
  const expires = input['expires_at']
  const hasExpiry = keys.length === 2 && typeof expires === 'string' && !Number.isNaN(Date.parse(expires)) && expires.length <= 40
  if (keys.length !== 1 && !hasExpiry) return null
  if (!boundedText(input, 'status_text', 32)) return null
  return hasExpiry ? { operation: 'status', status_text: input['status_text'], expires_at: expires } : { operation: 'status', status_text: input['status_text'] }
}

function personalUpdate(path: string, input: Record<string, unknown>): Record<string, unknown> | null {
  const keys = Object.keys(input)
  const profile = profileUpdate(path, input, keys)
  if (profile) return profile
  const locale = localeUpdate(path, input, keys)
  if (locale) return locale
  const presence = presenceUpdate(path, input, keys)
  if (presence) return presence
  return statusUpdate(path, input, keys)
}

function userRead(path: string, request: ApiRequest): Record<string, unknown> | null {
  const userId = USER_PATH.exec(path)?.[1]
  if (!userId || request.method !== 'GET') return null
  if (request.json !== undefined) return null
  return { operation: 'user', user_id: userId }
}

function personalOperation(path: string, request: ApiRequest): Record<string, unknown> | null {
  const read = userRead(path, request)
  if (request.method !== 'PUT' || !isRecord(request.json)) return read
  const update = personalUpdate(path, request.json)
  if (update) return update
  return read
}

interface TypedChatSnapshot {
  owner: string
  selector: CapturedSelector
  channel: () => StatusChannel
}

function captureTypedChat(): TypedChatSnapshot | null {
  const selector = captureSelector()
  const owner = selector.owner
  const channel = selector.port.chatChannel
  if (!owner || !selector.profile) return null
  if (!selector.authentication || !channel || nativeSession.authenticationUncertain) return null
  return { owner, selector, channel }
}

function typedChatCall(snapshot: TypedChatSnapshot, command: string, args: Record<string, unknown>, signal?: AbortSignal): Promise<NativeReply> {
  if (!selectorCurrent(snapshot.selector)) throw cancelled()
  return call(command, args, signal)
}

function typedChatBridge(snapshot: TypedChatSnapshot): NativeTypedChatBridge {
  return {
    context: snapshot.owner,
    isCurrent: () => selectorCurrent(snapshot.selector),
    channel: snapshot.channel,
    call: (command, args, signal) => typedChatCall(snapshot, command, args, signal),
  }
}

async function nativeTypedChat(path: string, request: ApiRequest): Promise<ApiReply> {
  const snapshot = captureTypedChat()
  if (!snapshot) return unavailable()
  const reply = await nativeSession.typedChat.handle(path, request, typedChatBridge(snapshot))
  if (reply) return reply
  return unavailable()
}

async function viaNative(command: string, payload: Record<string, unknown>, request: ApiRequest): Promise<ApiReply> {
  const reply = await call(command, payload, request.signal)
  return { status: reply.status, body: reply.body }
}

const METADATA_RESOURCES: Readonly<Record<string, string>> = {
  '/api/channels': 'channels', '/api/members': 'members',
  '/api/read-state': 'read_state', '/api/health': 'health', '/api/legal': 'legal',
}

function metadataCommand(resource: string): string {
  if (resource === 'legal' || resource === 'health') return 'native_public_metadata_request'
  return 'native_metadata_request'
}

type Route = (path: string, request: ApiRequest) => Promise<ApiReply> | null

function routeRegister(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (path !== '/api/auth/register' || request.method !== 'POST') return null
  if (!isRecord(request.json)) return null
  return nativeRegister(request, request.json)
}

function routePassword(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (path !== '/api/auth/password' || request.method !== 'PUT') return null
  if (!isRecord(request.json)) return null
  return nativePassword(request, request.json)
}

function routeAdmin(path: string, request: ApiRequest): Promise<ApiReply> | null {
  const admin = mapAdminOperation(path, request)
  if (!admin) return null
  return viaNative('native_admin_request', { input: admin }, request)
}

function routeLogin(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (path !== '/api/auth/login' || request.method !== 'POST') return null
  if (!isRecord(request.json)) return null
  return nativeLogin(request, request.json)
}

function routeMe(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (path !== '/api/auth/me' || request.method !== 'GET') return null
  return viaNative('native_auth_me', {}, request)
}

function routeLogout(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (path !== '/api/auth/logout' || request.method !== 'POST') return null
  return nativeLogout(request)
}

function routePersonal(path: string, request: ApiRequest): Promise<ApiReply> | null {
  const personal = personalOperation(path, request)
  if (!personal) return null
  return viaNative('native_personal_metadata_request', { input: personal }, request)
}

function routeTypedChat(path: string, request: ApiRequest): Promise<ApiReply> | null {
  if (!isNativeTypedChatPath(path)) return null
  return nativeTypedChat(path, request)
}

function routeMetadata(path: string, request: ApiRequest): Promise<ApiReply> | null {
  const resource = METADATA_RESOURCES[path]
  if (!resource) return null
  if (request.method !== 'GET' || request.json !== undefined) return null
  return viaNative(metadataCommand(resource), { resource }, request)
}

// The first route that matches the request answers it. GET /api/mentions (the
// mentions inbox) and /api/search are not served natively: they answer
// "unavailable" and the interface shows its failure state. The same holds for
// channel and category notification levels, GET /api/notification-settings and
// POST /api/read-state/read-all: the trusted desktop client has no typed
// command for them. GET /api/channels/{id}/first-unread (jump to the first
// unread message) is in the same position: the native DEV client answers
// "unavailable" and the pill simply stays hidden, the shipped clients' proxy
// passes the path. Also unavailable: /api/onboarding (rules acceptance,
// welcome flow) and the public invite preview. This transport belongs to the
// experimental Windows DEV client only (isDesktopRuntime); the shipped desktop
// clients (v0.2.x) run the signed web bundle through the trusted proxy, which
// passes every /api path, so rules acceptance works there. The same holds for Web Push (/api/push/*:
// config, devices, preferences, test): the native DEV client answers "unavailable",
// and the shipped clients' proxy passes the paths although the web bundle never
// registers a service worker there (the desktop shows its own OS notifications). Add typed onboarding
// operations here before the native DEV client is ever shipped.
// Pinned messages (GET /api/channels/{id}/pins, PUT/DELETE .../messages/{id}/pin) follow
// the same rule: the shipped clients' proxy passes them, the native DEV client answers "unavailable".
// The same holds for custom emoji (/api/emoji, /api/emoji/{id}/image and /still,
// /api/admin/emoji): the shipped clients' proxy passes them, the native DEV client
// answers "unavailable".
// The same holds for the account recovery endpoints (/api/auth/options,
// /api/auth/password-reset/*, /api/auth/email/verify, /api/users/me/account,
// /api/users/me/email*, /api/users/me/2fa/*, /api/users/me/export*, /api/users/me/delete, /api/auth/login/2fa) and the Owner's /api/admin/system/mail/test: the
// shipped clients reach them through the trusted proxy, the native DEV client
// answers "unavailable" and its sign-in screen offers no reset link.
// The same holds for slash commands and polls (GET /api/channels/*/commands,
// POST /api/channels/*/interactions, POST /api/channels/*/polls and
// /api/channels/*/messages/*/poll[/votes|/end]): the shipped clients' proxy
// passes them, the native DEV client answers "unavailable": there is no command menu and
// a poll cannot be posted.
// Also unavailable natively: GET /api/channels/{id}/messages/{id}/edits (the
// moderators' edit history); the shipped clients' proxy passes it.
const ROUTES: readonly Route[] = [
  routeRegister, routePassword, routeAdmin, routeLogin, routeMe, routeLogout, routePersonal, routeTypedChat, routeMetadata,
]

async function transport(path: string, request: ApiRequest): Promise<ApiReply> {
  if (request.form) return unavailable()
  for (const route of ROUTES) {
    const answer = route(path, request)
    if (answer) return answer
  }
  return unavailable()
}

export const nativeApiTransport: ApiTransport = transport
