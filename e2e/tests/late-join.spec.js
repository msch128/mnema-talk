import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `late_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Late ${suffix}`

const trackPeerConnections = () => {
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = function (...args) {
    const pc = new Native(...args)
    window.__pcs.push(pc)
    return pc
  }
  window.RTCPeerConnection.prototype = Native.prototype
  window.RTCPeerConnection.generateCertificate = Native.generateCertificate
}

// Audio bytes received on the current connection, from all other members.
const audioBytesIn = (page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  if (!pc) return 0
  let bytes = 0
  ;(await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') bytes += s.bytesReceived || 0 })
  return bytes
})

async function apiFetch(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const res = await fetch(path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, json: await res.json().catch(() => null) }
  }, { method, path, body })
}

// Someone joins a Talk where another member is already talking: both must
// hear each other (regression: the late joiner's microphone was never sent).
test('a member joining an occupied Talk is heard and hears', async ({ browser }) => {
  const firstCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const lateCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await firstCtx.addInitScript(trackPeerConnections)
  await lateCtx.addInitScript(trackPeerConnections)
  const first = await firstCtx.newPage()
  const late = await lateCtx.newPage()

  await first.goto('/')
  const dialog = first.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
  expect((await apiFetch(first, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })).status).toBe(201)
  const invite = await apiFetch(first, 'POST', '/api/admin/invites', { max_uses: 1 })
  await late.goto('/')
  expect((await apiFetch(late, 'POST', '/api/auth/register', { username: USER, password: USER_PASSWORD, invite_code: invite.json.code })).status).toBe(201)
  await first.reload()
  await late.reload()

  const sidebarVoice = (page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(first).click()
  await expect.poll(() => first.evaluate(() => window.__pcs.at(-1)?.connectionState)).toBe('connected')
  await first.waitForTimeout(500)

  // The second member joins the occupied Talk.
  await sidebarVoice(late).click()
  await expect.poll(() => late.evaluate(() => window.__pcs.at(-1)?.connectionState)).toBe('connected')

  // Audio flows both ways (the fake microphone sends a tone).
  for (const page of [first, late]) {
    await expect.poll(() => audioBytesIn(page), { timeout: 15_000 }).toBeGreaterThan(2000)
  }

  await firstCtx.close()
  await lateCtx.close()
})
