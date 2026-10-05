import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

// Throwaway account for this run; the password is generated, never committed.
const suffix = Date.now().toString(36)
const USER = `friend_${suffix}`
const USER_DISPLAY = `Friend ${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const TEXT_CHANNEL = 'allgemein'
const VOICE_CHANNEL = 'Tafelrunde'

// Same-origin fetch from inside the page, so cookies and the Origin header
// behave exactly like the real app (CSRF origin check included).
async function apiFetch(page, method, path, body) {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        credentials: 'same-origin',
        headers: body ? { 'Content-Type': 'application/json' } : {},
        body: body ? JSON.stringify(body) : undefined,
      })
      return { status: res.status, json: await res.json().catch(() => null) }
    },
    { method, path, body },
  )
}

// Records every RTCPeerConnection so the test can assert that the SFU
// transport (ICE + DTLS) really connected, not just that the UI says so.
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

async function signIn(page, username, password) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('login, chat and talk', async ({ browser }) => {
  const adminCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const userCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await adminCtx.addInitScript(trackPeerConnections)
  await userCtx.addInitScript(trackPeerConnections)
  const admin = await adminCtx.newPage()
  const user = await userCtx.newPage()

  // 1. Admin logs in through the UI (account seeded from ADMIN_INITIAL_PASSWORD).
  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)

  // 2. Fresh instance has no channels: create a text and a voice channel via the API.
  const text = await apiFetch(admin, 'POST', '/api/admin/channels', { name: TEXT_CHANNEL, type: 'text' })
  expect(text.status).toBe(201)
  const voice = await apiFetch(admin, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
  expect(voice.status).toBe(201)

  // 3. Admin creates an invite.
  const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  expect(invite.status).toBe(201)
  const code = invite.json.code
  expect(code).toBeTruthy()

  // 4. Second user registers with the invite through the UI.
  await user.goto('/')
  const dialog = user.getByRole('dialog')
  await dialog.getByRole('button', { name: 'Hast du einen Einladungscode? Hier registrieren' }).click()
  await dialog.getByLabel('Benutzername').fill(USER)
  await dialog.getByLabel('Anzeigename').fill(USER_DISPLAY)
  await dialog.getByLabel('Passwort').fill(USER_PASSWORD)
  await dialog.getByLabel('Einladungscode').fill(code)
  await dialog.getByRole('button', { name: 'Registrieren' }).click()
  await expect(dialog).toBeHidden()

  // 5. The user opens the text channel and sends a message; the admin sees it live.
  const sidebarText = (page) => page.locator('[data-channel-type="text"]', { hasText: TEXT_CHANNEL })
  await sidebarText(admin).click()
  await sidebarText(user).click()

  const message = `Hallo aus dem Smoke-Test ${suffix}`
  const composer = user.getByRole('textbox', { name: `Nachricht an #${TEXT_CHANNEL}` })
  await composer.fill(message)
  await composer.press('Enter')
  await expect(user.getByText(message)).toBeVisible()
  await expect(admin.getByText(message)).toBeVisible()

  // 6. Both join the voice channel and see each other in the Tafelrunde.
  const sidebarVoice = (page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(admin).click()
  await sidebarVoice(user).click()

  for (const page of [admin, user]) {
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    await expect(page.getByText(USER_DISPLAY).first()).toBeVisible()
    await expect(page.getByText(ADMIN_USER).first()).toBeVisible()
  }
  // The WebRTC transport to the SFU is actually up on both sides.
  for (const page of [admin, user]) {
    await expect
      .poll(() => page.evaluate(() => window.__pcs.some((pc) => pc.connectionState === 'connected')))
      .toBe(true)
  }
  // Both participants are listed under the voice channel for everyone.
  const members = (page) => sidebarVoice(page).locator('xpath=following-sibling::div[1]')
  await expect(members(admin)).toContainText(USER_DISPLAY)
  await expect(members(admin)).toContainText(ADMIN_USER)

  // 7. Leave; the participant list empties.
  await user.getByRole('button', { name: 'Verlassen' }).first().click()
  await expect(members(admin)).not.toContainText(USER_DISPLAY)
  await admin.getByRole('button', { name: 'Verlassen' }).first().click()
  await expect(admin.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)

  await adminCtx.close()
  await userCtx.close()
})
