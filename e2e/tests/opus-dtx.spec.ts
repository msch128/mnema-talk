import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Dtx ${suffix}`

// The browser is asked for discontinuous transmission on the microphone line
// (usedtx=1 in the SFU's offer), so a speaker who is quiet sends a short frame
// now and then instead of 50 packets a second, while the listener's audio goes
// on without a break (comfort noise fills the gaps). Chromium's fake microphone
// is silence with a short beep every second.

/** Audio the page received from the other members: packets and samples played out. */
const audioIn = (page: Page) => page.evaluate(async () => {
  const total = { packets: 0, samples: 0 }
  for (const pc of window.__pcs) {
    for (const stat of (await pc.getStats()).values()) {
      if (stat.type === 'inbound-rtp' && stat.kind === 'audio') {
        total.packets += stat.packetsReceived ?? 0
        total.samples += stat.totalSamplesReceived ?? 0
      }
    }
  }
  return total
})

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a quiet speaker sends few packets, the listener still plays continuous audio', async ({ browser }) => {
  test.setTimeout(120_000)
  const speakerCtx = await browser.newContext({ permissions: ['microphone'], viewport: { width: 1400, height: 900 } })
  const listenerCtx = await browser.newContext({ permissions: ['microphone'], viewport: { width: 1400, height: 900 } })
  await speakerCtx.addInitScript(trackPeerConnections)
  await listenerCtx.addInitScript(trackPeerConnections)
  const speaker = await speakerCtx.newPage()
  const listener = await listenerCtx.newPage()
  let channelId = ''
  try {
    await signIn(speaker, ADMIN_USER, ADMIN_PASSWORD)
    const created = await apiFetch(speaker, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
    expect(created.status).toBe(201)
    channelId = responseString(created, 'id')
    await listener.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(listener, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(speaker, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(listener, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await listener.reload()

    for (const page of [speaker, listener]) {
      await page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL }).click()
      await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    }
    await expect.poll(() => speaker.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 20_000 }).toBe('connected')

    // The offer the speaker's browser applied asks for DTX on the microphone line, and only there.
    const offer = await speaker.evaluate(() => window.__pcs.at(-1)?.remoteDescription?.sdp ?? '')
    expect(offer).toMatch(/a=fmtp:\d+ [^\r\n]*usedtx=1/)
    expect(offer.match(/usedtx=1/g)).toHaveLength(1)

    // Audio arrives at all, then over a window of silence and beeps: far fewer than 50 packets a second, no break in what is played out.
    await expect.poll(async () => (await audioIn(listener)).packets, { timeout: 30_000 }).toBeGreaterThan(5)
    const seconds = 8
    const before = await audioIn(listener)
    await listener.waitForTimeout(seconds * 1000)
    const after = await audioIn(listener)
    const perSecond = (after.packets - before.packets) / seconds
    expect(perSecond).toBeLessThan(30)
    expect(after.packets - before.packets).toBeGreaterThan(0)
    expect(after.samples - before.samples).toBeGreaterThan(seconds * 48000 * 0.9)
  } finally {
    for (const page of [listener, speaker]) await page.getByRole('button', { name: 'Verlassen' }).first().click({ timeout: 3000 }).catch(() => undefined)
    if (channelId) await apiFetch(speaker, 'DELETE', `/api/admin/channels/${channelId}`).catch(() => undefined)
    await listenerCtx.close()
    await speakerCtx.close()
  }
})
