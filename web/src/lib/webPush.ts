import { ref } from 'vue'
import { api } from './api'
import { isIosDevice, isStandalone, serviceWorkerAllowed } from './pwa'
import {
  decodePushConfig, decodePushSubscription, decodePushSubscriptions, decodePushTestResult,
  type PushConfig, type PushSubscription, type PushTestResult,
} from '../types/rest'

/**
 * Web Push on this device: subscribing the browser to the server's push
 * service key, listing and removing devices, and receiving the service
 * worker's "open this message" after a click. The server never sees more of
 * the subscription than the browser gives it, and the worker shows what the
 * server sends (see internal/push).
 */

/** Why push cannot be turned on here, or that it can. */
export type PushAvailability = 'ready' | 'needs-install' | 'unsupported'

/** Whether this browser can receive push: it needs a service worker, the Push API and notifications. iPhone and iPad only for the installed app. */
export function pushAvailability(): PushAvailability {
  const capable = serviceWorkerAllowed() && typeof window !== 'undefined' && 'PushManager' in window && 'Notification' in window
  if (capable) return 'ready'
  return isIosDevice() && !isStandalone() ? 'needs-install' : 'unsupported'
}

/** True while this device has push on and the permission still stands. The page then leaves background notifications to the worker. */
export const pushOnThisDevice = ref(false)

function keyBytes(key: string): Uint8Array<ArrayBuffer> {
  const padded = key.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(key.length / 4) * 4, '=')
  return Uint8Array.from(atob(padded), c => c.charCodeAt(0))
}

/** The same fingerprint the server gives a device: the first 32 hex characters of the endpoint's SHA-256. */
export async function endpointFingerprint(endpoint: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint))
  return [...new Uint8Array(digest)].map(b => b.toString(16).padStart(2, '0')).join('').slice(0, 32)
}

