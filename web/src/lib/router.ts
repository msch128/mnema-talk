import { ref } from 'vue'
import { isRecord } from '../types/validation'
import { isInvitePath } from './inviteLink'
import { dmPath, talkPath, textPath, type PathChannel, type PathDm } from './routePaths'

export type Route =
  | { view: 'root' }
  | { view: 'admin'; tab: string }
  // channelRef is what the address says: a slug, a former slug or (legacy links) an id.
  | { view: 'chat'; channelRef: string; messageId?: string; threadId?: string; postRef?: string }
  | { view: 'voice'; channelRef: string; showChat?: boolean; messageId?: string; watching: boolean }
  // dmRef is a username (a conversation of two) or the short id of a group.
  | { view: 'dm'; dmRef: string; group: boolean; messageId?: string; threadId?: string }
export type ChannelRoute = Extract<Route, { view: 'chat' | 'voice' }>
export interface RouteChannel extends PathChannel { previous_slugs?: readonly string[] | undefined }
export interface RouteOptions { isAdmin?: boolean; channels?: RouteChannel[]; dms?: PathDm[] }
export interface RouteResolution { redirect: string; reason: 'noAccess' | 'voiceNotFound' | 'channelNotFound' | null }

/** A route whose channel is known by id: what the app applies. */
export type TargetRoute =
  | Exclude<Route, { view: 'chat' | 'voice' | 'dm' }>
  | { view: 'chat'; channelId: string; messageId?: string; threadId?: string; postRef?: string }
  | { view: 'voice'; channelId: string; showChat?: boolean; messageId?: string; watching: boolean }

const PENDING_ROUTE_KEY = 'mnema_pending_route'

function decode(part: string | undefined): string {
  if (!part) return ''
  try {
    return decodeURIComponent(part)
  } catch {
    return part
  }
}

/** The trailing /m/:id or /t/:id of a chat-like address, starting at parts[at]. */
function parseTail(parts: string[], at: number): { messageId?: string; threadId?: string } {
  if (parts[at] === 'm' && parts[at + 1]) return { messageId: parts[at + 1] as string }
  if (parts[at] === 't' && parts[at + 1]) return { threadId: parts[at + 1] as string }
  return {}
}

function parseChatRoute(parts: string[]): Route {
  const channelRef = decode(parts[1])
  if (parts[2] === 'p' && parts[3]) return { view: 'chat', channelRef, postRef: decode(parts[3]) }
  return { view: 'chat', channelRef, ...parseTail(parts, 2) }
}

function parseVoiceRoute(parts: string[]): Route {
  const channelRef = decode(parts[1])
  if (parts[2] !== 'chat') return { view: 'voice', channelRef, watching: true }
  // A message in the Talk's chat (search, notification): /v/:slug/chat/m/:msgId.
  if (parts[3] === 'm' && parts[4]) return { view: 'voice', channelRef, showChat: true, messageId: parts[4], watching: true }
  return { view: 'voice', channelRef, showChat: true, watching: true }
}

function parseDmRoute(parts: string[]): Route {
  if (parts[1] === 'g' && parts[2]) return { view: 'dm', dmRef: decode(parts[2]), group: true, ...parseTail(parts, 3) }
  return { view: 'dm', dmRef: decode(parts[1]), group: false, ...parseTail(parts, 2) }
}

export function parseRoute(pathname = (typeof window !== 'undefined' ? window.location.pathname : '/')): Route {
  const parts = pathname.split('/').filter(Boolean)
  if (parts[0] === 'c' && parts[1]) return parseChatRoute(parts)
  if (parts[0] === 'v' && parts[1]) return parseVoiceRoute(parts)
  if (parts[0] === 'dm' && parts[1]) return parseDmRoute(parts)
  if (parts[0] === 'admin') return { view: 'admin', tab: parts[1] || 'users' }
  return { view: 'root' }
}

export const currentRoute = ref(parseRoute())

// Each entry we push carries its depth in history.state, so "is there an
// in-app entry to go back to" is answerable without guessing from document.referrer.
function historyIndex() {
  const state: unknown = window.history.state
  const idx = isRecord(state) ? state.idx : undefined
  return typeof idx === 'number' && Number.isInteger(idx) ? idx : 0
}

/** True when the previous history entry was created by this app. */
export function canGoBackInApp() {
  return typeof window !== 'undefined' && historyIndex() > 0
}

export function navigateTo(path: string, opts: boolean | { replace?: boolean } = false) {
  if (typeof window === 'undefined') return
  const replace = typeof opts === 'boolean' ? opts : !!opts?.replace
  // A path the browser resolves to another origin (like "//host/x") makes history throw; show the start page instead.
  if (path.startsWith('//')) path = '/'
  try {
    if (replace) {
      window.history.replaceState({ idx: historyIndex() }, '', path)
    } else if (window.location.pathname !== path) {
      window.history.pushState({ idx: historyIndex() + 1 }, '', path)
    }
  } catch {
    path = '/'
  }
  currentRoute.value = parseRoute(path)
}

/** Address of a message in a voice channel's chat. */
export function voiceMessagePath(channel: Pick<PathChannel, 'id' | 'slug'>, messageId: string) {
  return talkPath(channel, { messageId })
}

/**
 * Routes a user may not open. Returns the path to redirect to, or null.
 * Non-admins never get the admin console, not even for a frame.
 */
