// Notices to members and community content events: moderation notices, forum
// tag changes and community events (internal/moderation, internal/chat,
// internal/communityevents). Runtime checks by event name.
import { isCommunityeventsEventComment } from './rest'
import { hasOwn, isIdentifier, isInteger, isString, isTimestamp, onlyFields } from './validation'

const MODERATION_NOTICE_KINDS = new Set(['timeout', 'timeout_removed', 'ban', 'message_deleted', 'messages_deleted'])

function isValidModerationTiming(payload: Record<string, unknown>): boolean {
  if (hasOwn(payload, 'until') && !isTimestamp(payload['until'])) return false
  if (hasOwn(payload, 'duration_seconds') && !isInteger(payload['duration_seconds'])) return false
  return true
}

function isModerationNotice(payload: Record<string, unknown>): boolean {
  if (!MODERATION_NOTICE_KINDS.has(String(payload['kind']))) return false
  if (!isString(payload['reason'], 400)) return false
  if (hasOwn(payload, 'channel_name') && !isString(payload['channel_name'], 128)) return false
  if (hasOwn(payload, 'count') && !isInteger(payload['count'])) return false
  return isValidModerationTiming(payload)
}

/** Runtime checks of the notice and community events, by event name. */
export const NOTICE_EVENT_CHECKS: [string, (payload: Record<string, unknown>) => boolean][] = [
  ['moderation_notice', isModerationNotice],
  ['forum_tags_changed', payload => isIdentifier(payload['channel_id']) && onlyFields(payload, ['channel_id'])],
  ['community_updated', payload => isString(payload['community_name'], 120) && (!hasOwn(payload, 'community_icon') || isString(payload['community_icon'], 64))
    && onlyFields(payload, ['community_name', 'community_icon'])],
  ['event_delete', payload => isIdentifier(payload['id'])],
  ['event_reminder', payload => isIdentifier(payload['event_id']) && isString(payload['title'], 100)
    && isTimestamp(payload['start_time']) && (!hasOwn(payload, 'channel_id') || isIdentifier(payload['channel_id']))],
  ['event_comment', payload => (payload['action'] === 'created' || payload['action'] === 'updated' || payload['action'] === 'deleted')
    && isIdentifier(payload['event_id']) && isIdentifier(payload['comment_id'])
    && (!hasOwn(payload, 'comment') || isCommunityeventsEventComment(payload['comment']))],
  ['event_comment_notice', payload => isIdentifier(payload['event_id']) && isString(payload['title'], 100)
    && isString(payload['author_name'], 100) && isString(payload['excerpt'], 200)
    && (!hasOwn(payload, 'channel_id') || isIdentifier(payload['channel_id']))],
  ['event_signup_notice', payload => isIdentifier(payload['event_id']) && isString(payload['title'], 100)
    && (payload['kind'] === 'promoted' || payload['kind'] === 'removed' || payload['kind'] === 'waitlisted') && isString(payload['role_name'], 100)
    && isTimestamp(payload['start_time']) && (!hasOwn(payload, 'channel_id') || isIdentifier(payload['channel_id']))]
]