const BROWSERS: ReadonlyArray<readonly [RegExp, string]> = [
  [/Edg\//, 'Edge'], [/OPR\//, 'Opera'], [/Firefox\//, 'Firefox'], [/Chrome\//, 'Chrome'], [/Safari\//, 'Safari'],
]
const SYSTEMS: ReadonlyArray<readonly [RegExp, string]> = [
  [/Android/, 'Android'], [/iPhone|iPad|iPod/, 'iOS'], [/Windows/, 'Windows'], [/Mac OS X|Macintosh/, 'macOS'], [/Linux|X11/, 'Linux'],
]

function firstMatch(table: ReadonlyArray<readonly [RegExp, string]>, ua: string): string {
  return table.find(([pattern]) => pattern.test(ua))?.[1] ?? ''
}

/** A short name for this device in the list, like "Firefox on Linux". */
export function deviceLabel(ua = typeof navigator === 'undefined' ? '' : navigator.userAgent): string {
  const browser = firstMatch(BROWSERS, ua)
  const system = firstMatch(SYSTEMS, ua)
  return (browser && system ? `${browser} on ${system}` : browser || system).slice(0, 64)
}

/** The worker's registration once it is active; null when none was registered (a development build, or registration failed). */
async function registration(): Promise<ServiceWorkerRegistration | null> {
  return await navigator.serviceWorker.getRegistration('/') ? navigator.serviceWorker.ready : null
}

/** This browser's push subscription, if it has one. */
export async function currentSubscription(): Promise<globalThis.PushSubscription | null> {
  if (pushAvailability() !== 'ready') return null
  return (await registration())?.pushManager.getSubscription() ?? null
}

export async function loadConfig(): Promise<PushConfig> {
  return api('/api/push/config', { decode: decodePushConfig })
}

/** quiet: a refused session is not reported to the app (used while signing out). */
export async function loadDevices(quiet = false): Promise<PushSubscription[]> {
  const reply = await api('/api/push/subscriptions', { decode: decodePushSubscriptions, ...(quiet ? { shouldNotifyUnauthorized: () => false } : {}) })
  return reply.subscriptions
}

/** The server's entry for this browser, if it knows it. */
export async function thisDevice(devices: readonly PushSubscription[]): Promise<PushSubscription | null> {
  const sub = await currentSubscription()
  if (!sub) return null
  const fingerprint = await endpointFingerprint(sub.endpoint)
  return devices.find(d => d.fingerprint === fingerprint) ?? null
}

/** Re-reads whether this device receives push; call after sign-in and after changing it. */
export async function refreshPushState(): Promise<boolean> {
  try {
    const known = pushAvailability() === 'ready' && Notification.permission === 'granted' && !!(await thisDevice(await loadDevices()))
    pushOnThisDevice.value = known
    return known
  } catch {
    pushOnThisDevice.value = false
    return false
  }
}

export type EnableResult = 'enabled' | 'denied' | 'unavailable'

/** Turns push on for this browser: asks for permission (call from a click), subscribes and registers the device with the server. */
export async function enablePush(config: PushConfig): Promise<EnableResult> {
  if (pushAvailability() !== 'ready' || !config.enabled || !config.public_key) return 'unavailable'
  const worker = await registration()
  if (!worker) return 'unavailable'
  if (await Notification.requestPermission() !== 'granted') return 'denied'
  const manager = worker.pushManager
  const key = keyBytes(config.public_key)
  let sub = await manager.getSubscription()
  if (sub && !sameKey(sub, key)) {
    await sub.unsubscribe() // made for another server key: useless now
    sub = null
  }
  sub ??= await manager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
  const json = sub.toJSON()
  await api('/api/push/subscriptions', {
    method: 'POST',
    json: { endpoint: json.endpoint ?? sub.endpoint, p256dh: json.keys?.['p256dh'] ?? '', auth: json.keys?.['auth'] ?? '', label: deviceLabel() },
    decode: decodePushSubscription,
  })
  pushOnThisDevice.value = true
  return 'enabled'
}

function sameKey(sub: globalThis.PushSubscription, key: Uint8Array): boolean {
  const have = sub.options.applicationServerKey
  if (!have) return false
  const bytes = new Uint8Array(have)
  return bytes.length === key.length && bytes.every((b, i) => b === key[i])
}

/** Removes a device from the account. When it is this browser, its subscription ends too. */
export async function removeDevice(device: PushSubscription, quiet = false): Promise<void> {
  const mine = await thisDevice([device])
  await api(`/api/push/subscriptions/${device.id}`, { method: 'DELETE', ...(quiet ? { shouldNotifyUnauthorized: () => false } : {}) })
  if (mine) {
    await (await currentSubscription())?.unsubscribe()
    pushOnThisDevice.value = false
  }
}

export async function sendTestPush(): Promise<PushTestResult> {
  return api('/api/push/test', { method: 'POST', decode: decodePushTestResult })
}

export async function setPreview(on: boolean): Promise<void> {
  await api('/api/push/preferences', { method: 'PUT', json: { preview: on } })
}

const SIGN_OUT_WAIT_MS = 2000

/**
 * Ends push for this browser before the account signs out, so the next person
 * who signs in here is not sent the previous account's messages. Never throws
 * and never holds sign-out up for long.
 */
export async function releaseThisDevice(): Promise<void> {
  if (pushAvailability() !== 'ready') return
  const work = (async () => {
    const device = await thisDevice(await loadDevices(true))
    if (device) await removeDevice(device, true)
  })().catch(() => {})
  await Promise.race([work, new Promise(resolve => setTimeout(resolve, SIGN_OUT_WAIT_MS))])
  pushOnThisDevice.value = false
}

/** What the service worker posts after a click on a notification. */
export interface OpenRequest {
  channel_id: string
  message_id: string
  parent_id: string | null
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

function uuidOrNull(value: unknown): string | null {
  return typeof value === 'string' && UUID.test(value) ? value : null
}

/** Reads the worker's message; anything else is ignored. */
export function parseOpenRequest(data: unknown): OpenRequest | null {
  if (typeof data !== 'object' || data === null) return null
  const d = data as Record<string, unknown>
  const channel = uuidOrNull(d['channel_id'])
  const message = uuidOrNull(d['message_id'])
  if (d['type'] !== 'mnema:open-message' || !channel || !message) return null
  return { channel_id: channel, message_id: message, parent_id: uuidOrNull(d['parent_id']) }
}

/** Calls open for every click the service worker reports; returns a function that stops listening. */
export function listenForOpenRequests(open: (request: OpenRequest) => void): () => void {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return () => {}
  const handler = (event: MessageEvent) => {
    const request = parseOpenRequest(event.data)
    if (request) open(request)
  }
  navigator.serviceWorker.addEventListener('message', handler)
  return () => navigator.serviceWorker.removeEventListener('message', handler)
}
