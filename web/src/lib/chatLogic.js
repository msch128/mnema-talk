// Pure helpers behind the chat store and message rendering, kept free of
// Vue/Pinia so they can be unit tested directly.
import { CODE_BLOCK_RE, INLINE_CODE_RE, SPOILER_RE, URL_RE } from './markdown'

export function escapeRegExp(text) {
  return String(text).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// Whether `content` contains an @mention of `username` (case-insensitive).
export function mentionsUser(content, username) {
  if (!username || !content) return false
  return new RegExp('(^|[^A-Za-z0-9_.-])@' + escapeRegExp(username) + '($|[^A-Za-z0-9_.-])', 'i').test(content)
}

// Whether a new message should raise a desktop notification.
export function shouldNotify({ permission, level = 'all', isMention = false, hidden = false, isCurrentChannel = false }) {
  if (permission !== 'granted') return false
  if (level === 'mute') return false
  if (level === 'mentions' && !isMention) return false
  // The user is looking at this very conversation: no need to interrupt.
  return hidden || !isCurrentChannel
}

// URLs worth a preview card: the links renderMarkdown makes, except those
// inside code or spoilers.
export function extractPreviewUrls(content, max = 3) {
  if (!content) return []
  const plain = content
    .replace(CODE_BLOCK_RE, ' ')
    .replace(INLINE_CODE_RE, ' ')
    .replace(SPOILER_RE, ' ')
  const matches = plain.match(URL_RE)
  if (!matches) return []
  return [...new Set(matches)].slice(0, max)
}

// Id of the first message from someone else newer than `since` (ISO string):
// where the "new since" divider goes. Null when unknown or nothing is new.
export function firstUnreadId(messages, since, myId, hasMoreBefore = false) {
  if (!since || !messages?.length) return null
  const cutoff = new Date(since).getTime()
  if (!Number.isFinite(cutoff)) return null
  const i = messages.findIndex(m => new Date(m.created_at).getTime() > cutoff && m.user_id !== myId)
  if (i < 0) return null
  // Everything loaded is newer: without the history above we can't tell where it starts.
  if (i === 0 && hasMoreBefore) return null
  return messages[i].id
}

// What the typing line says: null | { key, params } for $t.
export function typingLine(users) {
  const names = (users || []).map(u => u.display_name || u.username).filter(Boolean)
  if (!names.length) return null
  if (names.length === 1) return { key: 'chat.isTyping', params: { name: names[0] } }
  if (names.length === 2) return { key: 'chat.twoTyping', params: { a: names[0], b: names[1] } }
  return { key: 'chat.severalTyping', params: {} }
}

// Calls fn(key) at most once per `ms` for each key: the first call runs
// immediately, calls inside the window collapse into one trailing call.
export function createKeyedThrottle(fn, ms) {
  const lastAt = new Map()
  const timers = new Map()
  function run(key) {
    lastAt.set(key, Date.now())
    fn(key)
  }
  function call(key) {
    if (timers.has(key)) return
    const wait = ms - (Date.now() - (lastAt.get(key) ?? -Infinity))
    if (wait <= 0) {
      run(key)
      return
    }
    timers.set(key, setTimeout(() => {
      timers.delete(key)
      run(key)
    }, wait))
  }
  function cancel(key) {
    clearTimeout(timers.get(key))
    timers.delete(key)
  }
  return { call, cancel }
}
