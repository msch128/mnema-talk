import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const ROLE = `Prüfer ${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('admin actions appear in the audit log, without secrets, and only for those who may read it', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const role = await apiFetch(a, 'POST', '/api/admin/roles', { name: ROLE, permissions: 0 })
    expect(role.status).toBe(201)
    // 16+ characters: shorter custom codes need a digit and the suffix may have none.
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { code: `geheim-${suffix}-audit` })
    expect(invite.status).toBe(201)

    await a.goto('/admin/audit')
    await expect(a.getByTestId('tab-audit')).toBeVisible()
    const roleEntry = a.locator('[data-testid^="audit-entry-"]', { hasText: ROLE }).first()
    await expect(roleEntry).toContainText('Rolle erstellt')
    await expect(roleEntry).toContainText(ADMIN_USER)
    await expect(a.locator('[data-testid^="audit-entry-"]', { hasText: 'Einladung erstellt' }).first()).toBeVisible()

    // The filter narrows the list to one group.
    await a.getByLabel('Nach Ereignis filtern').selectOption('invite')
    await a.getByRole('button', { name: 'Anwenden' }).click()
    await expect(a.locator('[data-testid^="audit-entry-"]', { hasText: 'Rolle erstellt' })).toHaveCount(0)

    // The invite code never reaches the log.
    await a.locator('[data-testid^="audit-entry-"]', { hasText: 'Einladung erstellt' }).first().getByRole('button').click()
    await expect(a.locator('pre').first()).not.toContainText(`geheim-${suffix}-audit`)

    // A member without the permission neither sees the tab nor the API.
    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const inv = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(inv, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    expect((await apiFetch(b, 'GET', '/api/admin/audit-log')).status).toBe(403)

    expect((await apiFetch(a, 'DELETE', `/api/admin/roles/${responseString(role, 'id')}`)).status).toBe(204)
  } finally {
    await aCtx.close()
    await bCtx.close()
  }
})
