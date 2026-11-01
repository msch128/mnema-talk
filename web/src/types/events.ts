// WebSocket is not represented in OpenAPI. These payloads follow
// internal/ws/{handle,voice,presence,signal,lifecycle}.go and chat publications.
import type { ModerationNotice } from '../lib/moderation'
import type { Message, Poll, ReactionSummary, ReadState, User, UserUpdate, VoiceUser, Presence } from './domain'
import type { ChatUserRef, CommunityeventsCommunityEvent } from './rest'
import { isAuthUser, isChatMessage, isChatPoll, isChatReactionSummary, isChatNotifyLevel, isCommunityeventsCommunityEvent } from './rest'
import type { StageHand, StageTalkPower, TalkReaction } from './stageEvents'
import { STAGE_EVENT_CHECKS } from './stageEvents'
import { NOTICE_EVENT_CHECKS } from './noticeEvents'
import type { DirectMessageMember, DirectMessageRef, DmCallEnd, DmCallRing } from './dmEvents'
import { DM_EVENT_CHECKS } from './dmEvents'
import { ContractError, onlyFields, hasOwn, isRecord, isIdentifier, isTimestamp, isInteger, isString, isArrayOf, isDictionaryOf } from './validation'

export type JsonValue = null | boolean | number | string | JsonValue[] | { [key: string]: JsonValue | undefined }
export interface MediaState { channel_id: string; user_id: string; screen: boolean; camera: boolean }
export interface ScreenViewers { channel_id: string; user_id: string; viewers: string[] }
/** One encoding of a screen share as its sender declares it. */
export interface ScreenLayer { width: number; height: number; fps: number }
export interface ScreenLayers { channel_id: string; user_id: string; layers: ScreenLayer[] }
/** The layer of a screen share this connection receives (-1 before the first picture) and whether the server chose it. */
export interface ScreenReception { channel_id: string; user_id: string; layer: number; auto: boolean }
/** The layers of my own screen share (or, with source 'camera', of my camera) that someone receives. */
export interface ScreenLayersWanted { channel_id: string; layers: number[]; source?: 'camera' }
/** Whose cameras the server does not forward to me (it caps them per viewer); [] for none. */
export interface CamerasCapped { channel_id: string; user_ids: string[] }
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
  | { refresh: true }
  | Pick<ReadState, 'channel_id' | 'notify_level'>

/** A bot's reply to a slash command that only the member who ran it sees (never stored). */
/** What a bot says it is doing: Discord's activity type (0 playing, 2 listening, 3 watching, 4 custom, 5 competing) and a short text. */
export interface BotActivity {
  type: number
  name: string
}

