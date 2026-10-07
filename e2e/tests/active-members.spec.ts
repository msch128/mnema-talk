import { randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { decodeMessage, decodeUserEnvelope } from '../../web/src/types/domain'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('disabled members leave the live roster, retain history and return when enabled', async ({ browser }) => {
  const suffix = randomBytes(6).toString('hex')
  const username = `roster_${suffix}`
  const displayName = `Roster ${suffix}`
  const observerName = `observer_${suffix}`
  const password = `pw-${randomBytes(16).toString('hex')}`
  const channelName = `roster-${suffix}`
  const content = `History remains after disabling ${suffix}`
  const adminContext = await browser.newContext()
  const observerContext = await browser.newContext()
  const memberContext = await browser.newContext()
  try {
    const admin = await adminContext.newPage()
    const observer = await observerContext.newPage()
    const member = await memberContext.newPage()
    await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(admin, 'POST', '/api/admin/channels', { name: channelName, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 2 })
    expect(invite.status).toBe(201)
    const code = responseString(invite, 'code')
    let memberId = ''
    for (const [page, name, display] of [[member, username, displayName], [observer, observerName, observerName]] as const) {
      await page.goto('/')
      const registration = await apiFetch(page, 'POST', '/api/auth/register', {
        username: name, display_name: display, password, invite_code: code,
      })
      expect(registration.status).toBe(201)
      const registered = decodeUserEnvelope(registration.json)
      if (name === username) memberId = registered.user.id
      await page.reload()
      await expect(page.getByRole('dialog')).toBeHidden()
    }
    expect(memberId).not.toBe('')
    for (const page of [admin, observer, member]) {
      await page.locator('[data-channel-type="text"]', { hasText: channelName }).click()
    }
    const roster = (page: Page) => page.locator(`[data-member="${username}"]`)
    for (const page of [admin, observer]) await expect(roster(page)).toBeVisible()

    const composer = member.getByRole('textbox', { name: `Nachricht an #${channelName}` })
    await composer.fill(content)
    await composer.press('Enter')
    const messages = async () => {
      const response = await apiFetch(observer, 'GET', `/api/channels/${channelId}/messages`)
      expect(response.status).toBe(200)
      if (!Array.isArray(response.json)) throw new Error('Expected a message list')
      return response.json.map((value: unknown) => decodeMessage(value))
    }
    await expect(observer.getByText(content, { exact: true })).toBeVisible()
    const original = (await messages()).find(message => message.content === content)
    if (!original) throw new Error('Missing historical fixture message')
    expect(original.user_id).toBe(memberId)
    expect(original.display_name).toBe(displayName)
    const history = async () => {
      await expect(observer.getByText(content, { exact: true })).toBeVisible()
      await expect(observer.getByTestId('author-name').filter({ hasText: displayName })).toBeVisible()
      expect((await messages()).find(message => message.id === original.id)).toMatchObject({
        user_id: memberId, username, display_name: displayName, content,
      })
    }

    expect((await apiFetch(admin, 'POST', `/api/admin/users/${memberId}/disable`)).status).toBe(204)
    // These already-open pages must update through the real WebSocket event.
    for (const page of [admin, observer]) await expect(roster(page)).toHaveCount(0)
    await expect(member.getByRole('dialog')).toBeVisible()
    await history()
    await observer.reload()
    await expect(observer.getByRole('textbox', { name: `Nachricht an #${channelName}` })).toBeVisible()
    await expect(roster(observer)).toHaveCount(0)
    await history()

    expect((await apiFetch(admin, 'POST', `/api/admin/users/${memberId}/enable`)).status).toBe(204)
    for (const page of [admin, observer]) {
      await expect(roster(page)).toBeVisible()
      await expect(roster(page)).toHaveAttribute('data-status', 'offline')
    }
    await history()
    // Re-enabling cannot restore the session invalidated by disabling.
    expect((await apiFetch(member, 'GET', '/api/auth/me')).status).toBe(401)
    await signIn(member, username, password)
    await expect(roster(observer)).toHaveAttribute('data-status', 'online')
    await history()
  } finally {
    await adminContext.close()
    await observerContext.close()
    await memberContext.close()
  }
})
