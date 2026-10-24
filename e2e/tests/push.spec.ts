import type { Browser, BrowserContext, Page, Worker } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// Web Push in a real Chromium against the real binary. Chromium's own push
// service (FCM) is not reachable from a test, so two things are stood in for:
// the browser's PushManager hands out a made-up subscription (a real P-256 key
// and a push address that resolves nowhere), and a push is delivered to the
// service worker through the DevTools protocol. Encryption and delivery to a
// push service are covered by the Go tests (RFC 8291 vectors, a fake push
// service that decrypts what it receives).

// Chromium's old headless shell reports every notification permission as
// denied; the full browser in its new headless mode honours the grant.
test.use({ channel: 'chromium' })

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const ENDPOINT = 'https://push.example.test/wpush/e2e-device'

// The suite shares the login budget of one address, so this file signs in once
// and its tests start from that session.
type Session = Awaited<ReturnType<BrowserContext['storageState']>>
let session: Session

// The specs drive a Chromium-only push stand-in (they launch Chromium themselves).
test.skip(({ browserName }) => browserName !== 'chromium', 'Chromium only')

test.beforeAll(async ({ browser }) => {
  const context = await browser.newContext({ serviceWorkers: 'allow' })
  const page = await context.newPage()
  await signIn(page)
  session = await context.storageState()
  await context.close()
})

async function signedInContext(browser: Browser, permissions: string[] = []): Promise<BrowserContext> {
  return browser.newContext({ permissions, storageState: session, serviceWorkers: 'allow' })
}

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

/** Replaces Chromium's PushManager with one that needs no push service. */
async function fakePushManager(context: BrowserContext) {
  await context.addInitScript((endpoint) => {
    let current: unknown = null
    const bytes = (n: number, fill: number) => Uint8Array.from({ length: n }, (_, i) => (i * fill + 7) % 251)
    const b64url = (b: Uint8Array) => btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    Object.defineProperty(PushManager.prototype, 'getSubscription', { configurable: true, value: async () => current })
    Object.defineProperty(PushManager.prototype, 'subscribe', {
      configurable: true,
      value: async (options: { applicationServerKey: BufferSource }) => {
        const keys = (async () => {
          const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits'])
          return new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey))
        })()
        const p256dh = b64url(await keys)
        const auth = b64url(bytes(16, 5))
        const sub = {
          endpoint,
          options: { applicationServerKey: options.applicationServerKey instanceof ArrayBuffer ? options.applicationServerKey : (options.applicationServerKey as ArrayBufferView).buffer },
          unsubscribe: async () => { current = null; return true },
          toJSON: () => ({ endpoint, expirationTime: null, keys: { p256dh, auth } }),
        }
        current = sub
        return sub
      },
    })
  }, ENDPOINT)
}

async function openAppDialog(page: Page) {
  await page.getByTestId('account-menu-button').click()
  await page.getByTestId('account-app').click()
  await expect(page.getByRole('dialog', { name: 'App & Benachrichtigungen' })).toBeVisible()
}

test('a member turns push on and off for this device', async ({ browser }) => {
  const context = await signedInContext(browser, ['notifications'])
  await fakePushManager(context)
  const page = await context.newPage()
  try {
    await page.goto('/')
    await page.evaluate(async () => { await navigator.serviceWorker.ready })
    await openAppDialog(page)
    const section = page.getByTestId('push-section')

    await expect(section.getByTestId('push-state')).toContainText('Lass dich auf diesem Gerät benachrichtigen')
    await section.getByTestId('push-enable').click()
    await expect(section.getByTestId('push-device')).toHaveCount(1)
    await expect(section.getByTestId('push-device')).toContainText('Dieses Gerät')

    const list = await apiFetch(page, 'GET', '/api/push/subscriptions') as { status: number, json: { subscriptions: Array<{ label: string, fingerprint: string }> } }
    expect(list.status).toBe(200)
    expect(list.json.subscriptions).toHaveLength(1)
    expect(JSON.stringify(list.json)).not.toContain('push.example.test')

    // The message text can be switched off, and the choice is kept on the server.
    await section.getByTestId('push-preview').click()
    await expect.poll(async () => ((await apiFetch(page, 'GET', '/api/push/config')).json as { preview: boolean }).preview).toBe(false)

    // A test push goes to the made-up address, which resolves nowhere: the
    // server reports it as not delivered instead of failing.
    await section.getByTestId('push-test').click()
    await expect(page.getByText('Die Testbenachrichtigung konnte nicht zugestellt werden. Versuche es noch einmal.')).toBeVisible()

    await section.getByTestId('push-disable').click()
    await expect(section.getByTestId('push-enable')).toBeVisible()
    await expect.poll(async () => ((await apiFetch(page, 'GET', '/api/push/subscriptions')).json as { subscriptions: unknown[] }).subscriptions.length).toBe(0)
  } finally {
    await apiFetch(page, 'PUT', '/api/push/preferences', { preview: true })
    await context.close()
  }
})

