import { Channel, invoke } from '@tauri-apps/api/core'

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
  /** The user used the toast: bring them to the message. */
  onClick: () => void
}

export interface NotifyAdapter {
  permission(): NotifyPermission
  requestPermission(): Promise<NotifyPermission>
  notify(n: OsNotification): void
  setBadge(count: number): void
}

export const browserNotify: NotifyAdapter = {
  permission: () => typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
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
      const toast = new Notification(n.title, { body: n.body, icon: '/favicon.svg', tag: n.channelId })
      toast.onclick = () => {
        if (typeof window !== 'undefined') window.focus()
        n.onClick()
        toast.close()
      }
    } catch (e) {
      console.warn('Browser notification failed:', e)
    }
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
}

/** Toasts waiting for a click; the oldest are forgotten. */
const PENDING_LIMIT = 32
const BADGE_MAX = 999

function pendingKey(channelId: string, messageId: string): string {
  return `${channelId}/${messageId}`
}

/** The desktop client's own toasts and badge. Always permitted: the OS decides. */
export function createHostNotify(): NotifyAdapter {
  const pending = new Map<string, () => void>()
  let opening: Promise<unknown> | null = null

  function click(event: HostClick) {
    const { channel_id: channelId, message_id: messageId } = event
    if (typeof channelId !== 'string' || typeof messageId !== 'string') return
    const key = pendingKey(channelId, messageId)
    const handler = pending.get(key)
    if (!handler) return
    pending.delete(key)
    handler()
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
    permission: () => 'granted',
    requestPermission: () => Promise.resolve('granted'),
    notify(n) {
      const key = pendingKey(n.channelId, n.messageId)
      pending.delete(key)
      pending.set(key, n.onClick)
      if (pending.size > PENDING_LIMIT) {
        const oldest = pending.keys().next().value
        if (oldest !== undefined) pending.delete(oldest)
      }
      void listen()
        .then(() => invoke('desktop_notify', { title: n.title, body: n.body, channelId: n.channelId, messageId: n.messageId }))
        .catch(() => { pending.delete(key) })
    },
    setBadge(count) {
      invoke('desktop_set_badge', { count: Math.min(Math.max(0, Math.trunc(count)), BADGE_MAX) }).catch(() => {})
    },
  }
}

export const hostNotify = createHostNotify()
