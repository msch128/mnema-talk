import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

// Roles members choose themselves (doc/roles-and-permissions.md): an admin opens
// a role in the role editor, a member takes it under Settings > Roles and drops
// it again. No call is needed.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const ROLE = `Gamer ${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('an admin opens a role to everyone; a member takes it and drops it again', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  let roleId = ''
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const created = await apiFetch(a, 'POST', '/api/admin/roles', { name: ROLE, permissions: 0 })
    expect(created.status).toBe(201)
    roleId = responseString(created, 'id')

    // The shared member signs in or registers.
    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await b.reload()

    // Closed by default: the member has no Roles section and the server refuses.
    expect((await apiFetch(b, 'PUT', `/api/me/roles/${roleId}`)).status).toBe(403)

    // The admin opens the role in the editor and describes it.
    await a.goto('/admin/roles')
    await a.locator(`[data-role="${ROLE}"] button[aria-pressed]`).click()
    await a.getByTestId('role-description').fill('Pings for game nights')
    await a.getByTestId('role-self-assignable').check()
    await a.getByTestId('unsaved-save').click()
    await expect(a.getByTestId('unsaved-save')).toBeDisabled()

    // The member picks it up: the switch is off, then on, and the server agrees.
    await b.reload()
    await b.getByTestId('account-menu-button').click()
    await b.getByTestId('menu-settings').click()
    await b.getByTestId('settings-tab-roles').click()
    const row = b.locator(`[data-role="${ROLE}"]`)
    await expect(row).toContainText('Pings for game nights')
    const toggle = row.getByRole('switch')
    await expect(toggle).toHaveAttribute('aria-checked', 'false')
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
    await expect(b.getByTestId('saved-indicator').first()).toHaveText('Gespeichert')
    const held = await apiFetch(b, 'GET', '/api/permissions')
    expect((held.json as { role_ids?: string[] }).role_ids).toContain(roleId)

    // And drops it again.
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')
    await expect.poll(async () => ((await apiFetch(b, 'GET', '/api/permissions')).json as { role_ids?: string[] }).role_ids ?? []).not.toContain(roleId)
  } finally {
    if (roleId) await apiFetch(a, 'DELETE', `/api/admin/roles/${roleId}`)
    await aCtx.close()
    await bCtx.close()
  }
})