export function guardRoute(route: Route | null | undefined, { isAdmin = false }: Pick<RouteOptions, 'isAdmin'> = {}): string | null {
  if (route?.view === 'admin' && !isAdmin) return '/'
  return null
}

const notFound = (route: Route): RouteResolution => ({ redirect: '/', reason: route.view === 'voice' ? 'voiceNotFound' : 'channelNotFound' })

/**
 * Decides whether a route can be shown as-is. Returns null when it can, else
 * { redirect, reason } where reason is a nav.* i18n key suffix (or null when
 * the redirect is only a correction: a legacy id or former slug rewritten to
 * the canonical address, a text channel opened as /v/:slug). A channel that
 * does not exist and one the member may not see give the same answer.
 */
export function resolveRoute(route: Route, { isAdmin = false, channels = [], dms = [] }: RouteOptions = {}): RouteResolution | null {
  const denied = guardRoute(route, { isAdmin })
  if (denied) return { redirect: denied, reason: 'noAccess' }
  if ((route.view === 'chat' || route.view === 'voice') && route.channelRef) return resolveChannelRoute(route, channels, dms)
  if (route.view === 'dm') return findDm(dms, route) ? null : notFound(route)
  return null
}

export function findChannel<C extends RouteChannel>(channels: readonly C[], ref: string): { channel: C; canonical: boolean } | null {
  const wanted = ref.toLowerCase()
  const bySlug = channels.find(c => c.slug === wanted)
  if (bySlug) return { channel: bySlug, canonical: true }
  const byOld = channels.find(c => c.previous_slugs?.includes(wanted))
  if (byOld) return { channel: byOld, canonical: false }
  const byId = channels.find(c => c.id === wanted)
  // A channel the server has no slug for yet is addressed by its id.
  return byId ? { channel: byId, canonical: !byId.slug } : null
}

function findDm(dms: readonly PathDm[], route: { dmRef: string; group: boolean }): PathDm | null {
  const ref = route.dmRef.toLowerCase()
  if (!route.group) return dms.find(d => !d.group && d.peer?.toLowerCase() === ref) ?? null
  const hits = dms.filter(d => d.id.startsWith(ref))
  return hits.length === 1 && ref.length >= 8 ? hits[0] ?? null : null
}

function resolveChannelRoute(route: ChannelRoute, channels: readonly RouteChannel[], dms: readonly PathDm[]): RouteResolution | null {
  const hit = findChannel(channels, route.channelRef)
  if (!hit) {
    // A conversation opened by id (an old link, a notification) moves to /dm/….
    const dm = route.view === 'chat' ? dms.find(d => d.id === route.channelRef.toLowerCase()) : undefined
    if (!dm) return notFound(route)
    return { redirect: dmPath(dm, dms, route), reason: null }
  }
  const ch = hit.channel
  if (route.view === 'voice' && ch.type !== 'voice') return { redirect: textPath(ch), reason: null }
  if (route.view === 'chat' && ch.type === 'voice') {
    // A message link into a voice channel opens it in the Talk's chat.
    return { redirect: route.messageId ? voiceMessagePath(ch, route.messageId) : talkPath(ch), reason: null }
  }
  if (hit.canonical) return null
  return { redirect: route.view === 'voice' ? talkPath(ch, { chat: !!route.showChat, messageId: route.messageId }) : chatPathOf(ch, route), reason: null }
}

function chatPathOf(ch: PathChannel, route: Extract<Route, { view: 'chat' }>): string {
  return route.postRef ? `/c/${ch.slug || ch.id}/p/${route.postRef}` : textPath(ch, route)
}

/** The route with its channel known by id, or null when the address names none of the member's channels. */
export function targetOf(route: Route, { channels = [], dms = [] }: Pick<RouteOptions, 'channels' | 'dms'> = {}): TargetRoute | null {
  if (route.view === 'chat' || route.view === 'voice') {
    const { channelRef, ...rest } = route
    const hit = findChannel(channels, channelRef)
    const dm = route.view === 'chat' && !hit ? dms.find(d => d.id === channelRef.toLowerCase()) : undefined
    const channelId = hit?.channel.id ?? dm?.id
    return channelId ? { ...rest, channelId } as TargetRoute : null
  }
  if (route.view === 'dm') {
    const dm = findDm(dms, route)
    if (!dm) return null
    return { view: 'chat', channelId: dm.id, ...(route.messageId ? { messageId: route.messageId } : {}), ...(route.threadId ? { threadId: route.threadId } : {}) }
  }
  return route
}

export const navigate = navigateTo
export const popRedirectRoute = consumePendingRoute

export function savePendingRoute() {
  if (typeof window === 'undefined') return
  const path = window.location.pathname
  if (path && path !== '/' && !path.startsWith('//') && path !== '/login' && !isInvitePath(path)) {
    try {
      sessionStorage.setItem(PENDING_ROUTE_KEY, path)
    } catch { /* storage is blocked: the link is simply not remembered */ }
  }
}

export function consumePendingRoute() {
  if (typeof window === 'undefined') return null
  try {
    const pending = sessionStorage.getItem(PENDING_ROUTE_KEY)
    if (pending) {
      sessionStorage.removeItem(PENDING_ROUTE_KEY)
      return pending.startsWith('//') ? null : pending
    }
  } catch { /* storage is blocked */ }
  return null
}

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    currentRoute.value = parseRoute(window.location.pathname)
  })
}
