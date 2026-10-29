import type { Browser, Page } from '@playwright/test'
import { apiFetch, audioDecodedIn, responseString, suiteMember, trackPeerConnections, leaveTalks } from './helpers'
import { test, expect } from './fixtures'

// What every supported engine must do for call media: seal it in the browser
// before it leaves, and refuse to take part where that is not possible. The
// specs run on Chromium, WebKit and Firefox alike.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Sealed ${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// Every audio line of the call runs through a frame transform.
const transforms = (page: Page) => page.evaluate(() => {
  const pc = window.__pcs.at(-1)
  if (!pc) return null
  const lines = pc.getTransceivers().filter(t => t.mid !== null)
  const sealed = (t: RTCRtpSender | RTCRtpReceiver) => (t as { transform?: unknown }).transform != null
  return {
    senders: lines.filter(t => t.sender.track?.kind === 'audio').map(t => sealed(t.sender)),
    receivers: lines.filter(t => t.receiver.track.kind === 'audio').map(t => sealed(t.receiver)),
  }
})

async function memberFor(admin: Page, member: Page) {
  if (suiteMember.registered) {
    await signIn(member, suiteMember.username, suiteMember.password)
    return
  }
  await member.goto('/')
  const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  const reg = await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })
  expect(reg.status).toBe(201)
  suiteMember.registered = true
  await member.reload()
}

// Playwright's Linux WebKit (WPE/GTK, version 26.6 like the macOS build) ships
// WebRTC but not the encoded-media API: RTCRtpScriptTransform,
// RTCRtpSender.createEncodedStreams and RTCEncodedAudioFrame are all undefined,
// so the app correctly refuses to join a call there (the second spec below
// proves that path). Real Safari and macOS WebKit have the API. Only that exact
// gap skips the spec, so a build that gains the API runs it again.
async function lacksFrameTransformOnLinuxWebKit(browser: Browser) {
  if (browser.browserType().name() !== 'webkit' || process.platform !== 'linux') return false
  const probe = await browser.newContext()
  try {
    const page = await probe.newPage()
    return await page.evaluate(() => typeof RTCRtpScriptTransform === 'undefined')
  } finally {
    await probe.close()
  }
}

test('call media is sealed on every audio line and arrives decoded', async ({ browser }) => {
  test.skip(await lacksFrameTransformOnLinuxWebKit(browser), 'Playwright Linux WebKit has no RTCRtpScriptTransform; calls cannot be sealed there')
  const contexts = await Promise.all([0, 1].map(() => browser.newContext({ permissions: ['microphone', 'camera'] })))
  const [adminCtx, memberCtx] = contexts as [typeof contexts[0], typeof contexts[0]]
  for (const ctx of contexts) {
    await ctx.addInitScript(trackPeerConnections)
  }
  const admin = await adminCtx.newPage()
  const member = await memberCtx.newPage()
  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
  expect((await apiFetch(admin, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })).status).toBe(201)
  await memberFor(admin, member)
  await admin.reload()

  // The engine has what sealing needs.
  for (const page of [admin, member]) {
    expect(await page.evaluate(() => typeof RTCRtpScriptTransform)).toBe('function')
    expect(await page.evaluate(() => crypto.subtle.generateKey('Ed25519', false, ['sign', 'verify']).then(() => true, () => false))).toBe(true)
  }

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(admin).click()
  await sidebarVoice(member).click()
  for (const page of [admin, member]) {
    await expect.poll(() => page.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 20_000 }).toBe('connected')
    await expect.poll(async () => (await transforms(page))?.senders.length, { timeout: 15_000 }).toBeGreaterThan(0)
  }

  // Nothing goes out, and nothing comes in, outside the frame transform.
  for (const page of [admin, member]) {
    const lines = await transforms(page)
    expect(lines?.senders.every(Boolean)).toBe(true)
    expect(lines?.receivers.length).toBeGreaterThan(0)
    expect(lines?.receivers.every(Boolean)).toBe(true)
  }

  // The frames open and decode: two seconds of audio arrive on both sides.
  for (const page of [admin, member]) {
    await expect.poll(() => audioDecodedIn(page), { timeout: 20_000 }).toBeGreaterThan(2 * 48_000)
  }

  // Both ended up with the same keys, which the safety code shows.
  const code = async (page: Page) => {
    await page.getByTestId('voice-panel-ping').click()
    const field = page.getByTestId('media-safety-code')
    await expect(field).toHaveText(/^\d{5} \d{5} \d{5} \d{5}$/, { timeout: 15_000 })
    const text = await field.textContent()
    await page.keyboard.press('Escape')
    return text
  }
  await expect.poll(async () => (await code(admin)) === (await code(member)), { timeout: 30_000 }).toBe(true)

  await leaveTalks([admin, member])
  await adminCtx.close()
  await memberCtx.close()
})

test('a browser that cannot seal media does not join, and sends nothing', async ({ browser }) => {
  const ctx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await ctx.addInitScript(trackPeerConnections)
  // The engine without the frame transform that reaches encoded media.
  await ctx.addInitScript(() => {
    Object.defineProperty(window, 'RTCRtpScriptTransform', { value: undefined, configurable: true })
    window.__micRequests = 0
    const ask = MediaDevices.prototype.getUserMedia
    MediaDevices.prototype.getUserMedia = function (...args: Parameters<typeof ask>) {
      window.__micRequests += 1
      return ask.apply(this, args)
    }
  })
  const page = await ctx.newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const channel = `Unsealed ${suffix}`
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  await page.reload()

  await page.locator('[data-channel-type="voice"]', { hasText: channel }).click()
  await expect(page.getByText(/Dieser Browser kann die Medien eines Talks nicht schützen/)).toBeVisible()

  // No call: no leave button, no peer connection, no microphone request.
  await expect(page.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
  expect(await page.evaluate(() => window.__pcs.length)).toBe(0)
  expect(await page.evaluate(() => window.__micRequests)).toBe(0)
  await ctx.close()
})
