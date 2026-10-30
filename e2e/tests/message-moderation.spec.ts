import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `msgmod-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
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
    await composer.fill('one')
    await composer.press('Enter')
    await expect(member.getByText('one', { exact: true })).toBeVisible()
    // The wait is shown before a second message is tried: on the composer, with the send button held.
    await expect(member.getByTestId('composer-limit')).toHaveText(/Langsammodus: Du kannst in 0:0\d wieder schreiben/)
    await composer.fill('zu schnell')
    await expect(member.getByRole('button', { name: 'Senden' })).toBeDisabled()
    await composer.press('Enter')
    await expect(member.getByText('zu schnell', { exact: true })).toHaveCount(0)
    await expect(member.getByTestId('composer-limit')).toHaveCount(0, { timeout: 7000 })
    await composer.fill('')
    await composer.fill('two')
    await composer.press('Enter')
    await expect(member.getByText('two', { exact: true })).toBeVisible()
    expect((await apiFetch(mod, 'PATCH', `/api/admin/channels/${channelId}`, { slow_mode_seconds: 0 })).status).toBe(200)

    // A message past the limit is counted before it is sent, not refused afterwards.
    await composer.fill('a'.repeat(4001))
    await expect(member.getByTestId('composer-limit')).toHaveText('1 Zeichen zu viel (höchstens 4000)')
    await expect(member.getByRole('button', { name: 'Senden' })).toBeDisabled()
    await composer.fill('')
    await expect(member.getByTestId('composer-limit')).toHaveCount(0)

    // Pinning shows a marker to everybody.
    await mod.getByText('two', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Nachricht anheften' }).click()
    await expect(member.getByTestId('pinned')).toHaveCount(1)
    await mod.getByText('two', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Anheftung aufheben' }).click()
    await expect(member.getByTestId('pinned')).toHaveCount(0)

    // Deleting with a reason tells the author.
    await mod.getByText('one', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Mit Grund löschen …' }).click()
    await mod.getByTestId('reason').fill('Test')
    await mod.getByTestId('message-moderation-submit').click()
    await expect(member.getByText('one', { exact: true })).toHaveCount(0)
    await expect(member.getByText(`Deine Nachricht in ${CHANNEL} wurde vom Moderationsteam entfernt: Test`)).toBeVisible()

    // Clearing a member's latest messages.
    for (const text of ['three', 'four']) {
      await composer.fill(text)
      await composer.press('Enter')
      await expect(member.getByText(text, { exact: true })).toBeVisible()
    }
    await mod.getByText('four', { exact: true }).click({ button: 'right' })
    await mod.getByRole('menuitem', { name: 'Letzte Nachrichten dieser Person löschen …' }).click()
    await mod.getByTestId('bulk-count').fill('5')
    await mod.getByTestId('reason').fill('Aufräumen')
    await mod.getByTestId('message-moderation-submit').click()
    for (const text of ['two', 'three', 'four']) await expect(member.getByText(text, { exact: true })).toHaveCount(0)

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
