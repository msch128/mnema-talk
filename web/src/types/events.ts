// WebSocket is not represented in OpenAPI. These payloads follow
// internal/ws/{handle,voice,presence,signal,lifecycle}.go and chat publications.
import type { ModerationNotice } from '../lib/moderation'
import type { Message, ReactionSummary, ReadState, User, UserUpdate, VoiceUser, Presence } from './domain'
import { isAuthUser, isChatMessage, isChatReactionSummary, isChatNotifyLevel } from './rest'
import { ContractError, hasOwn, isRecord, isIdentifier, isTimestamp, isInteger, isString, isArrayOf, isDictionaryOf } from './validation'

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue | undefined }
export interface MediaState { channel_id: string; user_id: string; screen: boolean; camera: boolean }
export interface ScreenViewers { channel_id: string; user_id: string; viewers: string[] }
/** One encoding of a screen share as its sender declares it. */
export interface ScreenLayer { width: number; height: number; fps: number }
export interface ScreenLayers { channel_id: string; user_id: string; layers: ScreenLayer[] }
/** The layer of a screen share this connection receives (-1 before the first picture) and whether the server chose it. */
export interface ScreenReception { channel_id: string; user_id: string; layer: number; auto: boolean }
export interface ScreenLayersWanted { channel_id: string; layers: number[] }
export interface SpeakingState { channel_id: string; user_id: string; active: boolean }
export interface MuteState { channel_id: string; user_id: string; muted: boolean; deafened: boolean }
export interface VoiceRooms { started: Record<string, string>; now: string }
export type VoiceSnapshot = Record<string, Record<string, VoiceUser>>
export type VoiceStateUpdate =
  | { action: 'join'; channel_id: string; user: VoiceUser; started_at: string }
  | { action: 'leave'; channel_id: string; user_id: string }
export type ReadStateUpdate =
  | Pick<ReadState, 'channel_id' | 'last_read_at' | 'unread_count' | 'mention_count'>
  | { channel_id: string; last_read_at: string; refresh: true }
  | Pick<ReadState, 'channel_id' | 'notify_level'>

/** A bot's reply to a slash command that only the member who ran it sees (never stored). */
export interface InteractionResponse {
  channel_id: string
  bot_id: string
  bot_name: string
  bot_avatar_url?: string
  content: string
  embeds: { title?: string; description?: string }[] | null
  created_at: string
}

export interface ServerEventPayloads {
  pong: { t: number }
  server_info: { version: string; screen_layers?: number }
  system_update: { version: string }
  presence_snapshot: Record<string, Presence> | string[]
  presence_update: { user_id: string; status: Presence }
  member_joined: User
  channels_changed: null
  /** Roles, assignments or overwrites changed for this user: refetch GET /api/permissions and /api/channels. */
  permissions_changed: null
  /** A role was created, edited, deleted, reordered or assigned: refetch GET /api/roles and /api/members. */
  roles_changed: null
  typing: { channel_id: string; user_id: string }
  read_state: ReadStateUpdate
  message_create: Message
  message_update: Message
  message_delete: { id: string; channel_id: string; parent_id: string | null }
  message_reaction: { message_id: string; channel_id?: string; reactions: ReactionSummary[] }
  interaction_response: InteractionResponse
  user_update: UserUpdate
  /** A member's timeout started (timeout_until) or ended (null). */
  member_timeout: { id: string; timeout_until: string | null }
  /** Told to the member a moderator acted on. */
  moderation_notice: ModerationNotice
  webrtc_offer: { type: 'offer'; sdp: string }
  webrtc_candidate: RTCIceCandidateInit
  voice_snapshot: VoiceSnapshot
  voice_state_update: VoiceStateUpdate
  voice_rooms: VoiceRooms
  user_stats: { user_id: string; voice_seconds: number }
  webrtc_media_state: MediaState
  screen_viewers: ScreenViewers
  screen_layers: ScreenLayers
  screen_quality: ScreenReception
  screen_layers_wanted: ScreenLayersWanted
  voice_speaking: SpeakingState
  voice_mute_state: MuteState
  voice_kicked: { channel_id: string; reason?: 'room_full' | 'missing_permission' }
  media_handshake: { channel_id: string; user_id: string; wire: string }
}

export type EventUnion<Payloads> = { [K in keyof Payloads]: { type: K; payload: Payloads[K] } }[keyof Payloads]
export type ServerEvent = EventUnion<ServerEventPayloads>
export type ServerEventType = keyof ServerEventPayloads

export type SubscribeRequest =
  | { kind: 'camera'; user_id: string; on: boolean; all?: false; quality?: never }
  | { kind: 'screen'; user_id: string; on: boolean; all?: false; quality?: 'auto' | number }
  | { kind: 'camera'; all: true; on: boolean; user_id?: never; quality?: never }

