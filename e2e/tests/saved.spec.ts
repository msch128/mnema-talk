import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `saved-${suffix}`
const VIEW_CHANNEL = 1 << 1

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a member saves a message, opens it from the saved list, loses access and removes it; pins say who pinned', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  const created: string[] = []
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    const channelSlug = responseString(channel, 'slug')
    created.push(channelId)

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    const posted = await apiFetch(owner, 'POST', `/api/channels/${channelId}/messages`, { content: 'remember this line' })
    expect(posted.status).toBe(201)
    const messageId = responseString(posted, 'id')

    // Save from the message menu.
    await member.goto(`/c/${channelId}`)
    await member.getByText('remember this line', { exact: true }).click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Nachricht speichern' }).click()
    await member.getByTestId('saved-button').click()
    const item = member.getByTestId('saved-item')
    await expect(item).toHaveCount(1)
    await expect(item).toContainText('remember this line')

    // A note, only for the member.
    await item.getByTestId('saved-note-edit').click()
    await member.getByTestId('saved-note-input').fill('fuer morgen')
    await member.getByTestId('saved-note-save').click()
    await expect(member.getByTestId('saved-note')).toHaveText('fuer morgen')
    expect((await apiFetch(owner, 'GET', '/api/me/saved')).json).toMatchObject({ items: [] })

    // Open it from the list.
    await member.goto('/')
    await member.getByTestId('saved-button').click()
    await member.getByTestId('saved-open').click()
    await expect(member).toHaveURL(new RegExp(`/c/${channelSlug}/m/`))

    // Pinned by: the pinned list names the Owner.
    expect((await apiFetch(owner, 'PUT', `/api/channels/${channelId}/messages/${messageId}/pin`)).status).toBe(200)
    await member.getByTestId('pinned-button').click()
    await expect(member.getByTestId('pinned-by')).toContainText('Angeheftet von')
    await member.keyboard.press('Escape')

    // Losing access turns the entry into "not available"; the note stays and it can be removed.
    const roles = (await apiFetch(owner, 'GET', '/api/roles')).json as { id: string; is_everyone: boolean }[] | { roles?: { id: string; is_everyone: boolean }[] }
    const everyoneId = (Array.isArray(roles) ? roles : roles.roles ?? []).find(r => r.is_everyone)?.id
    expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${channelId}/overwrites`, { overwrites: [{ id: everyoneId, type: 'role', allow: 0, deny: VIEW_CHANNEL }] })).status).toBe(204)
    await member.goto('/')
    await member.getByTestId('saved-button').click()
    await expect(member.getByTestId('saved-unavailable')).toBeVisible()
    await expect(member.getByTestId('saved-panel')).not.toContainText('remember this line')
    await expect(member.getByTestId('saved-note')).toHaveText('fuer morgen')
    await member.getByTestId('saved-remove').click()
    await expect(member.getByTestId('saved-empty')).toBeVisible()
  } finally {
    // Leave the sidebar as found: later specs count on its size.
    for (const id of created) await apiFetch(owner, 'DELETE', `/api/admin/channels/${id}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})