function isBotActivity(value: unknown): value is BotActivity {
  return isRecord(value) && isInteger(value['type']) && value['type'] >= 0 && value['type'] <= 5
    && isString(value['name'], 128) && value['name'] !== ''
}

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
  server_info: { version: string; screen_layers?: number; camera_layers?: number }
  system_update: { version: string }
  presence_snapshot: Record<string, Presence> | string[]
  /** A bot that set an activity (PRESENCE_UPDATE of the bot gateway) also sends it. */
  presence_update: { user_id: string; status: Presence; activity?: BotActivity }
  member_joined: User
  channels_changed: null
  /** Roles, assignments or overwrites changed for this user: refetch GET /api/permissions and /api/channels. */
  permissions_changed: null
  /** A role was created, edited, deleted, reordered or assigned: refetch GET /api/roles and /api/members. */
  roles_changed: null
  /** A custom emoji was added, renamed or deleted: refetch GET /api/emoji. */
  emoji_changed: null
  /** The caller followed or unfollowed a thread (sent to their own sessions only). */
  thread_follow_updated: { thread_id: string; channel_id: string; following: boolean }
  /** The caller blocked or unblocked a member (own sessions only): refetch GET /api/me/blocks. */
  block_updated: { user_id: string; blocked: boolean }
  /** The caller saved or removed a message (own sessions only): refetch GET /api/me/saved. */
  saved_updated: { message_id: string; saved: boolean }
  /** The caller's reminder was made, fell due or was removed (own sessions only): refetch GET /api/me/reminders. */
  reminder_update: { id: string; state: 'created' | 'due' | 'removed' }
  /** The caller's scheduled message was made, changed, sent or refused (own sessions only): refetch GET /api/me/scheduled. */
  scheduled_message_update: { id: string; channel_id: string; state: 'created' | 'updated' | 'sent' | 'failed' | 'deleted'; reason?: string }
  /** A direct message was started with the receiver, or the receiver was added to a group: a DirectMessage as GET /api/dms/{id} returns it. */
  dm_created: DirectMessageRef
  /** A member joined a group direct message the receiver is in. */
  dm_member_added: { dm_id: string; member: DirectMessageMember }
  /** A member left a group direct message the receiver is in (or the receiver left it on another device). */
  dm_member_removed: { dm_id: string; member: DirectMessageMember }
  /** The caller closed, reopened or favourited a conversation (own sessions only): read it again; dm_id null: the whole list (their settings changed). */
  dm_prefs_updated: { dm_id: string | null }
  /** A Talk was started from, or shared into, a conversation: everyone but the caller has an incoming call (dm_call_end stops it). */
  dm_call_ring: DmCallRing
  /** The call stopped ringing (answered, cancelled, timeout) or is over (ended); `active`: somebody is still in the Talk. */
  dm_call_end: DmCallEnd
  typing: { channel_id: string; user_id: string }
  read_state: ReadStateUpdate
  message_create: Message
  message_update: Message
  message_delete: { id: string; channel_id: string; parent_id: string | null }
  message_reaction: { message_id: string; channel_id?: string; reactions: ReactionSummary[] }
  /** Tallies of a poll after a vote or its end; the same for every viewer (no voters in an anonymous poll). */
  poll_update: { message_id: string; channel_id: string; poll: Poll }
  /** A message was pinned or unpinned: refetch GET /api/channels/{id}/pins and note it in the channel. */
  pins_changed: { channel_id: string; message_id: string; pinned: boolean; actor_id: string; actor_name: string; pinned_at?: string; pinned_by?: ChatUserRef }
  interaction_response: InteractionResponse
  user_update: UserUpdate
  /** A member's timeout started (timeout_until) or ended (null). */
  member_timeout: { id: string; timeout_until: string | null }
  /** Told to the member a moderator acted on. */
  moderation_notice: ModerationNotice
  /** A moderator signed this member out everywhere (/kick); the connection closes right after. They may sign in again at once. */
  kicked: { reason: string }
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
  cameras_capped: CamerasCapped
  voice_speaking: SpeakingState
  voice_mute_state: MuteState
  voice_kicked: { channel_id: string; reason?: 'room_full' | 'missing_permission' | 'rules_not_accepted' | 'talk_rate_limited' | 'talk_unavailable' | 'connection_lost' | 'media_unavailable' | 'joined_elsewhere' | 'voice_taken_over' }
  /** Who leads a temporary Talk now (null: nobody). */
  talk_leader: { channel_id: string; leader_id: string | null }
  /** Joining a Create-a-Talk lobby opened this Talk: join it like any other. */
  talk_moved: { from_channel_id: string; channel_id: string }
  media_handshake: { channel_id: string; user_id: string; wire: string }
  stage_hand: StageHand
  stage_talk_power: StageTalkPower
  talk_reaction: TalkReaction
  /** Tags in a forum channel changed. */
  forum_tags_changed: { channel_id: string }
  event_update: CommunityeventsCommunityEvent
  event_delete: { id: string }
  event_reminder: { event_id: string; title: string; start_time: string; channel_id?: string }
  community_updated: { community_name: string; community_icon?: string } // the Owner renamed the community or changed its icon (community_icon: the new version, '' when removed)
}

