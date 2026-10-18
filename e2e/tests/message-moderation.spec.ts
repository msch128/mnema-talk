import { test, expect } from '@playwright/test'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `msgmod-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('slow mode, pinning, deleting with a reason and clearing a member are visible to both sides', async ({ browser }) => {
  const modCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const mod = await modCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(mod, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(mod, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    expect((await apiFetch(mod, 'PATCH', `/api/admin/channels/${channelId}`, { slow_mode_seconds: 5 })).status).toBe(200)

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
    await expect(mod.getByTestId('slow-mode-chip')).toContainText('5 s')

    // Slow mode holds the member back, not the Owner.
    const composer = member.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.fill('eins')
    await composer.press('Enter')
    await expect(member.getByText('eins', { exact: true })).toBeVisible()
    await composer.fill('zu schnell')
    await composer.press('Enter')
    await expect(member.getByText(/Langsammodus: bitte warte noch/)).toBeVisible()
    await composer.fill('')
    await member.waitForTimeout(5200)
    await composer.fill('zwei')
    await composer.press('Enter')
    await expect(member.getByText('zwei', { exact: true })).toBeVisible()
    expect((await apiFetch(mod, 'PATCH', `/api/admin/channels/${channelId}`, { slow_mode_seconds: 0 })).status).toBe(200)

    // Pinning shows a marker to everybody.
    await mod.getByText('zwei', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Nachricht anheften' }).click()
    await expect(member.getByTestId('pinned')).toHaveCount(1)
    await mod.getByText('zwei', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Nachricht lösen' }).click()
    await expect(member.getByTestId('pinned')).toHaveCount(0)

    // Deleting with a reason tells the author.
    await mod.getByText('eins', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Mit Grund löschen…' }).click()
    await mod.getByTestId('reason').fill('Test')
    await mod.getByTestId('message-moderation-submit').click()
    await expect(member.getByText('eins', { exact: true })).toHaveCount(0)
    await expect(member.getByText(`Ein Moderator hat deine Nachricht in ${CHANNEL} entfernt: Test`)).toBeVisible()

    // Clearing a member's latest messages.
    for (const text of ['drei', 'vier']) {
      await composer.fill(text)
      await composer.press('Enter')
      await expect(member.getByText(text, { exact: true })).toBeVisible()
    }
    await mod.getByText('vier', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Letzte Nachrichten dieser Person löschen…' }).click()
    await mod.getByTestId('bulk-count').fill('5')
    await mod.getByTestId('reason').fill('Aufräumen')
    await mod.getByTestId('message-moderation-submit').click()
    for (const text of ['zwei', 'drei', 'vier']) await expect(member.getByText(text, { exact: true })).toHaveCount(0)

    // The audit log kept the texts.
    await mod.goto('/admin/audit')
    await mod.getByLabel('Nach Ereignis filtern').selectOption('message')
    await mod.getByRole('button', { name: 'Anwenden' }).click()
    const entry = mod.locator('[data-testid^="audit-entry-"]', { hasText: 'Nachricht gelöscht' }).first()
    await expect(entry).toBeVisible()
    await entry.getByRole('button').click()
    await expect(mod.locator('pre').first()).toContainText('Aufräumen')
  } finally {
    await modCtx.close()
    await memberCtx.close()
  }
})
