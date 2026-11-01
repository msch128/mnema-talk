import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Cams ${suffix}`
const TEXT_CHANNEL = `cams-text-${suffix}`

// A camera is only received while its tile is on screen: a member who looks at
// a text channel during the call must get no camera bytes from the server, and
// the picture must come back when the Talk is shown again.

/** Video bytes this page has received so far over its peer connections. */
const videoBytes = (page: Page) => page.evaluate(async () => {
  let bytes = 0
  for (const pc of window.__pcs) {
    for (const stat of (await pc.getStats()).values()) {
      if (stat.type === 'inbound-rtp' && stat.kind === 'video') bytes += stat.bytesReceived ?? 0
    }
  }
  return bytes
})

async function growth(page: Page, ms: number) {
  const before = await videoBytes(page)
  await page.waitForTimeout(ms)
  return (await videoBytes(page)) - before
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a camera whose tile is not on screen is not sent, and comes back with the tile', async ({ browser }) => {
  test.setTimeout(180_000)
  const publisherCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const viewerCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  await publisherCtx.addInitScript(trackPeerConnections)
  await viewerCtx.addInitScript(trackPeerConnections)
  const publisher = await publisherCtx.newPage()
  const viewer = await viewerCtx.newPage()
  const created: string[] = []
  try {
    await signIn(publisher, ADMIN_USER, ADMIN_PASSWORD)
    const voice = await apiFetch(publisher, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
    expect(voice.status).toBe(201)
    created.push(responseString(voice, 'id'))
    const text = await apiFetch(publisher, 'POST', '/api/admin/channels', { name: TEXT_CHANNEL, type: 'text' })
    expect(text.status).toBe(201)
    created.push(responseString(text, 'id'))

    await viewer.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(viewer, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(publisher, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(viewer, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await viewer.reload()

    const voiceRow = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
    for (const page of [publisher, viewer]) {
      await voiceRow(page).click()
      await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    }
    await publisher.getByRole('button', { name: 'Kamera einschalten' }).first().click()
    await expect.poll(() => growth(viewer, 2000), { timeout: 30_000 }).toBeGreaterThan(20_000)

    // Looking at a text channel during the call: the camera tile is gone from the screen, so is the camera.
    await viewer.locator('[data-channel-type="text"]', { hasText: TEXT_CHANNEL }).click()
    await expect(viewer.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    await viewer.waitForTimeout(3000)
    expect(await growth(viewer, 4000)).toBeLessThan(4000)

    // Back in the Talk: the tile is on screen again and the picture returns by itself.
    await voiceRow(viewer).click()
    await expect.poll(() => growth(viewer, 2000), { timeout: 30_000 }).toBeGreaterThan(20_000)
  } finally {
    for (const page of [viewer, publisher]) await page.getByRole('button', { name: 'Verlassen' }).first().click({ timeout: 3000 }).catch(() => undefined)
    for (const id of created) await apiFetch(publisher, 'DELETE', `/api/admin/channels/${id}`).catch(() => undefined)
    await viewerCtx.close()
    await publisherCtx.close()
  }
})
