import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// 1.2 small additions: standard emoji shortcodes in the composer and the
// events dialog's Past tab.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `small-${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('typing :smi suggests standard emoji and Enter inserts the character', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await ctx.newPage()
  try {
    await signIn(page)
    const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    await page.goto(`/c/${responseString(channel, 'id')}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.fill('hello :smi')
    const option = page.locator('[data-mention-option="smile"]')
    await expect(option).toBeVisible()
    await composer.press('ArrowDown')
    await composer.press('ArrowUp')
    await option.hover()
    await composer.press('Enter')
    await expect(composer).toHaveValue('hello 😄 ')
    await composer.fill(':smi')
    await expect(option).toBeVisible()
    await composer.press('Escape')
    await expect(option).toBeHidden()
  } finally {
    await ctx.close()
  }
})

test('the Past tab lists a cancelled event and the Upcoming tab does not', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await ctx.newPage()
  try {
    await signIn(page)
    const title = `Cancelled meetup ${suffix}`
    const created = await apiFetch(page, 'POST', '/api/events', { title, description: 'x', start_time: new Date(Date.now() + 3 * 86_400_000).toISOString(), location: 'Room 1' })
    expect(created.status).toBe(201)
    expect((await apiFetch(page, 'POST', `/api/events/${responseString(created, 'id')}/cancel`)).status).toBe(200)

    await page.goto('/')
    await page.getByRole('button', { name: 'Veranstaltungen' }).click()
    await page.getByRole('button', { name: 'Vergangen' }).click()
    await expect(page.locator('article').filter({ hasText: title })).toHaveCount(1)
    await page.getByRole('button', { name: /^Bevorstehend/ }).click()
    await expect(page.locator('article').filter({ hasText: title })).toHaveCount(0)
  } finally {
    await ctx.close()
  }
})
