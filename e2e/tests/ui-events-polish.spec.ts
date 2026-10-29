import { mkdir } from 'node:fs/promises'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// Events and forum on a phone: the event cards keep their size and show who
// is going, and the forum has the way back to the channel list.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const FORUM = `forum-${suffix}`
const SHOTS = process.env.UI_SHOTS_DIR

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function shot(page: Page, name: string) {
  if (!SHOTS) return
  await mkdir(SHOTS, { recursive: true })
  await page.screenshot({ path: `${SHOTS}/${name}.png` })
}

for (const viewport of [{ width: 390, height: 844, label: '390' }, { width: 1440, height: 900, label: '1440' }]) {
  test(`events list keeps its cards whole, names who is going and shows one dialog at ${viewport.label}px`, async ({ browser }) => {
    const tag = `${suffix}-${viewport.label}`
    const ctx = await browser.newContext({ viewport, hasTouch: viewport.width < 768 })
    const page = await ctx.newPage()
    try {
      await signIn(page)
      const start = new Date(Date.now() + 2 * 86_400_000).toISOString()
      for (const title of [`Game night ${tag}`, `Movie evening ${tag}`]) {
        const created = await apiFetch(page, 'POST', '/api/events', { title, description: 'Bring snacks', start_time: start, location: 'Room 42' })
        expect(created.status).toBe(201)
        expect((await apiFetch(page, 'PUT', `/api/events/${responseString(created, 'id')}/rsvp`, { response: 'going' })).status).toBe(200)
      }
      await page.goto('/')
      // Opening a channel closes the phone's drawer: let the first route settle before opening it.
      await expect(page).toHaveURL(/\/c\//)
      if (viewport.width < 768) await page.getByTestId('open-nav').click()
      await page.getByRole('button', { name: 'Veranstaltungen' }).click()
      const cards = page.locator('article').filter({ hasText: tag })
      await expect(cards).toHaveCount(2)
      for (const card of await cards.all()) {
        const box = await card.boundingBox()
        // A squashed card is cut below its button row (the Details button then sticks out).
        const details = await card.getByRole('button', { name: 'Details' }).boundingBox()
        expect(box && details && details.y + details.height <= box.y + box.height + 1).toBe(true)
        await expect(card.getByTestId('event-rsvp-summary')).toContainText('1 Zusage')
        await expect(card.getByTestId('event-rsvp-summary').locator('div.rounded-full')).not.toHaveText('?')
      }
      await shot(page, `events-list-${viewport.label}`)

      await cards.first().getByRole('button', { name: 'Details' }).click()
      const detail = page.getByRole('dialog')
      await expect(detail).toHaveCount(1)
      await expect(detail.getByRole('button', { name: 'Ich komme' })).toBeVisible()
      await expect(detail.getByRole('button', { name: 'Kann nicht' })).toBeVisible()
      await page.waitForTimeout(500) // let the dialog finish its fade-in
      await shot(page, `event-detail-${viewport.label}`)
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog').getByRole('heading', { name: 'Veranstaltungen' })).toBeVisible()
    } finally {
      await ctx.close()
    }
  })
}

test('a forum on a phone has the way back to the channel list and a search button', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true })
  const page = await ctx.newPage()
  try {
    await signIn(page)
    const forum = await apiFetch(page, 'POST', '/api/admin/channels', { name: FORUM, type: 'forum' })
    expect(forum.status).toBe(201)
    await page.goto(`/c/${responseString(forum, 'id')}`)
    await expect(page.getByTestId('forum-area')).toBeVisible()
    await shot(page, 'forum-390')
    await page.getByTestId('search-in-channel').click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await page.getByTestId('open-nav').click()
    await expect(page.getByRole('link', { name: FORUM }).or(page.getByText(FORUM).first())).toBeVisible()
  } finally {
    await ctx.close()
  }
})
