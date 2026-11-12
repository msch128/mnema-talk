// The shapes of read_state (internal/events/wire/messages.go): a read up to a
// time with its counts, the counts after a delete or an edit (with the
// channel's last_message_number), a request to read the counts again (one
// channel or all), and a notification level. Runtime check of the payload.
import type { ReadState } from './domain'
import { isChatNotifyLevel } from './rest'
import { hasOwn, isIdentifier, isInteger, isTimestamp, onlyFields } from './validation'

export type ReadStateUpdate =
  | Pick<ReadState, 'channel_id' | 'last_read_at' | 'unread_count' | 'mention_count'>
  | (Pick<ReadState, 'channel_id' | 'last_read_at' | 'unread_count' | 'mention_count'> & { last_message_number: number })
  | { channel_id: string; last_read_at: string; refresh: true }
  | { channel_id: string; refresh: true } | { refresh: true }
  | Pick<ReadState, 'channel_id' | 'notify_level'>

function isRefreshReadState(payload: Record<string, unknown>): boolean {
  if (payload['refresh'] !== true || (hasOwn(payload, 'last_read_at') && !isTimestamp(payload['last_read_at']))) return false
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
  if (hasOwn(payload, 'last_message_number') && !isInteger(payload['last_message_number'])) return false
  return onlyFields(payload, ['channel_id', 'last_read_at', 'unread_count', 'mention_count', 'last_message_number'])
}

export function isReadStateUpdate(payload: Record<string, unknown>): boolean {
  if (!hasOwn(payload, 'channel_id')) return payload['refresh'] === true && onlyFields(payload, ['refresh'])
  if (!isIdentifier(payload['channel_id'])) return false
  if (hasOwn(payload, 'refresh')) return isRefreshReadState(payload)
  if (hasOwn(payload, 'notify_level')) return isNotifyLevelReadState(payload)
  return isStandardReadState(payload)
}
