import { deflateSync } from 'node:zlib'
import type { Browser, Locator, Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, bounds, responseString } from './helpers'

// Timed messages on every screen (doc/timed-messages.md): with a lifetime
// chosen, the reply field of the thread stays wide enough to type in, the help
// line shows in full, the timer menu stays on a phone screen, a removed
// picture keeps to its column and shows as removed at once, and the image
// viewer closes when its message is gone. In English and German.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `timed-${Date.now().toString(36)}`
const PHONE = { width: 390, height: 844 }
// The desktop client's default window: the thread panel at its narrowest beside the member list.
const DESKTOP = { width: 1132, height: 820 }
const VIEWPORTS = [
  PHONE,
  { width: 820, height: 1180 },
  DESKTOP,
  { width: 1366, height: 768 },
  { width: 1920, height: 1080 }
]
const LANGUAGES = [
  { locale: 'en-US', oneHour: '1 h' },
  { locale: 'de-DE', oneHour: '1 Std' }
]
// About the narrowest a reply field may get and still take a sentence.
const MIN_FIELD = 150

let channelId = ''

test.describe.configure({ mode: 'serial' })

async function signedIn(browser: Browser, locale: string, viewport: { width: number; height: number }): Promise<Page> {
  const context = await browser.newContext({ locale, viewport })
  // Every spec signs in as the same administrator, whose stored language is
  // German: this page sees the account as English, without changing it.
  const language = locale.slice(0, 2)
  await context.route('**/api/auth/me', async route => {
    const response = await route.fetch()
    const json: unknown = await response.json().catch(() => null)
    await route.fulfill(json && typeof json === 'object' ? { response, json: { ...json, locale: language } } : { response })
  })
  await context.route('**/api/users/me/locale', route => route.fulfill({ status: 409, json: { code: 'CONFLICT', message: 'kept' } }))
  const page = await context.newPage()
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  return page
}

// A solid PNG of the given size, drawn here so the test needs no files.
function png(width: number, height: number): number[] {
  const table = Array.from({ length: 256 }, (_, n) => {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    return c >>> 0
  })
  const crc = (buf: Buffer) => {
    let c = 0xffffffff
    for (const b of buf) c = (table[(c ^ b) & 0xff] ?? 0) ^ (c >>> 8)
    return (c ^ 0xffffffff) >>> 0
  }
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4)
    length.writeUInt32BE(data.length)
    const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
    const sum = Buffer.alloc(4)
    sum.writeUInt32BE(crc(body))
    return Buffer.concat([length, body, sum])
  }
  const rows = Buffer.alloc((width * 3 + 1) * height, 90)
  for (let y = 0; y < height; y++) rows[y * (width * 3 + 1)] = 0
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header[8] = 8
  header[9] = 2
  return [...Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))])]
}

async function uploadPicture(page: Page, name: string, width: number, height: number): Promise<{ message: string; media: string }> {
  const result = await page.evaluate(async ({ id, bytes, name }) => {
    const form = new FormData()
    form.append('file', new Blob([new Uint8Array(bytes)], { type: 'image/png' }), name)
    const res = await fetch(`/api/channels/${id}/upload`, { method: 'POST', body: form, credentials: 'same-origin' })
    return { status: res.status, body: await res.text() }
  }, { id: channelId, bytes: png(width, height), name })
  expect(result.status, result.body.slice(0, 300)).toBe(201)
  const msg = JSON.parse(result.body) as { id: string; attachments: { id: string }[] }
  return { message: msg.id, media: msg.attachments[0]?.id ?? '' }
}

async function chooseLifetime(page: Page, scope: Locator, oneHour: string) {
  await scope.getByTestId('lifetime-button').click()
  await page.getByRole('menuitem', { name: oneHour, exact: true }).click()
  await expect(scope.getByTestId('lifetime-help')).toBeVisible()
}

async function expectWholeLine(line: Locator, where: string) {
  const cut = await line.evaluate(el => el.scrollWidth > el.clientWidth + 1 || getComputedStyle(el).textOverflow === 'ellipsis')
  expect(cut, `the help line is cut ${where}`).toBe(false)
}

async function expectWithinScreen(page: Page, target: Locator, where: string) {
  const box = await bounds(target)
  const width = page.viewportSize()?.width ?? 0
  expect(box.x, `${where} starts off screen`).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width, `${where} runs off screen`).toBeLessThanOrEqual(width + 0.5)
}

