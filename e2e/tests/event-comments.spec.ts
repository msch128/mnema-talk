import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, suiteMember } from './helpers'

// Event comments end to end: a member comments under an event and the
// administrator, who has the event open, sees it live and deletes it.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TITLE = `Spieleabend ${suffix}`
const TEXT = `Ich bringe Snacks ${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function openDetails(page: Page) {
  await page.getByRole('button', { name: 'Veranstaltungen' }).click()
  const card = page.locator('article').filter({ hasText: TITLE })
  await card.getByRole('button', { name: 'Details' }).click()
  await expect(page.getByTestId('event-comments')).toBeVisible()
}

test('a member comments under an event and the administrator sees it live and deletes it', async ({ browser }) => {
  const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  try {
    const admin = await adminCtx.newPage()
    await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
    const start = new Date(Date.now() + 2 * 86_400_000).toISOString()
    expect((await apiFetch(admin, 'POST', '/api/events', { title: TITLE, description: '', start_time: start, location: 'Halle 1' })).status).toBe(201)
    await admin.reload()

    const member = await memberCtx.newPage()
    if (!suiteMember.registered) {
      await member.goto('/')
      const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
      const code = (invite.json as { code: string }).code
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: code })).status).toBe(201)
      suiteMember.registered = true
      await member.reload()
    } else {
      await signIn(member, suiteMember.username, suiteMember.password)
    }

    await openDetails(admin)
    await expect(admin.getByTestId('event-comments')).toContainText('Noch keine Kommentare')

    await openDetails(member)
    await member.getByTestId('comment-input').fill(TEXT)
    await member.getByTestId('comment-send').click()
    await expect(member.getByTestId('event-comment').filter({ hasText: TEXT })).toBeVisible()

    // The administrator has the event open and sees it without reloading.
    const seen = admin.getByTestId('event-comment').filter({ hasText: TEXT })
    await expect(seen).toBeVisible()
    await seen.getByTestId('comment-delete').click()
    await admin.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click()
    await expect(seen).toHaveCount(0)
    await expect(member.getByTestId('event-comment').filter({ hasText: TEXT })).toHaveCount(0)
  } finally {
    await adminCtx.close()
    await memberCtx.close()
  }
})
