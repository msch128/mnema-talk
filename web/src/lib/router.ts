import { ref } from 'vue'
import { isRecord } from '../types/validation'
import type { Channel } from '../types/domain'

export type Route =
  | { view: 'root' }
  | { view: 'admin'; tab: string }
  | { view: 'chat'; channelId: string; messageId?: string; threadId?: string }
  | { view: 'voice'; channelId: string; showChat?: boolean; messageId?: string; watching: boolean }
export interface RouteOptions { isAdmin?: boolean; channels?: Array<Pick<Channel, 'id' | 'type'>> }
export interface RouteResolution { redirect: string; reason: 'noAccess' | 'voiceNotFound' | 'channelNotFound' | null }

const PENDING_ROUTE_KEY = 'mnema_pending_route'

export function parseRoute(pathname = (typeof window !== 'undefined' ? window.location.pathname : '/')): Route {
  const parts = pathname.split('/').filter(Boolean)

  if (parts[0] === 'c' && parts[1]) {
    const channelId = parts[1]
    if (parts[2] === 'm' && parts[3]) {
      return { view: 'chat', channelId, messageId: parts[3] }
    }
    if (parts[2] === 't' && parts[3]) {
      return { view: 'chat', channelId, threadId: parts[3] }
    }
    return { view: 'chat', channelId }
  }

  if (parts[0] === 'v' && parts[1]) {
    const channelId = parts[1]
    if (parts[2] === 'chat') {
      // /v/:id/chat/m/:msgId: a message in the Talk's chat (search, notification).
      if (parts[3] === 'm' && parts[4]) {
        return { view: 'voice', channelId, showChat: true, messageId: parts[4], watching: true }
      }
      return { view: 'voice', channelId, showChat: true, watching: true }
    }
    return { view: 'voice', channelId, watching: true }
  }

  if (parts[0] === 'admin') {
    return { view: 'admin', tab: parts[1] || 'users' }
  }

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
  if (replace) {
    window.history.replaceState({ idx: historyIndex() }, '', path)
  } else if (window.location.pathname !== path) {
    window.history.pushState({ idx: historyIndex() + 1 }, '', path)
  }
  currentRoute.value = parseRoute(path)
}

/** Address of a message in a voice channel's chat. */
export function voiceMessagePath(channelId: string, messageId: string) {
  return `/v/${channelId}/chat/m/${messageId}`
}

/**
 * Routes a user may not open. Returns the path to redirect to, or null.
 * Non-admins never get the admin console, not even for a frame.
 */
export function guardRoute(route: Route | null | undefined, { isAdmin = false }: Pick<RouteOptions, 'isAdmin'> = {}): string | null {
  if (route?.view === 'admin' && !isAdmin) return '/'
  return null
}

/**
 * Decides whether a route can be shown as-is. Returns null when it can, else
 * { redirect, reason } where reason is a nav.* i18n key suffix (or null when
 * the redirect is only a correction, e.g. a text channel opened as /v/:id).
 */
export function resolveRoute(route: Route, { isAdmin = false, channels = [] }: RouteOptions = {}): RouteResolution | null {
  const denied = guardRoute(route, { isAdmin })
  if (denied) return { redirect: denied, reason: 'noAccess' }
  if ((route.view === 'chat' || route.view === 'voice') && route.channelId) {
    const ch = channels.find(c => c.id === route.channelId)
    if (!ch) return { redirect: '/', reason: route.view === 'voice' ? 'voiceNotFound' : 'channelNotFound' }
    if (route.view === 'voice' && ch.type !== 'voice') return { redirect: `/c/${ch.id}`, reason: null }
    if (route.view === 'chat' && ch.type === 'voice') {
      // A message link into a voice channel opens it in the Talk's chat.
      return { redirect: route.messageId ? voiceMessagePath(ch.id, route.messageId) : `/v/${ch.id}`, reason: null }
    }
  }
  return null
}

export const navigate = navigateTo
export const popRedirectRoute = consumePendingRoute

export function savePendingRoute() {
  if (typeof window === 'undefined') return
  const path = window.location.pathname
  if (path && path !== '/' && path !== '/login') {
    sessionStorage.setItem(PENDING_ROUTE_KEY, path)
  }
}

export function consumePendingRoute() {
  if (typeof window === 'undefined') return null
  const pending = sessionStorage.getItem(PENDING_ROUTE_KEY)
  if (pending) {
    sessionStorage.removeItem(PENDING_ROUTE_KEY)
    return pending
  }
  return null
}

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    currentRoute.value = parseRoute(window.location.pathname)
  })
}