test.beforeAll(async ({ browser }) => {
  const page = await signedIn(browser, 'de-DE', DESKTOP)
  const res = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(res.status).toBe(201)
  channelId = responseString(res, 'id')
  expect((await apiFetch(page, 'POST', `/api/channels/${channelId}/messages`, { content: 'Thread root for the timer' })).status).toBe(201)
  await page.context().close()
})

test.afterAll(async ({ browser }) => {
  const page = await signedIn(browser, 'de-DE', DESKTOP)
  await apiFetch(page, 'DELETE', `/api/admin/channels/${channelId}`)
  await page.context().close()
})

for (const { locale, oneHour } of LANGUAGES) {
  for (const viewport of VIEWPORTS) {
    test(`with a lifetime chosen, the composers keep their field and their whole help line (${locale}, ${viewport.width}×${viewport.height})`, async ({ browser }) => {
      const page = await signedIn(browser, locale, viewport)
      const where = `at ${viewport.width}×${viewport.height}`
      await page.goto(`/c/${channelId}`)
      const root = page.locator('[data-msg-id]', { hasText: 'Thread root for the timer' }).first()
      await expect(root).toBeVisible()

      // The channel composer.
      const composer = page.getByTestId('chat-composer')
      await chooseLifetime(page, composer, oneHour)
      await expectWholeLine(composer.getByTestId('lifetime-help'), `in the channel ${where}`)
      expect((await bounds(composer.locator('textarea'))).width, `channel field ${where}`).toBeGreaterThanOrEqual(MIN_FIELD)

      // The thread, with the member list open where there is room for it.
      const members = page.getByRole('button', { name: /Mitgliederliste|member list/ })
      if (viewport.width >= 1100 && (await members.getAttribute('aria-pressed')) === 'false') await members.click()
      await root.hover()
      await root.getByTestId('msg-open-thread').click()
      const thread = page.locator('aside').filter({ has: page.getByTestId('thread-close') })
      await chooseLifetime(page, thread, oneHour)
      await expect(thread.getByTestId('lifetime-button')).toHaveAttribute('aria-label', new RegExp(oneHour))
      await expectWholeLine(thread.getByTestId('lifetime-help'), `in the thread ${where}`)
      const field = thread.locator('textarea')
      await field.fill('A longer sentence that has to wrap at word boundaries, not one letter at a time.')
      expect((await bounds(field)).width, `thread field ${where}`).toBeGreaterThanOrEqual(MIN_FIELD)

      // The timer menu stays on screen, from the thread too.
      await thread.getByTestId('lifetime-button').click()
      await expectWithinScreen(page, page.getByRole('menu'), `the timer menu ${where}`)
      await page.keyboard.press('Escape')
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
      expect(overflow, `the page scrolls sideways ${where}`).toBeLessThanOrEqual(0)
      await page.context().close()
    })
  }
}

test('a removed picture keeps to its column on a phone and shows as removed without a reload', async ({ browser }) => {
  const page = await signedIn(browser, 'de-DE', PHONE)
  const { media } = await uploadPicture(page, 'wide.png', 800, 600)
  await page.goto(`/c/${channelId}`)
  const picture = page.locator('[data-attachment="image"]').last()
  await expect(picture.locator('img')).toBeVisible()
  const before = await bounds(picture)

  expect((await apiFetch(page, 'DELETE', `/api/admin/media/${media}`)).status).toBe(204)
  const tile = page.locator('[data-media-placeholder="gone"]').last()
  await expect(tile).toBeVisible()
  await expectWithinScreen(page, tile, 'the removed-picture tile')
  const after = await bounds(tile)
  expect(Math.abs(after.width - before.width), 'the tile keeps the picture’s width').toBeLessThanOrEqual(2)
  expect(Math.abs(after.height - before.height), 'the tile keeps the picture’s height').toBeLessThanOrEqual(2)
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow, 'the page scrolls sideways').toBeLessThanOrEqual(0)
  await page.context().close()
})

test('the image viewer closes when its message is gone', async ({ browser }) => {
  const page = await signedIn(browser, 'de-DE', DESKTOP)
  const { message } = await uploadPicture(page, 'viewer.png', 400, 300)
  await page.goto(`/c/${channelId}`)
  const picture = page.locator(`[data-msg-id="${message}"] [data-attachment="image"] img`)
  await expect(picture).toBeVisible()
  await picture.click()
  const viewer = page.getByRole('dialog')
  await expect(viewer).toBeVisible()
  expect((await apiFetch(page, 'DELETE', `/api/channels/${channelId}/messages/${message}`)).status).toBe(204)
  await expect(viewer).toBeHidden()
  await page.context().close()
})
