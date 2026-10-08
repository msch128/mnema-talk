// Compact grouping: consecutive messages by the same author within a
// short window render without repeating avatar and name.
export const GROUP_WINDOW_MS = 7 * 60 * 1000
import type { Message } from '../types/domain'
type GroupableMessage = Partial<Pick<Message, 'user_id' | 'created_at' | 'reply_to_id' | 'reply_to'>>

function timeOf(msg: GroupableMessage | null | undefined): number {
  const t = msg?.created_at ? new Date(msg.created_at).getTime() : NaN
  return Number.isFinite(t) ? t : NaN
}

export function isContinuation(prev: GroupableMessage | null | undefined, msg: GroupableMessage | null | undefined, windowMs = GROUP_WINDOW_MS): boolean {
  if (!prev || !msg) return false
  // A reply shows its reference line above the header, so it always starts a group.
  if (msg.reply_to_id || msg.reply_to) return false
  if (!prev.user_id || prev.user_id !== msg.user_id) return false
  const a = timeOf(prev)
  const b = timeOf(msg)
  if (!Number.isFinite(a) || !Number.isFinite(b)) return false
  const diff = b - a
  return diff >= 0 && diff < windowMs
}

// Returns a Set of message ids that continue the previous message's group.
export function continuationIds(messages: Array<GroupableMessage & Pick<Message, 'id'>> | null | undefined, windowMs = GROUP_WINDOW_MS): Set<string> {
  const ids = new Set<string>()
  const list = messages || []
  for (let i = 1; i < list.length; i++) {
    const message = list[i]
    if (message && isContinuation(list[i - 1], message, windowMs)) ids.add(message.id)
  }
  return ids
}
