import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `saved-ux-${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('saving a message says where it went: toast with View, a mark on the message, the list, and unsaving from the mark', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  let channelId = ''
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    channelId = responseString(channel, 'id')

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    expect((await apiFetch(owner, 'POST', `/api/channels/${channelId}/messages`, { content: 'keep this for later' })).status).toBe(201)

    // The header button says what it holds.
    await member.goto(`/c/${channelId}`)
    await expect(member.getByTestId('saved-button')).toHaveAttribute('aria-label', /Gespeichert & Erinnerungen/)

    // Saving: the toast says where it went and offers to show it; the message carries a mark.
    const line = member.getByText('keep this for later', { exact: true })
    await line.click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Nachricht speichern' }).click()
    const toast = member.getByRole('button', { name: 'Ansehen' })
    await expect(toast).toBeVisible()
    await expect(member.getByTestId('saved-mark')).toBeVisible()

    // View opens the list on that message.
    await toast.click()
    const panel = member.getByTestId('saved-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByTestId('saved-item')).toContainText('keep this for later')
    await expect(panel.getByTestId('saved-note-edit')).toBeVisible()

    // Unsaving from the mark on the message takes it out of the list.
    await member.keyboard.press('Escape')
    await expect(panel).toBeHidden()
    await member.getByTestId('saved-mark').click()
    await expect(member.getByTestId('saved-mark')).toHaveCount(0)
    await member.getByTestId('saved-button').click()
    await expect(member.getByTestId('saved-empty')).toBeVisible()
    await expect(member.getByTestId('saved-empty')).toContainText('private')
  } finally {
    if (channelId) await apiFetch(owner, 'DELETE', `/api/admin/channels/${channelId}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})
