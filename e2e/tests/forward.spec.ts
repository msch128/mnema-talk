import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const SOURCE = `fwd-quelle-${suffix}`
const TARGET = `fwd-ziel-${suffix}`
const HIDDEN = `fwd-intern-${suffix}`
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

test('forwarding quotes the original per reader: visible to who may see it, "not available" to everybody else', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  const created: string[] = []
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const source = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: SOURCE, type: 'text' }), 'id')
    created.push(source)
    const target = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: TARGET, type: 'text' }), 'id')
    created.push(target)
    const hidden = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: HIDDEN, type: 'text' }), 'id')
    created.push(hidden)
    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    const rolesJson = (await apiFetch(owner, 'GET', '/api/roles')).json as { id: string; is_everyone: boolean }[] | { roles?: { id: string; is_everyone: boolean }[] }
    const everyoneId = (Array.isArray(rolesJson) ? rolesJson : rolesJson.roles ?? []).find(r => r.is_everyone)?.id
    expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${hidden}/overwrites`, { overwrites: [{ id: everyoneId, type: 'role', allow: 0, deny: VIEW_CHANNEL }] })).status).toBe(204)
    expect((await apiFetch(owner, 'POST', `/api/channels/${source}/messages`, { content: 'oeffentliche Zeile' })).status).toBe(201)
    const secret = responseString(await apiFetch(owner, 'POST', `/api/channels/${hidden}/messages`, { content: 'GEHEIMER-WORTLAUT' }), 'id')

    // The member forwards a public message through the dialog.
    await member.goto(`/c/${source}`)
    await member.getByText('oeffentliche Zeile', { exact: true }).click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Weiterleiten …' }).click()
    await member.getByTestId(`forward-target-${TARGET}`).check()
    await member.getByTestId('forward-comment').fill('schau mal')
    await member.getByTestId('forward-send').click()
    await expect(member.getByTestId('forward-dialog')).toBeHidden()
    await member.goto(`/c/${target}`)
    await expect(member.getByText('schau mal', { exact: true })).toBeVisible()
    await expect(member.getByTestId('forwarded')).toContainText('oeffentliche Zeile')

    // The Owner forwards a message of a channel the member cannot see.
    expect((await apiFetch(owner, 'POST', `/api/messages/${secret}/forward`, { channel_id: target, comment: 'for your information' })).status).toBe(201)
    await expect(member.getByText('for your information', { exact: true })).toBeVisible()
    const unavailable = member.getByTestId('forwarded-unavailable')
    await expect(unavailable).toHaveText('Das Original ist nicht verfügbar.')
    await expect(member.locator('body')).not.toContainText('GEHEIMER-WORTLAUT')
    await expect(member.locator('body')).not.toContainText(HIDDEN)

    // The Owner sees the original of the same message.
    await owner.goto(`/c/${target}`)
    await expect(owner.getByTestId('forwarded').filter({ hasText: 'GEHEIMER-WORTLAUT' })).toBeVisible()
  } finally {
    // Leave the sidebar as found: later specs count on its size.
    for (const id of created) await apiFetch(owner, 'DELETE', `/api/admin/channels/${id}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})