export type EventUnion<Payloads> = { [K in keyof Payloads]: { type: K; payload: Payloads[K] } }[keyof Payloads]
export type ServerEvent = EventUnion<ServerEventPayloads>
export type ServerEventType = keyof ServerEventPayloads

export type SubscribeRequest =
  | { kind: 'camera'; user_id: string; on: boolean; all?: false; quality?: never; fit?: never; fit_px?: number }
  | { kind: 'screen'; user_id: string; on: boolean; all?: false; quality?: 'auto' | number; fit?: number; fit_px?: never }
  | { kind: 'camera'; all: true; on: boolean; user_id?: never; quality?: never; fit?: never; fit_px?: never }
  | { kind: 'camera'; user_ids: string[]; on: boolean; user_id?: never; all?: false; quality?: never; fit?: never; fit_px?: never }

export interface ClientEventPayloads {
  ping: { t: number }
  presence_idle: { idle: boolean }
  voice_join: { channel_id: string; takeover?: true; resume?: true; tab?: string }
  voice_leave: Record<string, never>
  typing: { channel_id: string }
  voice_speaking: { active: boolean }
  voice_mute_state: { muted: boolean; deafened: boolean }
  webrtc_answer: RTCSessionDescriptionInit
  webrtc_candidate: RTCIceCandidateInit
  webrtc_diag: { [key: string]: JsonValue | undefined }
  webrtc_request_keyframe: { source?: 'screen' | 'camera' }
  webrtc_subscribe: SubscribeRequest
  webrtc_layers: { layers: ScreenLayer[]; source?: 'camera' }
  webrtc_screenshare_start: Record<string, never>
  webrtc_screenshare_stop: Record<string, never>
  webrtc_camera_stop: Record<string, never>
  media_handshake: { channel_id: string; wire: string }
  stage_hand: { raised: boolean; user_id?: string }
  stage_talk_power: { user_id: string; granted: boolean }
  talk_reaction: { emoji: string }
}
export type ClientEvent = EventUnion<ClientEventPayloads>
export type ClientEventType = keyof ClientEventPayloads

export function isPresence(value: unknown): value is Presence {
  return value === 'online' || value === 'away' || value === 'dnd' || value === 'focus' || value === 'offline'
}

export function isVoiceUser(value: unknown): value is VoiceUser {
  return isRecord(value) && isTimestamp(value['joined_at'])
    && typeof value['muted'] === 'boolean' && typeof value['deafened'] === 'boolean'
    && (!hasOwn(value, 'talk_power') || typeof value['talk_power'] === 'boolean')
    && (!hasOwn(value, 'hand_raised_at') || isTimestamp(value['hand_raised_at']))
    && isAuthUser(value)
}

function isOptionalString(value: unknown, maxLen: number): boolean {
  if (value === undefined || value === null) return true
  return isString(value, maxLen)
}

function isOptionalSdpMLineIndex(value: unknown): boolean {
  if (value === undefined || value === null) return true
  return isInteger(value) && value >= 0 && value <= 65_535
}

export function isIceCandidate(value: unknown): value is RTCIceCandidateInit {
  if (!isRecord(value)) return false
  if (!isString(value['candidate'], 16_384)) return false
  if (!isOptionalString(value['sdpMid'], 256)) return false
  if (!isOptionalSdpMLineIndex(value['sdpMLineIndex'])) return false
  return isOptionalString(value['usernameFragment'], 256)
}

// Variant payloads are spread into stores, so every retained field must belong
// to the validated variant. Go publishes these exact read-state/admin shapes.

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

type RecordCheck = (payload: Record<string, unknown>) => boolean

// Events whose payload is not a JSON object (null, an array or a user map).
const NON_OBJECT_CHECKS = new Map<string, (payload: unknown) => boolean>([
  ['channels_changed', payload => payload === null],
  ['permissions_changed', payload => payload === null],
  ['roles_changed', payload => payload === null],
  ['emoji_changed', payload => payload === null],
  ['member_joined', isAuthUser],
  ['message_create', isChatMessage],
  ['message_update', isChatMessage],
  ['event_update', isCommunityeventsCommunityEvent],
  ['presence_snapshot', payload => isArrayOf(payload, isIdentifier) || isDictionaryOf(payload, isPresence)],
  ['voice_snapshot', payload => isDictionaryOf(payload, (users): users is Record<string, VoiceUser> => isDictionaryOf(users, isVoiceUser))]
])