export interface ClientEventPayloads {
  ping: { t: number }
  presence_idle: { idle: boolean }
  voice_join: { channel_id: string }
  voice_leave: Record<string, never>
  typing: { channel_id: string }
  voice_speaking: { active: boolean }
  voice_mute_state: { muted: boolean; deafened: boolean }
  webrtc_answer: RTCSessionDescriptionInit
  webrtc_candidate: RTCIceCandidateInit
  webrtc_diag: { [key: string]: JsonValue | undefined }
  webrtc_request_keyframe: Record<string, never>
  webrtc_subscribe: SubscribeRequest
  webrtc_layers: { layers: ScreenLayer[] }
  webrtc_screenshare_start: Record<string, never>
  webrtc_screenshare_stop: Record<string, never>
  webrtc_camera_stop: Record<string, never>
  media_handshake: { channel_id: string; wire: string }
}
export type ClientEvent = EventUnion<ClientEventPayloads>
export type ClientEventType = keyof ClientEventPayloads

export function isPresence(value: unknown): value is Presence {
  return value === 'online' || value === 'away' || value === 'dnd' || value === 'focus' || value === 'offline'
}

export function isVoiceUser(value: unknown): value is VoiceUser {
  return isRecord(value) && isTimestamp(value['joined_at'])
    && typeof value['muted'] === 'boolean' && typeof value['deafened'] === 'boolean' && isAuthUser(value)
}

export function isIceCandidate(value: unknown): value is RTCIceCandidateInit {
  if (!isRecord(value)) return false
  return isString(value['candidate'], 16_384)
    && (value['sdpMid'] === undefined || value['sdpMid'] === null || isString(value['sdpMid'], 256))
    && (value['sdpMLineIndex'] === undefined || value['sdpMLineIndex'] === null || (isInteger(value['sdpMLineIndex']) && value['sdpMLineIndex'] >= 0 && value['sdpMLineIndex'] <= 65_535))
    && (value['usernameFragment'] === undefined || value['usernameFragment'] === null || isString(value['usernameFragment'], 256))
}

// Variant payloads are spread into stores, so every retained field must belong
// to the validated variant. Go publishes these exact read-state/admin shapes.
function onlyFields(value: Record<string, unknown>, fields: readonly string[]): boolean {
  return Object.keys(value).every(key => fields.includes(key))
}

/** A screen share has at most this many layers (internal/sfu MaxLayers). */
export const MAX_SCREEN_LAYERS = 3

function isLayerIndex(value: unknown): value is number {
  return isInteger(value) && value >= 0 && value < MAX_SCREEN_LAYERS
}

function isScreenLayer(value: unknown): value is ScreenLayer {
  return isRecord(value) && isInteger(value['width']) && isInteger(value['height']) && isInteger(value['fps'])
    && value['width'] > 0 && value['height'] > 0 && value['fps'] > 0
    && onlyFields(value, ['width', 'height', 'fps'])
}

function roomUser(value: Record<string, unknown>): boolean {
  return isIdentifier(value['channel_id']) && isIdentifier(value['user_id'])
}

