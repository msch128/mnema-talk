// Direct message events (internal/chat/dm*.go) and the calls that ring a
// conversation (internal/chat/dm_calls.go). Runtime checks by event name.
import type { ChatDMCall } from './rest'
import { isChatNotifyLevel } from './rest'
import { isIdentifier, isInteger, isRecord, isString, isTimestamp, isArrayOf, onlyFields, hasOwn } from './validation'

/** A member of a direct message conversation (the server's DirectMessage). */
export interface DirectMessageMember { id: string; username: string; display_name: string; avatar_url?: string }
/** A direct message conversation as GET /api/dms returns it. */
export interface DirectMessageRef {
  id: string
  group: boolean
  members: DirectMessageMember[]
  created_at: string
  last_message_at: string | null
  unread_count: number
  mention_count: number
  last_read_at: string | null
  notify_level: 'all' | 'mentions' | 'mute' | 'inherit'
  can_send: boolean; hidden: boolean; favourite_position: number | null
  call?: ChatDMCall
}

/** A Talk was started from, or shared into, a conversation: everyone but the caller has an incoming call. */
export interface DmCallRing {
  conversation_id: string
  talk_id: string
  message_id: string
  caller_id: string
  talk_name: string
  ring_seconds: number
}
export type DmCallEndReason = 'answered' | 'cancelled' | 'timeout' | 'ended'
/** The call stopped ringing (`active`: somebody is still in the Talk) or is over. */
export interface DmCallEnd {
  conversation_id: string
  talk_id: string
  reason: DmCallEndReason
  active: boolean
  /** Who answered (reason answered). */
  user_id?: string
  /** The "missed call" message an unanswered call left behind. */
  message_id?: string
}

const CALL_END_REASONS = new Set<string>(['answered', 'cancelled', 'timeout', 'ended'])

function isDirectMessageMember(value: unknown): value is DirectMessageMember {
  return isRecord(value) && isIdentifier(value['id']) && isString(value['username'], 128) && isString(value['display_name'], 128)
}

function isDmCall(payload: Record<string, unknown>): boolean {
  return isIdentifier(payload['conversation_id']) && isIdentifier(payload['talk_id'])
}

function isDmCallRing(payload: Record<string, unknown>): boolean {
  return isDmCall(payload) && isIdentifier(payload['message_id']) && isIdentifier(payload['caller_id'])
    && isString(payload['talk_name'], 256) && isInteger(payload['ring_seconds']) && payload['ring_seconds'] >= 0
}

function isDmCallEnd(payload: Record<string, unknown>): boolean {
  return isDmCall(payload) && CALL_END_REASONS.has(String(payload['reason'])) && typeof payload['active'] === 'boolean'
    && (!hasOwn(payload, 'user_id') || isIdentifier(payload['user_id']))
    && (!hasOwn(payload, 'message_id') || isIdentifier(payload['message_id']))
}

function isDirectMessageRef(payload: Record<string, unknown>): boolean {
  return isIdentifier(payload['id']) && typeof payload['group'] === 'boolean' && isArrayOf(payload['members'], isDirectMessageMember)
    && isTimestamp(payload['created_at']) && (payload['last_message_at'] === null || isTimestamp(payload['last_message_at']))
    && isInteger(payload['unread_count']) && isInteger(payload['mention_count'])
    && (payload['last_read_at'] === null || isTimestamp(payload['last_read_at']))
    && isChatNotifyLevel(payload['notify_level']) && typeof payload['can_send'] === 'boolean' && typeof payload['hidden'] === 'boolean'
    && (payload['favourite_position'] === null || isInteger(payload['favourite_position']))
}

/** Runtime checks of the direct message events, by event name. */
export const DM_EVENT_CHECKS: [string, (payload: Record<string, unknown>) => boolean][] = [
  ['dm_created', isDirectMessageRef],
  ['dm_member_added', payload => isIdentifier(payload['dm_id']) && isDirectMessageMember(payload['member'])],
  ['dm_member_removed', payload => isIdentifier(payload['dm_id']) && isDirectMessageMember(payload['member'])],
  ['dm_prefs_updated', payload => (payload['dm_id'] === null || isIdentifier(payload['dm_id'])) && onlyFields(payload, ['dm_id'])],
  ['dm_call_ring', isDmCallRing],
  ['dm_call_end', isDmCallEnd]
]
