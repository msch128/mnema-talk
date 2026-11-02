import { test, expect } from './fixtures'
import type { Browser, Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

// Event roles end to end: an administrator creates an event with a Tank role of
// one place, a member takes it, a second member lands on the waitlist and moves
// up when the first one leaves.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TITLE = `Raid ${suffix}`
const THIRD = { username: `third_${suffix}`, password: `pw-${suffix}-third-1234` }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function register(admin: Page, page: Page, username: string, password: string) {
  await page.goto('/')
  const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  expect((await apiFetch(page, 'POST', '/api/auth/register', { username, password, invite_code: responseString(invite, 'code') })).status).toBe(201)
  await page.reload()
}

async function openDetails(page: Page) {
  await page.getByRole('button', { name: 'Veranstaltungen' }).click()
  const card = page.locator('article').filter({ hasText: TITLE })
  await card.getByRole('button', { name: 'Details' }).click()
  await expect(page.getByTestId('event-roster')).toBeVisible()
}

const tank = (page: Page) => page.locator('[data-testid="event-role"][data-role="Tank"]')

async function member(browser: Browser, admin: Page, username: string, password: string, registered: boolean) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  if (registered) await signIn(page, username, password)
  else await register(admin, page, username, password)
  return { ctx, page }
}

test('a full role puts the next member on the waitlist, and they move up when a place frees', async ({ browser }) => {
  const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const admin = await adminCtx.newPage()
  const contexts = [adminCtx]
  try {
    await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)

    // The administrator creates the event with one role of one place.
    await admin.getByRole('button', { name: 'Veranstaltungen' }).click()
    await admin.getByTestId('events-empty-create').or(admin.getByRole('button', { name: 'Veranstaltung erstellen' }).first()).click()
    const dialog = admin.getByRole('dialog').last()
    await dialog.getByLabel('Titel').fill(TITLE)
    const start = new Date(Date.now() + 2 * 86_400_000)
    const pad = (n: number) => String(n).padStart(2, '0')
    await dialog.locator('input[type="datetime-local"]').first().fill(`${start.getFullYear()}-${pad(start.getMonth() + 1)}-${pad(start.getDate())}T18:00`)
    await dialog.getByLabel('Freitext-Ort').fill('Halle 1')
    await dialog.getByTestId('event-role-add').click()
    await dialog.getByTestId('event-role-name').fill('Tank')
    await dialog.getByRole('checkbox', { name: 'Unbegrenzt' }).uncheck()
    await dialog.getByTestId('event-role-capacity').fill('1')
    await dialog.getByRole('button', { name: 'Erstellen' }).click()
    await expect(dialog).toBeHidden()

    // Two members: the first takes the place, the second waits.
    const first = await member(browser, admin, suiteMember.username, suiteMember.password, suiteMember.registered)
    suiteMember.registered = true
    const second = await member(browser, admin, THIRD.username, THIRD.password, false)
    contexts.push(first.ctx, second.ctx)

    await openDetails(first.page)
    await tank(first.page).getByTestId('event-role-action').click()
    await expect(tank(first.page).getByTestId('event-role-fill')).toHaveText('1/1')
    await expect(tank(first.page).getByTestId('event-role-action')).toHaveText('Abmelden')

    await openDetails(second.page)
    await expect(tank(second.page).getByTestId('event-role-action')).toHaveText('Auf die Warteliste')
    await tank(second.page).getByTestId('event-role-action').click()
    await expect(second.page.getByTestId('event-my-waitlist')).toContainText('Platz 1')

    // The first member leaves; the second moves up and is told.
    await tank(first.page).getByTestId('event-role-action').click()
    await expect(tank(second.page).getByTestId('event-role-action')).toHaveText('Abmelden')
    await expect(second.page.getByTestId('event-my-waitlist')).toHaveCount(0)
    await expect(tank(second.page).getByTestId('event-role-fill')).toHaveText('1/1')
  } finally {
    for (const ctx of contexts) await ctx.close()
  }
})
