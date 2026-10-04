// Discord "cozy" grouping: consecutive messages by the same author within a
// short window render without repeating avatar and name.
export const GROUP_WINDOW_MS = 7 * 60 * 1000

function timeOf(msg) {
  const t = msg?.created_at ? new Date(msg.created_at).getTime() : NaN
  return Number.isFinite(t) ? t : NaN
}

export function isContinuation(prev, msg, windowMs = GROUP_WINDOW_MS) {
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
export function continuationIds(messages, windowMs = GROUP_WINDOW_MS) {
  const ids = new Set()
  const list = messages || []
  for (let i = 1; i < list.length; i++) {
    if (isContinuation(list[i - 1], list[i], windowMs)) ids.add(list[i].id)
  }
  return ids
}
