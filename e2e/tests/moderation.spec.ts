import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `mod-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a moderator times out and bans a member; they see it, recover, and the audit log shows it', async ({ browser }) => {
  const modCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const mod = await modCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(mod, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(mod, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(mod, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await member.reload()
    for (const page of [mod, member]) await page.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click()

    const composer = member.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.fill('before the timeout')
    await composer.press('Enter')
    await expect(member.getByText('before the timeout')).toBeVisible()

    // The moderator times the member out for a few seconds from the member list.
    await mod.locator(`[data-member="${suiteMember.username}"]`).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Auszeit verhängen …' }).click()
    const dialog = mod.getByRole('dialog')
    await dialog.getByTestId('duration-custom').click()
    await dialog.getByTestId('custom-amount').fill('8')
    await dialog.getByTestId('custom-unit').selectOption('seconds')
    await dialog.getByTestId('reason').fill('zu laut')
    await dialog.getByTestId('moderation-submit').click()
    await expect(dialog).toBeHidden()

    // The member sees the countdown and the reason, and cannot post.
    const banner = member.getByTestId('timeout-banner')
    await expect(banner).toBeVisible()
    await expect(banner).toContainText('zu laut')
    await expect(member.getByTestId('timeout-remaining')).toHaveText(/^0:0[0-8]$/)
    const refused = await apiFetch(member, 'POST', `/api/channels/${responseString(channel, 'id')}/messages`, { content: 'not allowed' })
    expect(refused.status).toBe(403)

    // When the time is up the banner goes and posting works again.
    await expect(banner).toBeHidden({ timeout: 20_000 })
    await composer.fill('after the timeout')
    await composer.press('Enter')
    await expect(member.getByText('after the timeout')).toBeVisible()

    // A ban ends the member's session; unbanning in the admin console lets them back in.
    await mod.locator(`[data-member="${suiteMember.username}"]`).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Sperren …' }).click()
    await mod.getByRole('dialog').getByTestId('reason').fill('Regelverstoß')
    await mod.getByRole('dialog').getByTestId('moderation-submit').click()
    await expect(mod.getByRole('dialog')).toBeHidden()
    await expect.poll(async () => (await apiFetch(member, 'GET', '/api/auth/me')).status).toBe(401)
    const login = await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })
    expect(login.status).toBe(403)
    expect(JSON.stringify(login.json)).toContain('Regelverstoß')

    await mod.goto('/admin/bans')
    const row = mod.locator('[data-testid^="ban-"]', { hasText: suiteMember.username })
    await expect(row).toContainText('Regelverstoß')
    await row.getByTestId('unban').click()
    await mod.getByRole('alertdialog').getByRole('button', { name: 'Entsperren' }).click()
    await expect(row).toHaveCount(0)
    expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)

    // The audit log has all of it.
    await mod.goto('/admin/audit')
    for (const label of ['Mitglied in Auszeit geschickt', 'Mitglied gesperrt', 'Mitglied entsperrt']) {
      await expect(mod.locator('[data-testid^="audit-entry-"]', { hasText: label }).first()).toBeVisible()
    }
  } finally {
    await modCtx.close()
    await memberCtx.close()
  }
})
