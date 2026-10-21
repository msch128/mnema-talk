import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `mentions-${suffix}`
const PRIVATE = `hidden-${suffix}`
const ROLE = `Team${suffix}`
const VIEW_CHANNEL = 1 << 1

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a role mention reaches its members, shows as a pill and lands in the inbox; hidden channels stay out', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    const hidden = await apiFetch(owner, 'POST', '/api/admin/channels', { name: PRIVATE, type: 'text' })
    expect(hidden.status).toBe(201)
    const hiddenId = responseString(hidden, 'id')

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    const memberId = responseUserId(await apiFetch(member, 'GET', '/api/auth/me'))

    // A mentionable role held by the member; one channel is hidden from everyone.
    const role = await apiFetch(owner, 'POST', '/api/admin/roles', { name: ROLE, permissions: 0, mentionable: true })
    expect(role.status).toBe(201)
    expect((await apiFetch(owner, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: [responseString(role, 'id')] })).status).toBe(204)
    const rolesJson = (await apiFetch(owner, 'GET', '/api/roles')).json as { roles?: { id: string; is_everyone: boolean }[] } | { id: string; is_everyone: boolean }[]
    const everyoneId = (Array.isArray(rolesJson) ? rolesJson : rolesJson.roles ?? []).find(r => r.is_everyone)?.id
    expect(everyoneId).toBeTruthy()
    expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${hiddenId}/overwrites`, { overwrites: [{ id: everyoneId, type: 'role', allow: 0, deny: VIEW_CHANNEL }] })).status).toBe(204)

    // The composer suggests the role, and picking it writes "@Role ".
    await owner.goto(`/c/${channelId}`)
    const composer = owner.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.fill(`@${ROLE.slice(0, 5)}`)
    await owner.locator(`[data-mention-option="${ROLE}"]`).click()
    await expect(composer).toHaveValue(`@${ROLE} `)
    await composer.pressSequentially('bitte lesen')
    await composer.press('Enter')
    await expect(owner.locator('.md-mention-role', { hasText: `@${ROLE}` })).toBeVisible()
    // In the hidden channel the same mention reaches nobody who cannot see it.
    expect((await apiFetch(owner, 'POST', `/api/channels/${hiddenId}/messages`, { content: `@${ROLE} geheim` })).status).toBe(201)

    // The member sees the badge, the pill, and the inbox entry that jumps to the message.
    await member.goto('/')
    const badge = member.getByTestId('mentions-inbox-badge')
    await expect(badge).toHaveText('1')
    await member.getByTestId('mentions-inbox-button').click()
    const items = member.getByTestId('mentions-inbox-item')
    await expect(items).toHaveCount(1)
    await expect(items.first()).toContainText('bitte lesen')
    await expect(member.getByTestId('mentions-inbox')).not.toContainText('geheim')
    await items.first().click()
    await expect(member).toHaveURL(new RegExp(`/c/${channelId}/m/`))
    await expect(member.locator('.md-mention-role', { hasText: `@${ROLE}` })).toBeVisible()
    expect((await apiFetch(owner, 'DELETE', `/api/admin/roles/${responseString(role, 'id')}`)).status).toBe(204)
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})