function isRefreshReadState(payload: Record<string, unknown>): boolean {
  if (payload['refresh'] !== true || !isTimestamp(payload['last_read_at'])) return false
  return onlyFields(payload, ['channel_id', 'last_read_at', 'refresh'])
}

function isNotifyLevelReadState(payload: Record<string, unknown>): boolean {
  if (!isChatNotifyLevel(payload['notify_level'])) return false
  return onlyFields(payload, ['channel_id', 'notify_level'])
}

function isStandardReadState(payload: Record<string, unknown>): boolean {
  const lastReadAt = payload['last_read_at']
  const validLastRead = lastReadAt === null || isTimestamp(lastReadAt)
  if (!validLastRead) return false
  if (!isInteger(payload['unread_count']) || !isInteger(payload['mention_count'])) return false
  return onlyFields(payload, ['channel_id', 'last_read_at', 'unread_count', 'mention_count'])
}

function isReadStateUpdate(payload: Record<string, unknown>): boolean {
  if (!hasOwn(payload, 'channel_id')) return payload['refresh'] === true && onlyFields(payload, ['refresh'])
  if (!isIdentifier(payload['channel_id'])) return false
  if (hasOwn(payload, 'refresh')) return isRefreshReadState(payload)
  if (hasOwn(payload, 'notify_level')) return isNotifyLevelReadState(payload)
  return isStandardReadState(payload)
}

function isOptionalAvatarUrl(payload: Record<string, unknown>): boolean {
  return !hasOwn(payload, 'bot_avatar_url') || isString(payload['bot_avatar_url'], 512)
}

function isOptionalNoteEmbeds(embeds: unknown): boolean {
  return embeds === null || isArrayOf(embeds, isNoteEmbed)
}

function isInteractionResponse(payload: Record<string, unknown>): boolean {
  if (!isIdentifier(payload['channel_id']) || !isIdentifier(payload['bot_id'])) return false
  if (!isString(payload['bot_name'], 64) || !isString(payload['content'], 8_000)) return false
  if (!isOptionalAvatarUrl(payload) || !isOptionalNoteEmbeds(payload['embeds'])) return false
  return isTimestamp(payload['created_at'])
}

