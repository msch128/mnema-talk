// The addresses of the app. A channel's address is its slug (/c/general,
// /v/lounge); the id stays the key everywhere else. Pure: no store, no
// window. A channel the client has no slug for yet (an old server, a channel
// just created) falls back to its id, which the router rewrites once the slug
// is known.

export interface PathChannel { id: string; type: string; slug?: string | undefined }

/** A conversation as the address sees it: peer is the other member of a 1:1 one. */
export interface PathDm { id: string; group: boolean; peer: string | null }

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const SHORT_ID = 8
const MAX_POST_SLUG = 50

const SPELLED: Record<string, string> = { ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss', æ: 'ae', œ: 'oe', ø: 'o', ł: 'l', đ: 'd', ð: 'd', þ: 'th' }

export function isUuid(value: string): boolean {
  return UUID.test(value)
}

/** Lowercase ASCII [a-z0-9-] with umlauts spelled out; the server's channel slugs follow the same rules. */
export function slugify(text: string, max = 60, fallback = 'channel'): string {
  const spelled = text.toLowerCase().replace(/[äöüßæœøłđðþ]/g, ch => SPELLED[ch] ?? ch)
  const plain = spelled.normalize('NFD').replace(/\p{Mn}/gu, '').replace(/[^a-z0-9]+/g, '-')
  const slug = plain.replace(/^-+|-+$/g, '').slice(0, max).replace(/-+$/g, '')
  return slug || fallback
}

export function shortId(id: string): string {
  return id.slice(0, SHORT_ID).toLowerCase()
}

/** What the address calls a channel. */
export function channelRef(channel: Pick<PathChannel, 'id' | 'slug'>): string {
  return channel.slug || channel.id
}

export interface ChannelTail { messageId?: string | undefined; threadId?: string | undefined }

function tailOf({ messageId, threadId }: ChannelTail): string {
  if (messageId) return `/m/${messageId}`
  if (threadId) return `/t/${threadId}`
  return ''
}

/** /c/:slug[/m/:messageId | /t/:threadId] */
export function textPath(channel: Pick<PathChannel, 'id' | 'slug'>, tail: ChannelTail = {}): string {
  return `/c/${channelRef(channel)}${tailOf(tail)}`
}

/** /v/:slug[/chat[/m/:messageId]] */
export function talkPath(channel: Pick<PathChannel, 'id' | 'slug'>, opts: { chat?: boolean; messageId?: string | undefined } = {}): string {
  const base = `/v/${channelRef(channel)}`
  if (opts.messageId) return `${base}/chat/m/${opts.messageId}`
  return opts.chat ? `${base}/chat` : base
}

/** The address of a channel by its kind: Talks open under /v, the rest under /c. */
export function channelPath(channel: PathChannel, tail: ChannelTail = {}): string {
  if (channel.type !== 'voice') return textPath(channel, tail)
  return talkPath(channel, { chat: !!tail.messageId, messageId: tail.messageId })
}

/** /c/:forum/p/:post-slug-:shortId; the full id route when another loaded post shares the short id. */
export function postPath(channel: Pick<PathChannel, 'id' | 'slug'>, post: { id: string; title?: string | undefined }, siblings: ReadonlyArray<{ id: string }> = []): string {
  const short = shortId(post.id)
  const clash = siblings.some(s => s.id !== post.id && shortId(s.id) === short)
  if (clash || !post.title) return textPath(channel, { threadId: post.id })
  return `/c/${channelRef(channel)}/p/${slugify(post.title, MAX_POST_SLUG, 'post')}-${short}`
}

/** The short id a post address ends in, or null. The full id works as well. */
export function postShortId(ref: string): string | null {
  if (isUuid(ref)) return shortId(ref)
  const m = /(?:^|-)([0-9a-f]{8})$/.exec(ref.toLowerCase())
  return m?.[1] ?? null
}

/** /dm/:username for a conversation of two, /dm/g/:shortId for a group. */
export function dmPath(dm: PathDm, all: ReadonlyArray<PathDm> = [], tail: ChannelTail = {}): string {
  // A member named "g" would read as the group prefix.
  if (!dm.group && dm.peer && dm.peer !== 'g') return `/dm/${encodeURIComponent(dm.peer)}${tailOf(tail)}`
  const short = shortId(dm.id)
  const clash = all.some(o => o.id !== dm.id && shortId(o.id) === short)
  return `/dm/g/${clash ? dm.id : short}${tailOf(tail)}`
}
