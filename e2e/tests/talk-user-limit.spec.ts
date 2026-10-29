import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections, expectTalkLive, leaveTalks } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const TALK = `Limit ${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// A Talk with a limit of one: the administrator takes the seat, the counter
// reads 1/1 live for everybody, and a member without Disconnect members is
// told the Talk is full instead of being let into a failing join.
test('a full Talk shows N/limit and turns members away', async ({ browser }) => {
  const adminCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const memberCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await adminCtx.addInitScript(trackPeerConnections)
  const admin = await adminCtx.newPage()
  const member = await memberCtx.newPage()

  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
  const created = await apiFetch(admin, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })
  expect(created.status).toBe(201)
  expect((await apiFetch(admin, 'PATCH', `/api/admin/channels/${responseString(created, 'id')}`, { user_limit: 1 })).status).toBe(200)
  if (suiteMember.registered) {
    await signIn(member, suiteMember.username, suiteMember.password)
  } else {
    const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
    await member.goto('/')
    expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  await admin.reload()
  await member.reload()

  const row = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: TALK })
  const seats = (page: Page) => row(page).getByTestId('sidebar-talk-seats')
  await expect(seats(member)).toHaveText('0/1')

  await row(admin).click()
  await expectTalkLive(admin)
  await expect(seats(member)).toHaveText('1/1')
  await expect(seats(member)).toHaveAttribute('data-full', 'true')

  await row(member).click()
  await expect(member.getByText('Dieser Talk ist voll (1/1)')).toBeVisible()
  await expect(seats(admin)).toHaveText('1/1')

  await leaveTalks([admin])
  await expect(seats(member)).toHaveText('0/1')
  await adminCtx.close()
  await memberCtx.close()
})