// Events whose payload is a JSON object, by event type.
const OBJECT_CHECKS = new Map<string, RecordCheck>([
  ['pong', payload => typeof payload['t'] === 'number' && Number.isFinite(payload['t'])],
  ['server_info', payload => isString(payload['version'], 64)
    && (!hasOwn(payload, 'screen_layers') || (isInteger(payload['screen_layers']) && payload['screen_layers'] >= 0 && payload['screen_layers'] <= MAX_SCREEN_LAYERS))
    && (!hasOwn(payload, 'camera_layers') || (isInteger(payload['camera_layers']) && payload['camera_layers'] >= 0 && payload['camera_layers'] <= MAX_SCREEN_LAYERS))],
  ['system_update', payload => isString(payload['version'], 64)],
  ['presence_update', payload => isIdentifier(payload['user_id']) && isPresence(payload['status'])
    && (!hasOwn(payload, 'activity') || isBotActivity(payload['activity']))],
  ['typing', roomUser],
  ['read_state', isReadStateUpdate],
  ['message_delete', payload => isIdentifier(payload['id']) && isIdentifier(payload['channel_id'])
    && (payload['parent_id'] === null || isIdentifier(payload['parent_id']))],
  ['thread_follow_updated', payload => isIdentifier(payload['thread_id']) && isIdentifier(payload['channel_id']) && typeof payload['following'] === 'boolean'],
  ['pins_changed', payload => isIdentifier(payload['channel_id']) && isIdentifier(payload['message_id']) && typeof payload['pinned'] === 'boolean'
    && isIdentifier(payload['actor_id']) && isString(payload['actor_name'], 128)],
  ['message_reaction', payload => isIdentifier(payload['message_id']) && (!hasOwn(payload, 'channel_id') || isIdentifier(payload['channel_id']))
    && isArrayOf(payload['reactions'], isChatReactionSummary)],
  ['poll_update', payload => isIdentifier(payload['message_id']) && isIdentifier(payload['channel_id']) && isChatPoll(payload['poll'])],
  ['interaction_response', isInteractionResponse],
  ['user_update', payload => hasOwn(payload, 'disabled')
    ? isIdentifier(payload['id']) && typeof payload['disabled'] === 'boolean' && onlyFields(payload, ['id', 'disabled'])
    : isAuthUser(payload)],
  ['member_timeout', payload => isIdentifier(payload['id']) && (payload['timeout_until'] === null || isTimestamp(payload['timeout_until']))
    && onlyFields(payload, ['id', 'timeout_until'])],
  ['kicked', payload => isString(payload['reason'], 400) && onlyFields(payload, ['reason'])],
  ['reminder_update', payload => isIdentifier(payload['id']) && (payload['state'] === 'created' || payload['state'] === 'due' || payload['state'] === 'removed')
    && onlyFields(payload, ['id', 'state'])],
  ['scheduled_message_update', payload => isIdentifier(payload['id']) && isIdentifier(payload['channel_id'])
    && ['created', 'updated', 'sent', 'failed', 'deleted'].includes(String(payload['state']))
    && (!hasOwn(payload, 'reason') || isString(payload['reason'], 64)) && onlyFields(payload, ['id', 'channel_id', 'state', 'reason'])],
  ['saved_updated', payload => isIdentifier(payload['message_id']) && typeof payload['saved'] === 'boolean' && onlyFields(payload, ['message_id', 'saved'])],
  ['block_updated', payload => isIdentifier(payload['user_id']) && typeof payload['blocked'] === 'boolean' && onlyFields(payload, ['user_id', 'blocked'])],
  ['webrtc_offer', payload => payload['type'] === 'offer' && isString(payload['sdp'], 262_144)],
  ['webrtc_candidate', isIceCandidate],
  ['voice_state_update', payload => isIdentifier(payload['channel_id']) && (
    payload['action'] === 'leave' ? isIdentifier(payload['user_id'])
      : payload['action'] === 'join' && isVoiceUser(payload['user']) && isTimestamp(payload['started_at']))],
  ['voice_rooms', payload => isDictionaryOf(payload['started'], isTimestamp) && isTimestamp(payload['now'])],
  ['user_stats', payload => isIdentifier(payload['user_id']) && isInteger(payload['voice_seconds'])],
  ['webrtc_media_state', payload => roomUser(payload) && typeof payload['screen'] === 'boolean' && typeof payload['camera'] === 'boolean'],
  ['screen_viewers', payload => roomUser(payload) && isArrayOf(payload['viewers'], isIdentifier)],
  ['screen_layers', payload => roomUser(payload) && isArrayOf(payload['layers'], isScreenLayer) && (payload['layers'] as unknown[]).length <= MAX_SCREEN_LAYERS],
  ['screen_quality', payload => roomUser(payload) && isInteger(payload['layer']) && payload['layer'] >= -1 && payload['layer'] < MAX_SCREEN_LAYERS
    && typeof payload['auto'] === 'boolean'],
  ['screen_layers_wanted', payload => isIdentifier(payload['channel_id']) && isArrayOf(payload['layers'], isLayerIndex)
    && (!hasOwn(payload, 'source') || payload['source'] === 'camera')],
  ['cameras_capped', payload => isIdentifier(payload['channel_id']) && isArrayOf(payload['user_ids'], isIdentifier)],
  ['voice_speaking', payload => roomUser(payload) && typeof payload['active'] === 'boolean'],
  ['voice_mute_state', payload => roomUser(payload) && typeof payload['muted'] === 'boolean' && typeof payload['deafened'] === 'boolean'],
  ['voice_kicked', payload => isIdentifier(payload['channel_id'])
    && (!hasOwn(payload, 'reason') || VOICE_KICK_REASONS.has(payload['reason'] as string))],
  ['talk_leader', payload => isIdentifier(payload['channel_id']) && (payload['leader_id'] === null || isIdentifier(payload['leader_id']))],
  ['talk_moved', payload => isIdentifier(payload['from_channel_id']) && isIdentifier(payload['channel_id'])],
  ...STAGE_EVENT_CHECKS,
  ...DM_EVENT_CHECKS,
  ['media_handshake', payload => isIdentifier(payload['channel_id']) && isIdentifier(payload['user_id'])
    && isString(payload['wire'], 48_000) && payload['wire'] !== ''],
  ...NOTICE_EVENT_CHECKS
])

