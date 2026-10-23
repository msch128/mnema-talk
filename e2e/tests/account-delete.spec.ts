import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const MEMBER = `weg_${suffix}`
const PASSWORD = `pw-${suffix}-delete-test`
const CHANNEL = `abschied-${suffix}`
const WORDS = `private Worte von ${MEMBER}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
}

test('a member deletes the account; the thread stays, the words and the name are gone', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const mCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const m = await mCtx.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    await expect(a.getByTestId('account-menu-button')).toBeVisible()
    const channel = await apiFetch(a, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    const channelId = responseString(channel, 'id')
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })

    await m.goto('/')
    expect((await apiFetch(m, 'POST', '/api/auth/register', { username: MEMBER, password: PASSWORD, invite_code: responseString(invite, 'code') })).status).toBe(201)
    expect((await apiFetch(m, 'POST', `/api/channels/${channelId}/messages`, { content: WORDS })).status).toBe(201)
    await m.reload()

    // Delete from the Account & security dialog.
    await m.getByTestId('account-menu-button').click()
    await m.getByTestId('menu-account-security').click()
    await m.getByTestId('delete-start').click()
    await expect(m.getByTestId('delete-submit')).toBeDisabled()
    await m.getByTestId('delete-name').fill(MEMBER)
    await m.getByTestId('privacy-password').fill(PASSWORD)
    await m.getByTestId('delete-submit').click()

    // Back at the sign-in screen, and the old credentials are dead.
    await expect(m.getByRole('dialog').getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
    expect((await apiFetch(m, 'POST', '/api/auth/login', { username: MEMBER, password: PASSWORD })).status).toBe(401)

    // The Owner sees the message as written by a deleted user, without words.
    const list = await apiFetch(a, 'GET', `/api/channels/${channelId}/messages`)
    const messages = list.json as { username: string; display_name: string; content: string }[]
    expect(messages).toHaveLength(1)
    expect(messages[0]?.content).toBe('')
    expect(messages[0]?.display_name).toBe('Deleted member')
    expect(messages[0]?.username.startsWith('deleted-')).toBe(true)
    expect(JSON.stringify(list.json)).not.toContain(MEMBER)
    await a.goto(`/c/${channelId}`)
    await expect(a.getByText('Gelöschtes Mitglied')).toBeVisible()
  } finally {
    await aCtx.close()
    await mCtx.close()
  }
})
