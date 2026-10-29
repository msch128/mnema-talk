import { deflateSync } from 'node:zlib'
import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, bounds, responseString } from './helpers'

// Layout guards for the desktop: the channel title, the admin console shell and
// the message list when an image arrives late. The phone counterparts live in
// mobile.spec.ts.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Cool_Guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
// 20 characters: the longest name that must stay whole beside the open member list at 1000px.
const LONG_NAME = `no-topic-channel-${suffix.slice(-3)}`
const IMAGE_CHANNEL = `bilder-${suffix}`
const TABS = ['users', 'roles', 'channels', 'invites', 'onboarding', 'emoji', 'bans', 'bots', 'media', 'audit', 'system']

// Channels these tests make are removed again: other specs count unread channels.
const made: string[] = []
async function makeChannel(page: Page, name: string): Promise<string> {
  const res = await apiFetch(page, 'POST', '/api/admin/channels', { name, type: 'text' })
  expect(res.status).toBe(201)
  const id = responseString(res, 'id')
  made.push(id)
  return id
}
test.afterEach(async ({ page }) => {
  for (const id of made.splice(0)) await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
})

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// A gradient of the given size; real pixels so the browser lays the image out only once it has loaded.
function gradientPng(width: number, height: number): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    return c >>> 0
  })
  const crc = (data: Buffer) => {
    let c = 0xffffffff
    for (const byte of data) c = (crcTable[(c ^ byte) & 0xff] ?? 0) ^ (c >>> 8)
    return (c ^ 0xffffffff) >>> 0
  }
  const chunk = (type: string, data: Buffer) => {
    const length = Buffer.alloc(4)
    length.writeUInt32BE(data.length)
    const body = Buffer.concat([Buffer.from(type), data])
    const sum = Buffer.alloc(4)
    sum.writeUInt32BE(crc(body))
    return Buffer.concat([length, body, sum])
  }
  const rows = Buffer.alloc((width * 3 + 1) * height)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const at = y * (width * 3 + 1) + 1 + x * 3
      rows[at] = (x * 255) / width
      rows[at + 1] = 120
      rows[at + 2] = (y * 255) / height
    }
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header[8] = 8
  header[9] = 2
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', header), chunk('IDAT', deflateSync(rows)), chunk('IEND', Buffer.alloc(0))])
}

test('a channel without a topic shows its whole name in the header at 1000px', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 800 })
  await signIn(page)
  await makeChannel(page, LONG_NAME)
  await page.reload()
  await page.locator('[data-channel-type="text"]', { hasText: LONG_NAME }).click()
  const title = page.getByTestId('chat-channel-name')
  await expect(title).toHaveText(LONG_NAME)
  const truncated = await title.evaluate((el) => el.scrollWidth > el.clientWidth)
  expect(truncated, 'the title is cut off').toBe(false)
})

for (const width of [1440, 1000]) {
  test(`admin console at ${width}px: every tab in reach, stable height, title follows the tab`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await signIn(page)
    await page.goto('/admin/users')
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    const first = await bounds(dialog)
    const barTop = (await bounds(page.getByTestId('admin-tabs'))).y
    for (const tab of TABS) {
      const button = page.getByTestId(`tab-${tab}`)
      await expect(button).toBeInViewport({ ratio: 1 })
      await button.click()
      await expect(button).toHaveAttribute('aria-pressed', 'true')
      await expect(dialog.getByRole('heading', { level: 2 })).toHaveText(/Admin/)
      const box = await bounds(dialog)
      expect(box.height, `${tab} dialog height`).toBe(first.height)
      expect((await bounds(page.getByTestId('admin-tabs'))).y, `${tab} tab bar position`).toBe(barTop)
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow).toBeLessThanOrEqual(0)
  })
}

test('a channel with an image opens at the newest message even when the image loads late', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 800 })
  await signIn(page)
  const id = await makeChannel(page, IMAGE_CHANNEL)
  for (let i = 1; i <= 4; i++) expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: `Text ${i}` })).status).toBe(201)
  const png = [...gradientPng(900, 600)]
  const uploaded = await page.evaluate(async ({ id, png }) => {
    const form = new FormData()
    form.append('file', new Blob([new Uint8Array(png)], { type: 'image/png' }), 'bild.png')
    const res = await fetch(`/api/channels/${id}/upload`, { method: 'POST', body: form, credentials: 'same-origin' })
    return res.status
  }, { id, png })
  expect(uploaded).toBe(201)
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'Das Letzte' })).status).toBe(201)

  // The picture arrives a second after the list was first laid out.
  await page.route('**/api/media/**', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1000))
    await route.continue()
  })
  await page.reload()
  await page.locator('[data-channel-type="text"]', { hasText: IMAGE_CHANNEL }).click()
  const image = page.locator('[data-attachment="image"] img')
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((img) => (img instanceof HTMLImageElement ? img.complete && img.naturalHeight : 0))).toBeGreaterThan(0)
  const last = page.locator('[data-msg-id]', { hasText: 'Das Letzte' })
  await expect(last).toBeInViewport({ ratio: 1 })
  const fromBottom = await page.locator('[data-msg-id]').first().evaluate((row) => {
    const list = row.parentElement
    return list ? list.scrollHeight - list.scrollTop - list.clientHeight : -1
  })
  expect(fromBottom).toBeLessThanOrEqual(1)
})
