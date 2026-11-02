import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a lockdown shuts the door with a calm answer, a waiting time tells a new member when they may write', async ({ browser }) => {
  const adminCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const newbieCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const admin = await adminCtx.newPage()
  const newbie = await newbieCtx.newPage()
  try {
    await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
    await admin.goto('/admin/raid')

    // Lockdown on: the banner is there and a sign-up is refused with the paused sentence.
    await admin.getByTestId('lockdown-start').click()
    await expect(admin.getByTestId('lockdown-state')).toContainText('Lockdown ist aktiv')
    await expect(admin.getByTestId('lockdown-banner')).toBeVisible()
    const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect(invite.status).toBe(201)
    const code = responseString(invite, 'code')
    const refused = await apiFetch(newbie, 'POST', '/api/auth/register', { username: `newbie_${suffix}`, password: 'newbie-password-123', invite_code: code })
    expect(refused.status).toBe(503)
    await admin.getByTestId('lockdown-end').click()
    await expect(admin.getByTestId('lockdown-state')).toHaveText('Anmeldungen sind offen.')
    await expect(admin.getByTestId('lockdown-banner')).toHaveCount(0)

    // Waiting time: the new member sees when writing opens up, in the composer.
    expect((await apiFetch(admin, 'PUT', '/api/admin/raid', { join_limit: 0, join_window_minutes: 10, min_age_write_minutes: 60, min_age_react_minutes: 0, min_age_speak_minutes: 0 })).status).toBe(200)
    await newbie.goto('/')
    const registered = await apiFetch(newbie, 'POST', '/api/auth/register', { username: `newbie_${suffix}`, password: 'newbie-password-123', invite_code: code })
    expect(registered.status).toBe(201)
    await newbie.reload()
    await expect(newbie.getByPlaceholder(/Schreiben kannst du in/)).toBeVisible()
  } finally {
    await apiFetch(admin, 'PUT', '/api/admin/raid/lockdown', { active: false })
    await apiFetch(admin, 'PUT', '/api/admin/raid', { join_limit: 0, join_window_minutes: 10, min_age_write_minutes: 0, min_age_react_minutes: 0, min_age_speak_minutes: 0 })
    await adminCtx.close()
    await newbieCtx.close()
  }
})
