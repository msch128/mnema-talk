import { Channel, invoke } from '@tauri-apps/api/core'
import { trustedApp } from './native/trustedDesktop'

/**
 * OS notifications and the app badge, behind one adapter per host: the browser
 * (Notification API, Badging API) and the trusted desktop client, which shows
 * the toast and the taskbar/dock badge itself. The desktop commands are the
 * contract in doc/desktop-trusted-client.md; a client that does not announce
 * the `notify` capability gets the browser path, so old clients keep working.
 */
export type NotifyPermission = NotificationPermission | 'unsupported'

export interface OsNotification {
  title: string
  body: string
  channelId: string
  messageId: string
  /**
   * The user used the toast: bring them to the message. `join` says they chose
   * the toast's Join button (an incoming call, on a host that has one).
   */
  onClick: (action?: NotifyAction) => void
  /** An incoming call: the toast stays until it is used, and may offer Join. */
  call?: boolean
}

/** What a toast button can ask for besides opening the message. */
export type NotifyAction = 'join'

export interface NotifyAdapter {
  permission(): NotifyPermission
  /** Asks the platform again what it allows right now (the person may have changed it in the system settings). */
  refreshPermission(): Promise<NotifyPermission>
  requestPermission(): Promise<NotifyPermission>
  notify(n: OsNotification): void
  /** Withdraws the toast of a conversation or channel (a call that was answered elsewhere or stopped ringing). */
  close?(channelId: string): void
  setBadge(count: number): void
  /** Tells the host what is waiting in this instance (the multi-instance client shows it on its server rail). */
  reportUnread?(counts: UnreadCounts): void
}

/** What an instance has waiting: channels with unread messages that may notify, and mentions. */
export interface UnreadCounts {
  unread: number
  mentions: number
}

/** The toasts of this page that can still be withdrawn, by channel (the tag the browser replaces them by). */
const shown = new Map<string, Notification>()

export const browserNotify: NotifyAdapter = {
  permission: () => typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
  async refreshPermission() {
    return browserNotify.permission()
  },
  async requestPermission() {
    if (typeof Notification === 'undefined') return 'unsupported'
    try {
      return await Notification.requestPermission()
    } catch {
      return Notification.permission
    }
  },
  notify(n) {
    try {
      const toast = new Notification(n.title, { body: n.body, icon: '/favicon.svg', tag: n.channelId, requireInteraction: n.call === true })
      shown.set(n.channelId, toast)
      toast.onclick = () => {
        if (typeof window !== 'undefined') window.focus()
        n.onClick()
        toast.close()
      }
      toast.onclose = () => { if (shown.get(n.channelId) === toast) shown.delete(n.channelId) }
    } catch (e) {
      console.warn('Browser notification failed:', e)
    }
  },
  close(channelId) {
    shown.get(channelId)?.close()
    shown.delete(channelId)
  },
  setBadge(count) {
    if (typeof navigator === 'undefined') return
    const nav = navigator as Navigator & { setAppBadge?: (n: number) => Promise<void>, clearAppBadge?: () => Promise<void> }
    const done = count > 0 ? nav.setAppBadge?.(count) : nav.clearAppBadge?.()
    done?.catch(() => {})
  },
}

interface HostClick {
  channel_id?: unknown
  message_id?: unknown
  /** Which button of a call toast was used; absent for the toast itself. */
  action?: unknown
}

/** Toasts waiting for a click; the oldest are forgotten. */
const PENDING_LIMIT = 32
const BADGE_MAX = 999

function pendingKey(channelId: string, messageId: string): string {
  return `${channelId}/${messageId}`
}

function boundedCount(count: number): number {
  return Number.isFinite(count) ? Math.min(Math.max(0, Math.trunc(count)), BADGE_MAX) : 0
}

const PERMISSIONS: readonly NotifyPermission[] = ['granted', 'denied', 'default', 'unsupported']

function asPermission(value: unknown): NotifyPermission | null {
  return typeof value === 'string' && (PERMISSIONS as readonly string[]).includes(value) ? value as NotifyPermission : null
}

/**
 * The desktop client's own toasts and badge. The operating system owns the
 * permission: macOS asks the person once (when the page asks, or at the first
 * toast), Windows and Linux never ask. The state starts optimistic, so a host
 * that cannot report it (a client from before `desktop_notify_permission`)
 * behaves as before, and is replaced by what the host says.
 */
export function createHostNotify(hostShowsCalls: () => boolean = () => false): NotifyAdapter {
  const pending = new Map<string, (action?: NotifyAction) => void>()
  let opening: Promise<unknown> | null = null
  let state: NotifyPermission = 'granted'

  async function ask(command: string): Promise<NotifyPermission> {
    try {
      const answer = asPermission(await invoke(command))
      if (answer) state = answer
    } catch { /* an older host: keep what is known */ }
    return state
  }

  function click(event: HostClick) {
    const { channel_id: channelId, message_id: messageId } = event
    if (typeof channelId !== 'string' || typeof messageId !== 'string') return
    const key = pendingKey(channelId, messageId)
    const handler = pending.get(key)
    if (!handler) return
    pending.delete(key)
    handler(event.action === 'join' ? 'join' : undefined)
  }

  function listen(): Promise<unknown> {
    if (opening) return opening
    const channel = new Channel<HostClick>()
    channel.onmessage = click
    const started = invoke('desktop_notify_open', { onEvent: channel })
    opening = started
    started.catch(() => { if (opening === started) opening = null })
    return started
  }

  return {
    permission: () => state,
    refreshPermission: () => ask('desktop_notify_permission'),
    requestPermission: () => ask('desktop_notify_request_permission'),
    notify(n) {
      const key = pendingKey(n.channelId, n.messageId)
      pending.delete(key)
      pending.set(key, n.onClick)
      if (pending.size > PENDING_LIMIT) {
        const oldest = pending.keys().next().value
        if (oldest !== undefined) pending.delete(oldest)
      }
      // A host with the `notify_call` capability shows a call as a call; any other shows it as a message.
      const kind = hostShowsCalls() && n.call ? { kind: 'call' } : {}
      void listen()
        .then(() => invoke('desktop_notify', { title: n.title, body: n.body, channelId: n.channelId, messageId: n.messageId, ...kind }))
        .catch(() => { pending.delete(key) })
    },
    close(channelId) {
      if (hostShowsCalls()) invoke('desktop_notify_close', { channelId }).catch(() => {})
    },
    setBadge(count) {
      invoke('desktop_set_badge', { count: boundedCount(count) }).catch(() => {})
    },
    reportUnread({ unread, mentions }) {
      invoke('desktop_report_unread', { unread: boundedCount(unread), mentions: boundedCount(mentions) }).catch(() => {})
    },
  }
}

export const hostNotify = createHostNotify(() => trustedApp()?.capabilities.includes('notify_call') === true)
