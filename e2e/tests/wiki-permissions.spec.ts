import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

// Who may write the wiki (doc/wiki.md): reading is open to every member, editing
// needs "Edit the wiki", and "Manage the wiki" alone changes nothing. One member
// goes through the three cases, the Owner hands out the roles in between.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TITLE = `Guild charter ${suffix}`
const EDIT_WIKI = 1 << 27
const MANAGE_WIKI = 1 << 28

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a member who may only read sees a read-only wiki; Edit the wiki makes it editable; Manage the wiki alone does not', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  const roleIds: string[] = []
  let memberId = ''
  let slug = ''
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const created = await apiFetch(a, 'POST', '/api/wiki/pages', { title: TITLE, content: 'Be kind.' })
    expect(created.status).toBe(201)
    slug = responseString(created, 'slug')

    // The member (shared with the other specs) signs in or registers.
    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    memberId = responseUserId(await apiFetch(b, 'GET', '/api/auth/me'))

    // The member's roles are replaced, then the wiki is opened anew so nothing depends on a live update.
    const give = async (permissions: number | null) => {
      let roleIdsOfMember: string[] = []
      if (permissions !== null) {
        const role = await apiFetch(a, 'POST', '/api/admin/roles', { name: `Wiki ${permissions} ${suffix}`, permissions })
        expect(role.status).toBe(201)
        roleIdsOfMember = [responseString(role, 'id')]
        roleIds.push(...roleIdsOfMember)
      }
      expect((await apiFetch(a, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: roleIdsOfMember })).status).toBe(204)
      await b.goto(`/wiki/${slug}`)
      await expect(b.getByTestId('wiki-title')).toHaveText(TITLE)
    }

    // Read only: the page and its history, but no way to write.
    await give(null)
    await expect(b.getByTestId('wiki-content')).toContainText('Be kind.')
    await expect(b.getByTestId('wiki-history')).toBeVisible()
    await expect(b.getByTestId('wiki-edit')).toHaveCount(0)
    await expect(b.getByTestId('wiki-new-top')).toHaveCount(0)
    await expect(b.getByTestId('wiki-new')).toHaveCount(0)
    await expect(b.getByTestId('wiki-lock')).toHaveCount(0)
    await b.goto(`/wiki/${slug}/edit`)
    await expect(b.getByTestId('wiki-no-edit')).toBeVisible()
    await expect(b.getByTestId('wiki-editor')).toHaveCount(0)
    expect((await apiFetch(b, 'PATCH', `/api/wiki/pages/${slug}`, { base_rev: 1, content: 'vandalism' })).status).toBe(403)

    // Manage the wiki alone: still read only (managing needs editing too).
    await give(MANAGE_WIKI)
    await expect(b.getByTestId('wiki-edit')).toHaveCount(0)
    await expect(b.getByTestId('wiki-lock')).toHaveCount(0)
    await expect(b.getByTestId('wiki-delete')).toHaveCount(0)
    await expect(b.getByTestId('wiki-new-top')).toHaveCount(0)
    expect((await apiFetch(b, 'PATCH', `/api/wiki/pages/${slug}`, { base_rev: 1, content: 'managed' })).status).toBe(403)
    expect((await apiFetch(b, 'PUT', `/api/wiki/pages/${slug}/lock`, { locked: true })).status).toBe(403)

    // Edit the wiki: the page opens in the editor and the change is saved as revision 2.
    await give(EDIT_WIKI)
    await expect(b.getByTestId('wiki-new-top')).toBeVisible()
    await expect(b.getByTestId('wiki-lock')).toHaveCount(0)
    await b.getByTestId('wiki-edit').click()
    await b.getByTestId('wiki-text-input').fill('Be kind. Be on time.')
    await b.getByTestId('wiki-save').click()
    await expect(b.getByTestId('wiki-content')).toContainText('Be on time.')
    const saved = await apiFetch(a, 'GET', `/api/wiki/pages/${slug}`)
    expect((saved.json as { rev: number; content: string }).rev).toBe(2)
    // An editor still does not organise the tree.
    expect((await apiFetch(b, 'PATCH', `/api/wiki/pages/${slug}`, { slug: `${slug}-renamed` })).status).toBe(403)
  } finally {
    if (memberId) await apiFetch(a, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: [] })
    for (const id of roleIds) await apiFetch(a, 'DELETE', `/api/admin/roles/${id}`)
    if (slug) await apiFetch(a, 'DELETE', `/api/wiki/pages/${slug}`)
    await aCtx.close()
    await bCtx.close()
  }
})
