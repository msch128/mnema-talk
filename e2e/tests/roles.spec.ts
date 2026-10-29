import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const SECRET = `geheim-${suffix}`
const TALK = `Runde ${suffix}`
const ROLE = `Insider ${suffix}`

// Permission bits (see doc/roles-and-permissions.md).
const VIEW_CHANNEL = 1 << 1
const CONNECT = 1 << 7
const MANAGE_ROLES = 1 << 12
const MANAGE_MEMBERS = 1 << 15

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the Owner creates and assigns a role; the member gains and loses rights live', async ({ browser }) => {
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const ownerId = responseUserId(await apiFetch(a, 'GET', '/api/auth/me'))

    // A private text channel and a Talk only holders of the role may join.
    const text = await apiFetch(a, 'POST', '/api/admin/channels', { name: SECRET, type: 'text' })
    expect(text.status).toBe(201)
    const talk = await apiFetch(a, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })
    expect(talk.status).toBe(201)
    const roles = await apiFetch(a, 'GET', '/api/roles')
    const list = (roles.json as { roles?: { id: string; is_everyone: boolean }[] }).roles ?? (roles.json as { id: string; is_everyone: boolean }[])
    const everyoneId = list.find(r => r.is_everyone)?.id
    expect(everyoneId).toBeTruthy()

    // The member (shared with the other specs) signs in or registers.
    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await b.reload()
    const memberId = responseUserId(await apiFetch(b, 'GET', '/api/auth/me'))

    // The Owner creates the role in the web app.
    await a.goto('/admin/roles')
    await a.getByRole('button', { name: 'Neue Rolle' }).click()
    const editor = a.getByRole('group', { name: 'Ausgewählte Rolle' })
    await editor.getByLabel('Name').fill(ROLE)
    await editor.locator('[data-permission="manage_roles"] [role="switch"]').click()
    await editor.locator('[data-permission="manage_members"] [role="switch"]').click()
    await a.getByRole('button', { name: 'Rolle speichern' }).click()
    await expect(a.getByRole('list', { name: 'Rollen' }).getByText(ROLE)).toBeVisible()
    const created = await apiFetch(a, 'GET', '/api/roles')
    const all = (created.json as { roles?: { id: string; name: string; permissions: number }[] }).roles ?? (created.json as { id: string; name: string; permissions: number }[])
    const role = all.find(r => r.name === ROLE)
    expect(role?.permissions).toBe(MANAGE_ROLES | MANAGE_MEMBERS)

    // Hide the channels from everyone and open them to the role.
    const deny = (bit: number) => [
      { id: everyoneId, type: 'role', allow: 0, deny: bit },
      { id: role?.id, type: 'role', allow: bit, deny: 0 },
    ]
    expect((await apiFetch(a, 'PUT', `/api/admin/channels/${responseString(text, 'id')}/overwrites`, { overwrites: deny(VIEW_CHANNEL) })).status).toBe(204)
    expect((await apiFetch(a, 'PUT', `/api/admin/channels/${responseString(talk, 'id')}/overwrites`, { overwrites: deny(CONNECT) })).status).toBe(204)

    // Without the role: no private channel, and the Talk refuses.
    await b.reload()
    const secret = b.locator('[data-channel-type="text"]', { hasText: SECRET })
    const round = b.locator('[data-channel-type="voice"]', { hasText: TALK })
    await expect(round).toBeVisible()
    await expect(secret).toHaveCount(0)
    await round.click()
    await expect(b.getByText('Du darfst diesem Talk nicht beitreten.').first()).toBeVisible()
    await expect(b.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)

    // The Owner assigns the role in the user list; the member's page follows without a reload.
    await a.goto('/admin/users')
    await a.locator('tr', { hasText: suiteMember.username }).getByTestId('action-roles').click()
    await a.getByRole('dialog').locator(`[data-role="${ROLE}"] input`).check()
    await a.getByRole('dialog').getByRole('button', { name: 'Speichern' }).click()
    await expect(secret).toBeVisible()
    await round.click()
    await expect(b.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()

    // A holder of Manage roles still cannot touch the Owner.
    const refused = await apiFetch(b, 'PUT', `/api/admin/users/${ownerId}/roles`, { role_ids: [] })
    expect(refused.status).toBe(403)
    expect((await apiFetch(b, 'PUT', `/api/admin/users/${ownerId}/roles`, { role_ids: [role?.id] })).status).toBe(403)

    // Taking the role away removes the channel and ends the call.
    expect((await apiFetch(a, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: [] })).status).toBe(204)
    await expect(secret).toHaveCount(0)
    await expect(b.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)

    expect((await apiFetch(a, 'DELETE', `/api/admin/roles/${role?.id}`)).status).toBe(204)
  } finally {
    await aCtx.close()
    await bCtx.close()
  }
})
