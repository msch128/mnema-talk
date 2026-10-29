import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections, leaveTalks, expectTalkLive } from './helpers'
import { test, expect } from './fixtures'

// The connection panel's "audio sent" must grow while the microphone sends.
// The call has the browser's fake microphone, so it runs in CI only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// The number in the panel's "Audio gesendet" line, e.g. "1.234 Pakete (47 kbit/s) · 96 KB".
async function panelPacketsSent(page: Page): Promise<number> {
  const text = await page.locator('[data-stat="mic"]').innerText()
  const digits = /([\d.]+)\s+Paket/.exec(text)?.[1]?.replaceAll('.', '')
  return digits ? Number(digits) : -1
}

test('the connection panel counts audio packets up while the microphone sends', async ({ browser }) => {
  const ctxA = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const ctxB = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await ctxA.addInitScript(trackPeerConnections)
  await ctxB.addInitScript(trackPeerConnections)
  const admin = await ctxA.newPage()
  const member = await ctxB.newPage()

  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
  const channel = `Stats ${suffix}`
  expect((await apiFetch(admin, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
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

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
  await sidebarVoice(admin).click()
  await expectTalkLive(admin)
  await sidebarVoice(member).click()
  await expectTalkLive(member)

  await admin.getByTestId('voice-panel-ping').click()
  const mic = admin.locator('[data-stat="mic"]')
  await expect(mic).toBeVisible()
  await expect.poll(() => panelPacketsSent(admin), { timeout: 20_000 }).toBeGreaterThan(0)
  const first = await panelPacketsSent(admin)
  // The panel refreshes every second; two seconds on, the count has grown.
  await expect.poll(() => panelPacketsSent(admin), { timeout: 15_000, intervals: [500] }).toBeGreaterThan(first + 20)

  await leaveTalks([admin, member])
  await ctxA.close()
  await ctxB.close()
})
