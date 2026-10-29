// Pure helpers behind the chat store and message rendering, kept free of
// Vue/Pinia so they can be unit tested directly.
import { previewLinks } from './markdown'
import type { Message, User, NotifyLevel } from '../types/domain'

export function escapeRegExp(text: string): string {
  return String(text).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

const mentionPatterns = new Map<string, RegExp>()

// Like the server (internal/mention): "@name" at the start or after a character
// that cannot be part of a username, with a trailing '.' or '-' (end of a
// sentence) not part of the name.
function mentionPattern(username: string): RegExp {
  let pattern = mentionPatterns.get(username)
  if (!pattern) {
    pattern = new RegExp('(^|[^A-Za-z0-9_.-])@' + escapeRegExp(username) + '[.-]*($|[^A-Za-z0-9_.-])', 'i')
    if (mentionPatterns.size >= 64) mentionPatterns.clear()
    mentionPatterns.set(username, pattern)
  }
  return pattern
}

// Whether `content` contains an @mention of `username` (case-insensitive).
export function mentionsUser(content: string | null | undefined, username: string | null | undefined): boolean {
  if (!username || !content) return false
  return mentionPattern(username).test(content)
}

// Whether a new message should raise a desktop notification. A muted channel
// stays silent except for a message addressed to the member (@name, their role
// or a reply), like on Discord; @all and @here do not break a mute.
export function shouldNotify({ permission, level = 'all', isMention = false, isDirectMention = false, hidden = false, isCurrentChannel = false }: { permission: NotificationPermission | 'unsupported'; level?: NotifyLevel; isMention?: boolean; isDirectMention?: boolean; hidden?: boolean; isCurrentChannel?: boolean }): boolean {
  if (permission !== 'granted') return false
  if (level === 'mute' && !isDirectMention) return false
  if (level === 'mentions' && !isMention) return false
  // The user is looking at this very conversation: no need to interrupt.
  return hidden || !isCurrentChannel
}

// URLs worth a preview card: the links renderMarkdown shows as links, except
// those inside code or spoilers and <autolinks> that opt out of previews.
export function extractPreviewUrls(content: string | null | undefined, max = 3): string[] {
  return [...new Set(previewLinks(content))].slice(0, max)
}

type UnreadMessage = Pick<Message, 'id' | 'created_at' | 'user_id'>

function isUnreadCandidate(m: UnreadMessage, cutoff: number, myId?: string): boolean {
  if (m.user_id === myId) return false
  return new Date(m.created_at).getTime() > cutoff
}

// Id of the first message from someone else newer than `since` (ISO string):
// where the "new since" divider goes. Null when unknown or nothing is new.
export function firstUnreadId(
  messages: UnreadMessage[] | null | undefined,
  since: string | null | undefined,
  myId: string | undefined,
  hasMoreBefore = false
): string | null {
  if (!since || !messages || messages.length === 0) return null
  const cutoff = new Date(since).getTime()
  if (!Number.isFinite(cutoff)) return null

  const i = messages.findIndex(m => isUnreadCandidate(m, cutoff, myId))
  if (i < 0) return null
  // Everything loaded is newer: without the history above we can't tell where it starts.
  if (i === 0 && hasMoreBefore) return null
  return messages[i]?.id ?? null
}

function extractTypingNames(users: Array<Partial<Pick<User, 'display_name' | 'username'>>> | null | undefined): string[] {
  if (!users) return []
  const names: string[] = []
  for (const u of users) {
    const name = u.display_name || u.username
    if (name) names.push(name)
  }
  return names
}

// What the typing line says: null | { key, params } for $t.
export function typingLine(users: Array<Partial<Pick<User, 'display_name' | 'username'>>> | null | undefined) {
  const names = extractTypingNames(users)
  if (names.length === 0) return null
  if (names.length === 1) return { key: 'chat.isTyping', params: { name: names[0] } }
  if (names.length === 2) return { key: 'chat.twoTyping', params: { a: names[0], b: names[1] } }
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
  function reset() {
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    lastAt.clear()
  }
  return { call, cancel, reset }
}
