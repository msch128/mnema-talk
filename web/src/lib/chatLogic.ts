// Pure helpers behind the chat store and message rendering, kept free of
// Vue/Pinia so they can be unit tested directly.
import { CODE_BLOCK_RE, INLINE_CODE_RE, SPOILER_RE, URL_RE } from './markdown'
import type { Message, User, NotifyLevel } from '../types/domain'

export function escapeRegExp(text: string): string {
  return String(text).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// Whether `content` contains an @mention of `username` (case-insensitive).
export function mentionsUser(content: string | null | undefined, username: string | null | undefined): boolean {
  if (!username || !content) return false
  return new RegExp('(^|[^A-Za-z0-9_.-])@' + escapeRegExp(username) + '($|[^A-Za-z0-9_.-])', 'i').test(content)
}

// Whether a new message should raise a desktop notification.
export function shouldNotify({ permission, level = 'all', isMention = false, hidden = false, isCurrentChannel = false }: { permission: NotificationPermission | 'unsupported'; level?: NotifyLevel; isMention?: boolean; hidden?: boolean; isCurrentChannel?: boolean }): boolean {
  if (permission !== 'granted') return false
  if (level === 'mute') return false
  if (level === 'mentions' && !isMention) return false
  // The user is looking at this very conversation: no need to interrupt.
  return hidden || !isCurrentChannel
}

// URLs worth a preview card: the links renderMarkdown makes, except those
// inside code or spoilers.
export function extractPreviewUrls(content: string | null | undefined, max = 3): string[] {
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
export function firstUnreadId(messages: Array<Pick<Message, 'id' | 'created_at' | 'user_id'>> | null | undefined, since: string | null | undefined, myId: string | undefined, hasMoreBefore = false): string | null {
  if (!since || !messages?.length) return null
  const cutoff = new Date(since).getTime()
  if (!Number.isFinite(cutoff)) return null
  const i = messages.findIndex(m => new Date(m.created_at).getTime() > cutoff && m.user_id !== myId)
  if (i < 0) return null
  // Everything loaded is newer: without the history above we can't tell where it starts.
  if (i === 0 && hasMoreBefore) return null
  return messages[i]?.id || null
}

// What the typing line says: null | { key, params } for $t.
export function typingLine(users: Array<Partial<Pick<User, 'display_name' | 'username'>>> | null | undefined) {
  const names = (users || []).map(u => u.display_name || u.username).filter(Boolean)
  if (!names.length) return null
  const first = names[0]
  const second = names[1]
  if (names.length === 1 && first) return { key: 'chat.isTyping', params: { name: first } }
  if (names.length === 2 && first && second) return { key: 'chat.twoTyping', params: { a: first, b: second } }
  return { key: 'chat.severalTyping', params: {} }
}

// Calls fn(key) at most once per `ms` for each key: the first call runs
// immediately, calls inside the window collapse into one trailing call.
export function createKeyedThrottle(fn: (key: string) => void | Promise<unknown>, ms: number) {
  const lastAt = new Map<string, number>()
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  function run(key: string) {
    lastAt.set(key, Date.now())
    fn(key)
  }
  function call(key: string) {
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
  function cancel(key: string) {
    const timer = timers.get(key)
    if (timer !== undefined) clearTimeout(timer)
    timers.delete(key)
  }
  return { call, cancel }
}