// The check registered for an event type. The wire-controlled name is only
// compared, never used as a key or method name.
function checkFor<T>(checks: ReadonlyMap<string, T>, type: string): T | undefined {
  for (const [name, check] of checks) if (name === type) return check
  return undefined
}

export function isServerEvent(value: unknown): value is ServerEvent {
  if (!isRecord(value) || typeof value['type'] !== 'string') return false
  const payload = value['payload']
  const direct = checkFor(NON_OBJECT_CHECKS, value['type'])
  if (direct) return direct(payload)
  if (!isRecord(payload)) return false
  return checkFor(OBJECT_CHECKS, value['type'])?.(payload) ?? false
}

function isNoteEmbed(value: unknown): boolean {
  return isRecord(value) && (!hasOwn(value, 'title') || isString(value['title'], 1_000)) && (!hasOwn(value, 'description') || isString(value['description'], 8_000))
}

const VOICE_KICK_REASONS = new Set(['room_full', 'missing_permission', 'rules_not_accepted', 'talk_rate_limited', 'talk_unavailable', 'connection_lost', 'media_unavailable', 'joined_elsewhere', 'voice_taken_over'])

const SERVER_EVENT_TYPES = new Set<string>([
  'pong', 'server_info', 'system_update', 'presence_snapshot', 'presence_update', 'member_joined',
  'channels_changed', 'permissions_changed', 'roles_changed', 'emoji_changed', 'block_updated', 'saved_updated', 'reminder_update', 'scheduled_message_update', 'thread_follow_updated', 'dm_created', 'dm_member_added', 'dm_member_removed', 'dm_prefs_updated', 'dm_call_ring', 'dm_call_end', 'typing', 'read_state', 'message_create', 'message_update', 'message_delete',
  'message_reaction', 'poll_update', 'pins_changed', 'interaction_response', 'user_update', 'member_timeout', 'moderation_notice', 'kicked', 'webrtc_offer', 'webrtc_candidate', 'voice_snapshot',
  'voice_state_update', 'voice_rooms', 'user_stats', 'webrtc_media_state', 'screen_viewers',
  'screen_layers', 'screen_quality', 'screen_layers_wanted', 'cameras_capped', 'voice_speaking', 'voice_mute_state', 'voice_kicked', 'talk_leader', 'talk_moved', 'stage_hand', 'stage_talk_power', 'talk_reaction', 'media_handshake', 'forum_tags_changed', 'event_update', 'event_delete', 'event_reminder', 'community_updated'
])

/** Unknown event names stay ignorable for forward compatibility; malformed
 * known events never reach stores. No payload is included in the error. */
export function decodeServerEvent(value: unknown): ServerEvent | null {
  if (isRecord(value) && typeof value['type'] === 'string' && !SERVER_EVENT_TYPES.has(value['type'])) return null
  if (!isServerEvent(value)) throw new ContractError('WebSocket event') // i18n-ignore: schema name
  return value
}
