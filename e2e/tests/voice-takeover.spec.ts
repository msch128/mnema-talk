import type { Page } from '@playwright/test'
import { apiFetch, trackPeerConnections, expectTalkLive, leaveTalks } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `Takeover ${Date.now().toString(36)}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

const row = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: CHANNEL })
const question = (page: Page) => page.getByRole('dialog').filter({ hasText: 'Talk hier übernehmen?' })
const moved = (page: Page) => page.getByText('Dein Gespräch wurde auf ein anderes Gerät verschoben.')
const peerCount = (page: Page) => page.evaluate(() => window.__pcs.length)

// One account on two devices (two browser contexts, as a browser and the
// desktop client are): joining from the second asks, a yes moves the call
// there and only the call; the first can take it back, which moves it again.
test('a call moves between two sessions of one account', async ({ browser }) => {
  const ctxA = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const ctxB = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await ctxA.addInitScript(trackPeerConnections)
  await ctxB.addInitScript(trackPeerConnections)
  const a = await ctxA.newPage()
  const b = await ctxB.newPage()
  await signIn(a)
  await signIn(b)
  expect((await apiFetch(a, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'voice' })).status).toBe(201)
  await a.reload()
  await b.reload()

  await row(a).click()
  await expectTalkLive(a)

  // Joining from B asks first; "Abbrechen" changes nothing on either side.
  await row(b).click()
  await expect(question(b)).toBeVisible()
  expect(await peerCount(b)).toBe(0)
  await question(b).getByRole('button', { name: 'Abbrechen' }).click()
  await expect(question(b)).toBeHidden()
  expect(await peerCount(b)).toBe(0)
  await expectTalkLive(a)

  // Browser to client: B takes over, A's call ends with a notice, not an error.
  await row(b).click()
  await question(b).getByRole('button', { name: 'Hier übernehmen' }).click()
  await expectTalkLive(b)
  await expect(moved(a)).toBeVisible()
  await expect(a.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
  // Only the voice ended: A is still signed in and still sees the Talk's member.
  await expect(a.locator('nav [data-voice-user]')).toHaveCount(1)

  // And back: client to browser, without another question.
  await a.getByRole('button', { name: 'Zurück hierher' }).click()
  await expect(question(a)).toBeHidden()
  await expectTalkLive(a)
  await expect(moved(b)).toBeVisible()
  await expect(b.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)

  await leaveTalks([a])
  await ctxA.close()
  await ctxB.close()
})
