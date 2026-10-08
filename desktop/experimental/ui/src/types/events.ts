// WebSocket is not represented in OpenAPI. These payloads follow
// internal/ws/{handle,voice,presence,signal,lifecycle}.go and chat publications.
import type { Message, ReactionSummary, ReadState, User, UserUpdate, VoiceUser, Presence } from './domain'
import { isAuthUser, isChatMessage, isChatReactionSummary, isChatNotifyLevel } from './rest'
import { ContractError, hasOwn, isRecord, isIdentifier, isTimestamp, isInteger, isString, isArrayOf, isDictionaryOf } from './validation'

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue | undefined }
export interface MediaState { channel_id: string; user_id: string; screen: boolean; camera: boolean }
export interface ScreenViewers { channel_id: string; user_id: string; viewers: string[] }
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

export interface ServerEventPayloads {
  pong: { t: number }
  server_info: { version: string }
  system_update: { version: string }
  presence_snapshot: Record<string, Presence> | string[]
  presence_update: { user_id: string; status: Presence }
  member_joined: User
  channels_changed: null
  typing: { channel_id: string; user_id: string }
  read_state: ReadStateUpdate
  message_create: Message
  message_update: Message
  message_delete: { id: string; channel_id: string; parent_id: string | null }
  message_reaction: { message_id: string; reactions: ReactionSummary[] }
  user_update: UserUpdate
  webrtc_offer: { type: 'offer'; sdp: string }
  webrtc_candidate: RTCIceCandidateInit
  voice_snapshot: VoiceSnapshot
  voice_state_update: VoiceStateUpdate
  voice_rooms: VoiceRooms
  user_stats: { user_id: string; voice_seconds: number }
  webrtc_media_state: MediaState
  screen_viewers: ScreenViewers
  voice_speaking: SpeakingState
  voice_mute_state: MuteState
  voice_kicked: { channel_id: string; reason?: 'room_full' }
}

export type EventUnion<Payloads> = { [K in keyof Payloads]: { type: K; payload: Payloads[K] } }[keyof Payloads]
export type ServerEvent = EventUnion<ServerEventPayloads>
export type ServerEventType = keyof ServerEventPayloads

export type SubscribeRequest =
  | { kind: 'screen' | 'camera'; user_id: string; on: boolean; all?: false }
  | { kind: 'camera'; all: true; on: boolean; user_id?: never }

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
  webrtc_screenshare_start: Record<string, never>
  webrtc_screenshare_stop: Record<string, never>
  webrtc_camera_stop: Record<string, never>
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

function roomUser(value: Record<string, unknown>): boolean {
  return isIdentifier(value['channel_id']) && isIdentifier(value['user_id'])
}

export function isServerEvent(value: unknown): value is ServerEvent {
  if (!isRecord(value) || typeof value['type'] !== 'string') return false
  const payload = value['payload']
  switch (value['type']) {
    case 'channels_changed': return payload === null
    case 'member_joined': return isAuthUser(payload)
    case 'message_create':
    case 'message_update': return isChatMessage(payload)
    case 'presence_snapshot': return isArrayOf(payload, isIdentifier) || isDictionaryOf(payload, isPresence)
    case 'voice_snapshot': return isDictionaryOf(payload, (users): users is Record<string, VoiceUser> => isDictionaryOf(users, isVoiceUser))
  }
  if (!isRecord(payload)) return false
  switch (value['type']) {
    case 'pong': return typeof payload['t'] === 'number' && Number.isFinite(payload['t'])
    case 'server_info':
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
    case 'message_reaction': return isIdentifier(payload['message_id']) && isArrayOf(payload['reactions'], isChatReactionSummary)
    case 'user_update': return hasOwn(payload, 'disabled')
      ? isIdentifier(payload['id']) && typeof payload['disabled'] === 'boolean' && onlyFields(payload, ['id', 'disabled'])
      : isAuthUser(payload)
    case 'webrtc_offer': return payload['type'] === 'offer' && isString(payload['sdp'], 262_144)
    case 'webrtc_candidate': return isIceCandidate(payload)
    case 'voice_state_update': return isIdentifier(payload['channel_id']) && (
      payload['action'] === 'leave' ? isIdentifier(payload['user_id'])
        : payload['action'] === 'join' && isVoiceUser(payload['user']) && isTimestamp(payload['started_at']))
    case 'voice_rooms': return isDictionaryOf(payload['started'], isTimestamp) && isTimestamp(payload['now'])
    case 'user_stats': return isIdentifier(payload['user_id']) && isInteger(payload['voice_seconds'])
    case 'webrtc_media_state': return roomUser(payload) && typeof payload['screen'] === 'boolean' && typeof payload['camera'] === 'boolean'
    case 'screen_viewers': return roomUser(payload) && isArrayOf(payload['viewers'], isIdentifier)
    case 'voice_speaking': return roomUser(payload) && typeof payload['active'] === 'boolean'
    case 'voice_mute_state': return roomUser(payload) && typeof payload['muted'] === 'boolean' && typeof payload['deafened'] === 'boolean'
    case 'voice_kicked': return isIdentifier(payload['channel_id'])
      && (!hasOwn(payload, 'reason') || payload['reason'] === 'room_full')
    default: return false
  }
}

const SERVER_EVENT_TYPES = new Set<string>([
  'pong', 'server_info', 'system_update', 'presence_snapshot', 'presence_update', 'member_joined',
  'channels_changed', 'typing', 'read_state', 'message_create', 'message_update', 'message_delete',
  'message_reaction', 'user_update', 'webrtc_offer', 'webrtc_candidate', 'voice_snapshot',
  'voice_state_update', 'voice_rooms', 'user_stats', 'webrtc_media_state', 'screen_viewers',
  'voice_speaking', 'voice_mute_state', 'voice_kicked'
])

/** Unknown event names stay ignorable for forward compatibility; malformed
 * known events never reach stores. No payload is included in the error. */
export function decodeServerEvent(value: unknown): ServerEvent | null {
  if (isRecord(value) && typeof value['type'] === 'string' && !SERVER_EVENT_TYPES.has(value['type'])) return null
  if (!isServerEvent(value)) throw new ContractError('WebSocket event')
  return value
}