test('the server refuses a push address that points into its network', async ({ browser }) => {
  const context = await signedInContext(browser)
  const page = await context.newPage()
  await page.goto('/')
  const key = await page.evaluate(async () => {
    const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits'])
    const raw = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey))
    return btoa(String.fromCharCode(...raw)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  })
  for (const endpoint of ['https://127.0.0.1/x', 'https://192.168.1.1/x', 'http://push.example.test/x', 'https://push.example.test:6379/x']) {
    const res = await apiFetch(page, 'POST', '/api/push/subscriptions', { endpoint, p256dh: key, auth: 'AAAAAAAAAAAAAAAAAAAAAA', label: 'x' })
    expect(res.status, endpoint).toBe(400)
  }
  const ok = await apiFetch(page, 'POST', '/api/push/subscriptions', { endpoint: ENDPOINT + '-api', p256dh: key, auth: 'AAAAAAAAAAAAAAAAAAAAAA', label: 'API device' })
  expect(ok.status).toBe(201)
  expect((await apiFetch(page, 'DELETE', `/api/push/subscriptions/${responseString(ok, 'id')}`)).status).toBe(204)
  await context.close()
})

// What the worker asked the browser to show for one push.
interface Shown { title: string, body: string, tag: string }

/**
 * Runs a push through the installed worker the way the browser does (its own
 * `push` listener gets a PushEvent) and resolves once the worker has finished
 * with it, with what it asked the browser to show. Delivering through the
 * DevTools protocol instead is fire-and-forget: under load Chromium sometimes
 * drops such a push, and a notification it was asked to show can be missing
 * from `getNotifications()` afterwards, so a test cannot tell "the worker did
 * not show it" from "the browser lost it".
 */
async function pushThroughWorker(worker: Worker, payload: string): Promise<Shown[]> {
  return worker.evaluate(async (data) => {
    type Registration = { showNotification: (title: string, options: { body: string, tag: string }) => Promise<void> }
    const registration = (self as unknown as { registration: Registration }).registration
    const asked: Shown[] = []
    const original = registration.showNotification
    registration.showNotification = async (title, options) => {
      asked.push({ title, body: options.body, tag: options.tag })
      // The notification itself is Chromium's business and is not on screen in a test.
    }
    const finishing: Array<Promise<unknown>> = []
    const PushEventOf = (self as unknown as { PushEvent: new (type: string, init: { data: string }) => Event }).PushEvent
    const event = new PushEventOf('push', { data })
    // Outside the browser's own dispatch a script-made event cannot be given work to wait for; take it instead.
    Object.defineProperty(event, 'waitUntil', { value: (work: Promise<unknown>) => { finishing.push(work) } })
    try {
      self.dispatchEvent(event)
      await Promise.all(finishing)
    } finally {
      registration.showNotification = original
    }
    return asked
  }, payload)
}

test('the service worker shows a delivered push and keeps quiet for a message while the app is on screen', async ({ browser }) => {
  const context = await signedInContext(browser)
  const page = await context.newPage()
  try {
    await page.goto('/')
    await page.evaluate(async () => { await navigator.serviceWorker.ready })
    await page.reload()
    await expect.poll(() => page.evaluate(() => navigator.serviceWorker.controller !== null)).toBe(true)
    await expect.poll(() => context.serviceWorkers().length).toBe(1)
    const worker = context.serviceWorkers()[0]!

    // A message for a channel the page is not showing: the page is on screen, so the page shows it itself.
    const channel = '11111111-1111-4111-8111-111111111111'
    const message = '22222222-2222-4222-8222-222222222222'
    const chat = JSON.stringify({ v: 1, type: 'message', title: 'Alice (#general)', body: 'Hello', tag: channel, ts: Date.now(), channel_id: channel, message_id: message })
    expect(await pushThroughWorker(worker, chat)).toEqual([])

    // The test push is always shown.
    const ping = JSON.stringify({ v: 1, type: 'test', title: 'Mnema Talk', body: 'Push works on this device.', tag: 'test', ts: Date.now() })
    expect(await pushThroughWorker(worker, ping)).toEqual([{ title: 'Mnema Talk', body: 'Push works on this device.', tag: 'test' }])

    // Anything that is not ours still shows something (the browser requires it) and never throws.
    const plain: Shown = { title: 'Mnema Talk', body: '', tag: 'mnema' }
    expect(await pushThroughWorker(worker, JSON.stringify({ unexpected: true }))).toEqual([plain])
    expect(await pushThroughWorker(worker, 'not json at all')).toEqual([plain])
  } finally {
    await context.close()
  }
})
