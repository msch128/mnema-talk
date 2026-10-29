import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// The profile banner (1.1): set under "Banner ändern" on one's own profile,
// seen on the profile of that member by other members, removed again. Every UI
// string is German (the suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Cool_Guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

/** A 1200x600 PNG drawn in the browser (no fixture file in the repository); the server stores its centre 1200x400. */
async function drawPng(page: Page): Promise<Buffer> {
  const base64 = await page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 1200
    canvas.height = 600
    const ctx = canvas.getContext('2d')!
    ctx.fillStyle = '#10b981'
    ctx.fillRect(0, 0, 1200, 600)
    return canvas.toDataURL('image/png').split(',')[1] ?? ''
  })
  return Buffer.from(base64, 'base64')
}

async function openProfileOf(page: Page, name: string) {
  await page.locator('[data-member]').filter({ hasText: name }).first().click()
  await expect(page.getByRole('dialog')).toBeVisible()
}

test('a member sets a banner, others see it on the profile, and it can be removed', async ({ browser }) => {
  const owner = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await owner.goto('/')
  expect((await apiFetch(owner, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  await owner.goto('/')

  const guest = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await guest.goto('/')
  const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
  const name = `banner${suffix}`
  const registered = await apiFetch(guest, 'POST', '/api/auth/register', { username: name, password: `pw-${suffix}-banner-x1`, invite_code: responseString(invite, 'code') })
  expect(registered.status).toBe(201)
  await guest.goto('/')

  try {
    // Without a banner: the default header, and only "Banner ändern" is offered.
    await openProfileOf(owner, ADMIN_USER)
    await expect(owner.getByTestId('profile-banner')).toHaveCount(0)
    await expect(owner.getByTestId('banner-remove')).toHaveCount(0)

    // Set one through the file input.
    await owner.getByTestId('banner-input').setInputFiles({ name: 'banner.png', mimeType: 'image/png', buffer: await drawPng(owner) })
    const banner = owner.getByTestId('profile-banner')
    await expect(banner).toBeVisible()
    await expect.poll(() => banner.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1200)
    await expect(banner).toHaveCSS('object-fit', 'cover')
    await expect(owner.getByTestId('banner-remove')).toBeVisible()
    await owner.keyboard.press('Escape')

    // Another member sees it on that profile; they get no controls for it.
    await openProfileOf(guest, ADMIN_USER)
    const seen = guest.getByTestId('profile-banner')
    await expect(seen).toBeVisible()
    await expect.poll(() => seen.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth)).toBe(1200)
    await expect(guest.getByTestId('profile-banner-controls')).toHaveCount(0)
    await guest.keyboard.press('Escape')

    // Remove it again.
    await openProfileOf(owner, ADMIN_USER)
    await expect(owner.getByTestId('profile-banner')).toBeVisible()
    await owner.getByTestId('banner-remove').click()
    await expect(owner.getByTestId('profile-banner')).toHaveCount(0)
    await owner.keyboard.press('Escape')
    await openProfileOf(guest, ADMIN_USER)
    await expect(guest.getByTestId('profile-banner')).toHaveCount(0)
  } finally {
    await apiFetch(owner, 'DELETE', '/api/users/me/banner')
  }
})
