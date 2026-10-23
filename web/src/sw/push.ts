// Web Push in the service worker: showing a notification for a push, and
// opening the app at the message when it is clicked. The server composes the
// finished text (in the member's language), so nothing here is translated.

/** What the server sends (see internal/push/payload.go); unknown fields are ignored. */
export interface PushMessage {
  type: 'message' | 'test'
  title: string
  body: string
  tag: string
  timestamp: number
  channelId: string | null
  messageId: string | null
  parentId: string | null
  mention: boolean
}

/** What a click carries from the notification to the app. */
export interface OpenMessage {
  type: 'mnema:open-message'
  channel_id: string
  message_id: string
  parent_id: string | null
}

export interface NotificationLike {
  data?: unknown
  close(): void
}

export interface ClientLike {
  visibilityState?: string
  focus(): Promise<unknown>
  postMessage(message: unknown): void
}

export interface PushScope {
  registration: {
    showNotification(title: string, options: Record<string, unknown>): Promise<void>
    getNotifications(): Promise<unknown[]>
    pushManager: { subscribe(options: unknown): Promise<{ toJSON(): { endpoint?: string, keys?: Record<string, string> } }> }
  }
  clients: {
    matchAll(options: { type: 'window', includeUncontrolled: boolean }): Promise<ClientLike[]>
    openWindow(url: string): Promise<unknown>
  }
  navigator?: { setAppBadge?(count: number): Promise<void>, clearAppBadge?(): Promise<void> }
  fetch(input: Request | string, init?: RequestInit): Promise<Response>
}

export interface PushEventLike {
  data: { json(): unknown } | null
  waitUntil(work: Promise<unknown>): void
}

export interface ClickEventLike {
  notification: NotificationLike
  waitUntil(work: Promise<unknown>): void
}

export interface SubscriptionChangeEventLike {
  oldSubscription: { options: unknown } | null
  waitUntil(work: Promise<unknown>): void
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const MAX_TEXT = 512

function text(value: unknown): string | null {
  return typeof value === 'string' && value.length <= MAX_TEXT ? value : null
}

function id(value: unknown): string | null {
  return typeof value === 'string' && UUID.test(value) ? value.toLowerCase() : null
}

/** Reads a push payload; anything that is not one of ours is refused. */
export function parsePush(raw: unknown): PushMessage | null {
  if (typeof raw !== 'object' || raw === null) return null
  const p = raw as Record<string, unknown>
  const title = text(p['title'])
  const body = text(p['body'])
  if (p['v'] !== 1 || (p['type'] !== 'message' && p['type'] !== 'test') || title === null || body === null) return null
  return {
    type: p['type'],
    title,
    body,
    tag: text(p['tag']) ?? p['type'],
    timestamp: typeof p['ts'] === 'number' && Number.isFinite(p['ts']) ? p['ts'] : Date.now(),
    channelId: id(p['channel_id']),
    messageId: id(p['message_id']),
    parentId: id(p['parent_id']),
    mention: p['mention'] === true,
  }
}

function readPayload(event: PushEventLike): unknown {
  try {
    return event.data ? event.data.json() : null
  } catch {
    return null
  }
}

/** Notification options of a push. The tag is the channel: the page's own notification of the same message replaces it instead of doubling it. */
export function notificationOptions(message: PushMessage): Record<string, unknown> {
  return {
    body: message.body,
    tag: message.tag,
    renotify: true,
    icon: '/pwa-icon-192.png',
    badge: '/pwa-badge-96.png',
    timestamp: message.timestamp,
    data: { channel_id: message.channelId, message_id: message.messageId, parent_id: message.parentId },
  }
}

async function appIsOnScreen(scope: PushScope): Promise<boolean> {
  const windows = await scope.clients.matchAll({ type: 'window', includeUncontrolled: true })
  return windows.some(w => w.visibilityState === 'visible')
}

/** The app icon shows how many notifications wait; the page replaces it with the real unread count while it is open. */
async function updateBadge(scope: PushScope): Promise<void> {
  const nav = scope.navigator
  if (!nav) return
  try {
    const waiting = (await scope.registration.getNotifications()).length
    if (waiting > 0) await nav.setAppBadge?.(waiting)
    else await nav.clearAppBadge?.()
  } catch { /* a badge is a nicety */ }
}

/** A push arrived. Messages are not shown while the app is on screen (it shows them itself); the test push always is. */
export async function onPush(scope: PushScope, event: PushEventLike): Promise<void> {
  const message = parsePush(readPayload(event))
  if (message?.type === 'message' && await appIsOnScreen(scope)) return
  const shown = message ?? { type: 'message' as const, title: 'Mnema Talk', body: '', tag: 'mnema', timestamp: Date.now(), channelId: null, messageId: null, parentId: null, mention: false }
  await scope.registration.showNotification(shown.title, notificationOptions(shown))
  await updateBadge(scope)
}

/** Where a click leads: the message in its channel (a thread reply opens at its root). */
export function openTarget(data: unknown): { url: string, message: OpenMessage | null } {
  const d = typeof data === 'object' && data !== null ? data as Record<string, unknown> : {}
  const channel = id(d['channel_id'])
  const message = id(d['message_id'])
  if (!channel || !message) return { url: '/', message: null }
  const parent = id(d['parent_id'])
  return {
    url: `/c/${channel}/m/${parent ?? message}`,
    message: { type: 'mnema:open-message', channel_id: channel, message_id: message, parent_id: parent },
  }
}

/** A notification was clicked: focus the app (a tab on screen first) and tell it which message to show, or open it. */
export async function onNotificationClick(scope: PushScope, event: ClickEventLike): Promise<void> {
  event.notification.close()
  const target = openTarget(event.notification.data)
  const windows = await scope.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const window = windows.find(w => w.visibilityState === 'visible') ?? windows[0]
  if (window) {
    await window.focus()
    if (target.message) window.postMessage(target.message)
  } else {
    await scope.clients.openWindow(target.url)
  }
  await updateBadge(scope)
}

/** The browser replaced the subscription: subscribe again the same way and tell the server. */
export async function onSubscriptionChange(scope: PushScope, event: SubscriptionChangeEventLike): Promise<void> {
  if (!event.oldSubscription) return
  const fresh = (await scope.registration.pushManager.subscribe(event.oldSubscription.options)).toJSON()
  if (!fresh.endpoint || !fresh.keys?.['p256dh'] || !fresh.keys['auth']) return
  await scope.fetch('/api/push/subscriptions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify({ endpoint: fresh.endpoint, p256dh: fresh.keys['p256dh'], auth: fresh.keys['auth'], label: '' }),
  })
}
