import { invoke as tauriInvoke } from '@tauri-apps/api/core'
import { isWebDesktopSelector } from './desktopRuntime'
import { isTrustedDesktop, trustedApp, type TrustedApp } from './native/trustedDesktop'
import { browserNotify, hostNotify, type NotifyAdapter, type NotifyPermission, type OsNotification, type UnreadCounts } from './osNotify'
import { TrustedSocket } from './native/trustedSocket'

/** What the chat store needs from a live socket, whichever host opens it. */
export type PlatformSocket = WebSocket | TrustedSocket

/**
 * The one seam between the app and the host it runs in. Everything that
 * differs between a plain browser and the desktop client goes through here,
 * so the rest of the app stays platform-agnostic. The window globals and IPC
 * commands behind it are the contract with the desktop client
 * (doc/desktop-trusted-client.md) and are read nowhere else.
 */
export interface Platform {
  /** The desktop client's instance selector page. */
  isWebDesktopSelector(): boolean
  /** The app runs from the desktop client's verified web bundle. */
  isTrustedDesktop(): boolean
  /** The verified bundle announcement, or null. */
  trustedApp(): TrustedApp | null
  /** Opens the real-time socket of this host. */
  openSocket(url: string): PlatformSocket
  /** Gaming overlay surface the host rendered this page as, or null. */
  gamingSurface(): string | null
  /** Token that authorises the gaming bridge, or null outside the client. */
  gamingBridgeToken(): string | null
  /** Calls a host command of the desktop client. */
  invoke<T = unknown>(command: string, args?: Record<string, unknown>): Promise<T>
  /** Whether OS notifications may be shown. */
  notificationPermission(): NotifyPermission
  /** Asks the host what notifications may do right now (it can change outside the app). */
  refreshNotificationPermission(): Promise<NotifyPermission>
  /** Asks for notification permission; call only from a user gesture. */
  requestNotificationPermission(): Promise<NotifyPermission>
  /** Shows an OS notification; clicking it runs `onClick`. */
  notify(n: OsNotification): void
  /** Withdraws the toast of a conversation or channel, if it is still up (a call that stopped ringing). */
  closeNotification(channelId: string): void
  /** Count on the taskbar/dock/app icon; 0 clears it. */
  setBadge(count: number): void
  /**
   * What this instance has waiting, for the multi-instance desktop client's
   * server rail and combined badge. Feature-detected: only a host that
   * announces the `unread` capability gets it, and `supportsUnreadReport`
   * says whether that is the case. Additive, no `web_client_api` bump.
   */
  reportUnread(counts: UnreadCounts): void
  /** The host takes `reportUnread` (it then also sums the badge over its instances). */
  supportsUnreadReport(): boolean
}

function windowString(name: string): string | null {
  if (typeof window === 'undefined') return null
  const value: unknown = Reflect.get(window, name)
  return typeof value === 'string' ? value : null
}

/** The host shows toasts itself only when it announced it and this is its instance window. */
function notifier(): NotifyAdapter {
  const app = trustedApp()
  return app?.role === 'instance' && app.capabilities.includes('notify') ? hostNotify : browserNotify
}

const SOCKET_PATH = '/api/ws'

/**
 * What the host is asked to open. A client that announces `ws_query` relays the
 * one question the socket may carry (`?dm=1`: send conversation events too);
 * an older one only opens the plain path, and a socket URL it would refuse
 * outright must not reach it.
 */
function trustedSocketPath(url: string): string {
  try {
    const { pathname, search } = new URL(url)
    if (pathname === SOCKET_PATH && search === '?dm=1' && trustedApp()?.capabilities.includes('ws_query')) return `${SOCKET_PATH}${search}`
  } catch { /* not a URL: the plain path */ }
  return SOCKET_PATH
}

function unreadReporter(): NotifyAdapter | null {
  const app = trustedApp()
  return app?.role === 'instance' && app.capabilities.includes('unread') ? hostNotify : null
}

/** A browser tab, including the trusted bundle served from the client's own origin. */
export const browserPlatform: Platform = {
  isWebDesktopSelector,
  isTrustedDesktop,
  trustedApp,
  openSocket: url => isTrustedDesktop() ? new TrustedSocket(trustedSocketPath(url)) : new WebSocket(url),
  gamingSurface: () => windowString('__MNEMA_GAMING_SURFACE__'),
  gamingBridgeToken: () => windowString('__MNEMA_GAMING_BRIDGE__'),
  invoke: <T = unknown>(command: string, args?: Record<string, unknown>) => tauriInvoke<T>(command, args),
  notificationPermission: () => notifier().permission(),
  refreshNotificationPermission: () => notifier().refreshPermission(),
  requestNotificationPermission: () => notifier().requestPermission(),
  notify: n => notifier().notify(n),
  closeNotification: channelId => notifier().close?.(channelId),
  setBadge: count => notifier().setBadge(count),
  reportUnread: counts => unreadReporter()?.reportUnread?.(counts),
  supportsUnreadReport: () => unreadReporter()?.reportUnread !== undefined,
}

/** The platform of this page. */
export const platform: Platform = browserPlatform
