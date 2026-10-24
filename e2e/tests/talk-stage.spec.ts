import type { Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString, suiteMember, expectTalkLive, leaveTalks, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

// Stages (doc/talks.md): the audience cannot speak until a Stage manager gives
// them talk power; a raised hand is answered with one click; reactions float
// over the sender's tile. Needs a call (fake microphone), so it runs in CI only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const STAGE = `Stage ${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the audience of a Stage asks to speak, is accepted, and reacts', async ({ browser }) => {
  const ctxOptions = { permissions: ['microphone' as const], viewport: { width: 1400, height: 900 } }
  const aCtx = await browser.newContext(ctxOptions)
  const bCtx = await browser.newContext(ctxOptions)
  await aCtx.addInitScript(trackPeerConnections)
  await bCtx.addInitScript(trackPeerConnections)
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()

  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(a)) await a.reload()
  const created = await apiFetch(a, 'POST', '/api/admin/channels', { name: STAGE, type: 'voice', is_stage: true })
  expect(created.status).toBe(201)
  const stageId = responseString(created, 'id')

  await b.goto('/')
  if (!suiteMember.registered) {
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  } else {
    expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  }
  await b.reload()

  const row = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: STAGE })
  await row(a).click()
  await expectTalkLive(a)
  await row(b).click()
  await expectTalkLive(b)

  // The manager speaks; the member sits in the audience with the microphone off.
  await expect(a.getByTestId('stage-layout')).toBeVisible()
  await expect(a.getByTestId('stage-speakers')).toBeVisible()
  await expect(b.getByTestId('talk-mute')).toBeDisabled()
  await expect(b.getByTestId('stage-listener')).not.toHaveCount(0)

  // The member raises a hand; the manager sees it and accepts with one click.
  await b.getByTestId('stage-raise-hand').click()
  await expect(a.getByTestId('stage-hand')).toHaveCount(1)
  await a.getByTestId('stage-accept').click()
  await expect(a.getByTestId('stage-hand')).toHaveCount(0)
  await expect(b.getByTestId('talk-mute')).toBeEnabled()
  await expect(b.getByTestId('stage-raise-hand')).toHaveCount(0)

  // Talk power taken back: the microphone is disabled again.
  await a.getByTestId('stage-take').click()
  await expect(b.getByTestId('talk-mute')).toBeDisabled()

  // A live reaction floats over the sender's tile for the others.
  await b.getByTestId('talk-react').click()
  await b.getByTestId('reaction-palette').getByRole('button', { name: '🎉' }).click()
  await expect(a.getByTestId('talk-reaction').first()).toBeVisible()
  await expect(a.getByTestId('talk-reaction')).toHaveCount(0, { timeout: 10_000 })

  await leaveTalks([a, b])
  await apiFetch(a, 'DELETE', `/api/admin/channels/${stageId}`)
  await aCtx.close()
  await bCtx.close()
})
