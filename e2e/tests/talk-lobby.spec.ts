import type { Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString, suiteMember, leaveTalks, expectTalkLive, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

// Create-a-Talk lobbies (doc/talks.md). Joining a lobby opens a temporary Talk;
// the opener leads it; when the leader leaves, the member present longest does.
// Needs a call (fake microphone), so it runs in CI only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const LOBBY = `Lobby ${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('joining a lobby opens a Talk, its leader hands over, and the empty Talk goes away', async ({ browser }) => {
  test.setTimeout(240_000) // two calls, a hand-over and the removal grace
  const ctxOptions = { permissions: ['microphone' as const], viewport: { width: 1400, height: 900 } }
  const aCtx = await browser.newContext(ctxOptions)
  const bCtx = await browser.newContext(ctxOptions)
  // expectTalkLive reads window.__pcs, which this records.
  for (const ctx of [aCtx, bCtx]) await ctx.addInitScript(trackPeerConnections)
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()

  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(a)) await a.reload()
  const created = await apiFetch(a, 'POST', '/api/admin/channels', { name: LOBBY, type: 'voice' })
  expect(created.status).toBe(201)
  const lobbyId = responseString(created, 'id')
  expect((await apiFetch(a, 'PATCH', `/api/admin/channels/${lobbyId}`, { is_lobby: true })).status).toBe(200)

  await b.goto('/')
  if (!suiteMember.registered) {
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  } else {
    expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  }
  await b.reload()

  // The admin joins the lobby and lands in a new Talk, not in the lobby.
  const lobbyRow = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: LOBBY })
  await lobbyRow(a).click()
  await expectTalkLive(a)
  const talks = a.locator('[data-channel-type="voice"]', { hasText: /Talk \d+/ })
  await expect(talks.first()).toBeVisible()
  await expect(lobbyRow(a).locator('xpath=..').locator('[data-voice-user]')).toHaveCount(0)

  // The first Talk shows its leader.
  const firstTalk = a.locator('[data-channel-item]', { has: a.locator('[data-voice-user]') }).first()
  await expect(firstTalk.getByTestId('talk-leader-mark')).toBeVisible()

  // A second member joins that Talk; the admin leaves; the member leads now.
  await b.locator('[data-channel-type="voice"]', { hasText: /Talk \d+/ }).first().click()
  await expectTalkLive(b)
  await a.getByRole('button', { name: 'Verlassen' }).first().click()
  await expect(b.locator('[data-voice-user]').getByTestId('talk-leader-mark')).toBeVisible({ timeout: 15_000 })

  // The leader can rename the Talk without Manage channels.
  const sidebarTalk = b.locator('[data-channel-item]', { has: b.locator('[data-voice-user]') }).first()
  await sidebarTalk.locator('[data-channel-type="voice"]').click({ button: 'right' })
  await b.getByRole('menuitem', { name: 'Talk-Einstellungen' }).click()
  await b.getByTestId('talk-name').fill(`Raid ${suffix}`)
  await b.getByTestId('talk-name').press('Enter')
  await expect(b.locator('[data-channel-type="voice"]', { hasText: `Raid ${suffix}` })).toBeVisible()
  // The dialog stays open after saving and would cover the Leave button.
  await b.keyboard.press('Escape')
  await expect(b.getByTestId('talk-name')).toHaveCount(0)

  // Everyone leaves: the empty Talk is removed after the grace time.
  await leaveTalks([b])
  await expect(b.locator('[data-channel-type="voice"]', { hasText: `Raid ${suffix}` })).toHaveCount(0, { timeout: 60_000 })

  await apiFetch(a, 'DELETE', `/api/admin/channels/${lobbyId}`)
  await aCtx.close()
  await bCtx.close()
})