export function isServerEvent(value: unknown): value is ServerEvent {
  if (!isRecord(value) || typeof value['type'] !== 'string') return false
  const payload = value['payload']
  switch (value['type']) {
    case 'channels_changed':
    case 'permissions_changed':
    case 'roles_changed': return payload === null
    case 'member_joined': return isAuthUser(payload)
    case 'message_create':
    case 'message_update': return isChatMessage(payload)
    case 'presence_snapshot': return isArrayOf(payload, isIdentifier) || isDictionaryOf(payload, isPresence)
    case 'voice_snapshot': return isDictionaryOf(payload, (users): users is Record<string, VoiceUser> => isDictionaryOf(users, isVoiceUser))
  }
  if (!isRecord(payload)) return false
  switch (value['type']) {
    case 'pong': return typeof payload['t'] === 'number' && Number.isFinite(payload['t'])
    case 'server_info': return isString(payload['version'], 64)
      && (!hasOwn(payload, 'screen_layers') || (isInteger(payload['screen_layers']) && payload['screen_layers'] >= 0 && payload['screen_layers'] <= MAX_SCREEN_LAYERS))
    case 'system_update': return isString(payload['version'], 64)
    case 'presence_update': return isIdentifier(payload['user_id']) && isPresence(payload['status'])
    case 'typing': return roomUser(payload)
    case 'read_state':
      if (!isIdentifier(payload['channel_id'])) return false
      if (hasOwn(payload, 'refresh')) return payload['refresh'] === true && isTimestamp(payload['last_read_at'])
        && onlyFields(payload, ['channel_id', 'last_read_at', 'refresh'])
      if (hasOwn(payload, 'notify_level')) return isChatNotifyLevel(payload['notify_level'])
        && onlyFields(payload, ['channel_id', 'notify_level'])
      return (payload['last_read_at'] === null || isTimestamp(payload['last_read_at']))
        && isInteger(payload['unread_count']) && isInteger(payload['mention_count'])
        && onlyFields(payload, ['channel_id', 'last_read_at', 'unread_count', 'mention_count'])
    case 'message_delete': return isIdentifier(payload['id']) && isIdentifier(payload['channel_id'])
      && (payload['parent_id'] === null || isIdentifier(payload['parent_id']))
    case 'message_reaction': return isIdentifier(payload['message_id']) && (!hasOwn(payload, 'channel_id') || isIdentifier(payload['channel_id']))
      && isArrayOf(payload['reactions'], isChatReactionSummary)
    case 'interaction_response': return isIdentifier(payload['channel_id']) && isIdentifier(payload['bot_id']) && isString(payload['bot_name'], 64)
      && (!hasOwn(payload, 'bot_avatar_url') || isString(payload['bot_avatar_url'], 512)) && isString(payload['content'], 8_000)
      && (payload['embeds'] === null || isArrayOf(payload['embeds'], isNoteEmbed)) && isTimestamp(payload['created_at'])
    case 'user_update': return hasOwn(payload, 'disabled')
      ? isIdentifier(payload['id']) && typeof payload['disabled'] === 'boolean' && onlyFields(payload, ['id', 'disabled'])
      : isAuthUser(payload)
    case 'member_timeout': return isIdentifier(payload['id']) && (payload['timeout_until'] === null || isTimestamp(payload['timeout_until']))
      && onlyFields(payload, ['id', 'timeout_until'])
    case 'moderation_notice': return (payload['kind'] === 'timeout' || payload['kind'] === 'timeout_removed' || payload['kind'] === 'ban')
      && isString(payload['reason'], 400)
      && (!hasOwn(payload, 'until') || isTimestamp(payload['until']))
      && (!hasOwn(payload, 'duration_seconds') || isInteger(payload['duration_seconds']))
    case 'webrtc_offer': return payload['type'] === 'offer' && isString(payload['sdp'], 262_144)
    case 'webrtc_candidate': return isIceCandidate(payload)
    case 'voice_state_update': return isIdentifier(payload['channel_id']) && (
      payload['action'] === 'leave' ? isIdentifier(payload['user_id'])
        : payload['action'] === 'join' && isVoiceUser(payload['user']) && isTimestamp(payload['started_at']))
    case 'voice_rooms': return isDictionaryOf(payload['started'], isTimestamp) && isTimestamp(payload['now'])
    case 'user_stats': return isIdentifier(payload['user_id']) && isInteger(payload['voice_seconds'])
    case 'webrtc_media_state': return roomUser(payload) && typeof payload['screen'] === 'boolean' && typeof payload['camera'] === 'boolean'
    case 'screen_viewers': return roomUser(payload) && isArrayOf(payload['viewers'], isIdentifier)
    case 'screen_layers': return roomUser(payload) && isArrayOf(payload['layers'], isScreenLayer) && (payload['layers'] as unknown[]).length <= MAX_SCREEN_LAYERS
    case 'screen_quality': return roomUser(payload) && isInteger(payload['layer']) && payload['layer'] >= -1 && payload['layer'] < MAX_SCREEN_LAYERS
      && typeof payload['auto'] === 'boolean'
    case 'screen_layers_wanted': return isIdentifier(payload['channel_id']) && isArrayOf(payload['layers'], isLayerIndex)
    case 'voice_speaking': return roomUser(payload) && typeof payload['active'] === 'boolean'
    case 'voice_mute_state': return roomUser(payload) && typeof payload['muted'] === 'boolean' && typeof payload['deafened'] === 'boolean'
    case 'voice_kicked': return isIdentifier(payload['channel_id'])
      && (!hasOwn(payload, 'reason') || payload['reason'] === 'room_full' || payload['reason'] === 'missing_permission')
    case 'media_handshake': return isIdentifier(payload['channel_id']) && isIdentifier(payload['user_id'])
      && isString(payload['wire'], 48_000) && payload['wire'] !== ''
    default: return false
  }
}

function isNoteEmbed(value: unknown): boolean {
  return isRecord(value) && (!hasOwn(value, 'title') || isString(value['title'], 1_000)) && (!hasOwn(value, 'description') || isString(value['description'], 8_000))
}

const SERVER_EVENT_TYPES = new Set<string>([
  'pong', 'server_info', 'system_update', 'presence_snapshot', 'presence_update', 'member_joined',
  'channels_changed', 'permissions_changed', 'roles_changed', 'typing', 'read_state', 'message_create', 'message_update', 'message_delete',
  'message_reaction', 'interaction_response', 'user_update', 'member_timeout', 'moderation_notice', 'webrtc_offer', 'webrtc_candidate', 'voice_snapshot',
  'voice_state_update', 'voice_rooms', 'user_stats', 'webrtc_media_state', 'screen_viewers',
  'screen_layers', 'screen_quality', 'screen_layers_wanted', 'voice_speaking', 'voice_mute_state', 'voice_kicked', 'media_handshake'
])

/** Unknown event names stay ignorable for forward compatibility; malformed
 * known events never reach stores. No payload is included in the error. */
export function decodeServerEvent(value: unknown): ServerEvent | null {
  if (isRecord(value) && typeof value['type'] === 'string' && !SERVER_EVENT_TYPES.has(value['type'])) return null
  if (!isServerEvent(value)) throw new ContractError('WebSocket event')
  return value
}
