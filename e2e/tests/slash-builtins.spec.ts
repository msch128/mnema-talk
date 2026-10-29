import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const STAMP = Date.now().toString(36)
const CHANNEL = `slash-${STAMP}`
// A member of this spec alone: the kick signs them out everywhere, which must not touch the shared suite member.
const KICKED = { username: `kickee-${STAMP}`, password: 'kickee-password-123' }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function runCommand(page: Page, channel: string, text: string) {
  const composer = page.getByRole('textbox', { name: `Nachricht an #${channel}` })
  await composer.fill(text)
  await expect(page.getByTestId('slash-suggestions')).toContainText(text)
  await composer.press('Enter')
  await expect(page.getByTestId('slash-bar')).toBeVisible()
}

test('built-in commands post for the caller, and /kick signs a member out with a calm note and no sanction', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')

    await member.goto('/')
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    const registered = await apiFetch(member, 'POST', '/api/auth/register', { ...KICKED, invite_code: responseString(invite, 'code') })
    expect(registered.status).toBe(201)
    await member.goto(`/c/${channelId}`)

    // A text command posts a message of its own.
    await owner.goto(`/c/${channelId}`)
    await runCommand(owner, CHANNEL, '/shrug')
    await owner.getByTestId('slash-bar').locator('[data-option="text"]').fill('no idea')
    await owner.getByTestId('slash-run').click()
    await expect(owner.getByRole('article').filter({ hasText: 'no idea' })).toContainText('¯\\_(ツ)_/¯')
    await expect(member.getByRole('article').filter({ hasText: 'no idea' })).toBeVisible()

    // The Owner kicks the member.
    await runCommand(owner, CHANNEL, '/kick')
    const bar = owner.getByTestId('slash-bar')
    await bar.locator('[data-option="member"]').selectOption({ label: KICKED.username })
    await bar.locator('[data-option="reason"]').fill('Pause')
    await owner.getByTestId('slash-run').click()
    await expect(owner.getByText('Erledigt.')).toBeVisible()

    // The member reads a calm note and is signed out ...
    await expect(member.getByText('Du wurdest von einer Moderation abgemeldet: Pause')).toBeVisible()
    await expect(member.getByRole('dialog').getByTestId('login-heading')).toBeVisible()
    // ... and may sign in again at once.
    await signIn(member, KICKED.username, KICKED.password)
    await member.goto(`/c/${channelId}`)
    await expect(member.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })).toBeEnabled()
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})

test('a member without the permission is not offered the moderation commands', async ({ page }) => {
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: `slash-menu-${STAMP}`, type: 'text' })
  const channelId = responseString(channel, 'id')
  const invite = await apiFetch(page, 'POST', '/api/admin/invites', { max_uses: 1 })
  const username = `plain-${STAMP}`
  const ctx = await page.context().browser()!.newContext()
  const plain = await ctx.newPage()
  try {
    await plain.goto('/')
    expect((await apiFetch(plain, 'POST', '/api/auth/register', { username, password: 'plain-password-123', invite_code: responseString(invite, 'code') })).status).toBe(201)
    await plain.goto(`/c/${channelId}`)
    const composer = plain.getByRole('textbox', { name: `Nachricht an #slash-menu-${STAMP}` })
    await composer.fill('/')
    const menu = plain.getByTestId('slash-suggestions')
    await expect(menu).toContainText('/nick')
    await expect(menu).not.toContainText('/kick')
    await expect(menu).not.toContainText('/ban')
    await expect(menu).not.toContainText('/timeout')
  } finally {
    await ctx.close()
  }
})
