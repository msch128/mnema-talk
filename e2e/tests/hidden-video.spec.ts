import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Hidden ${suffix}`

// A hidden page (a locked phone, a minimised window) is not looking at the
// shared screen: the server must stop sending its video while the voices go
// on, and send it again when the page is shown. Headless Chromium has no real
// hidden state, so the page is told what the browser would tell it.
const fakeScreenAndVisibility = () => {
  MediaDevices.prototype.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 1280
    canvas.height = 720
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas 2D context unavailable')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n * 3) % 360} 60% 40%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.fillStyle = '#fff'
      ctx.font = '48px sans-serif'
      for (let i = 0; i < 8; i++) ctx.fillText(`hidden ${n} ${i}`, 40 + ((n * 7 + i * 90) % 800), 60 + i * 55)
      n++
    }, 33)
    return canvas.captureStream(30)
  }
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) { super(...args); window.__pcs.push(this) }
  }
  let hidden = false
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (hidden ? 'hidden' : 'visible') })
  ;(window as unknown as { __setHidden: (h: boolean) => void }).__setHidden = (h) => {
    hidden = h
    document.dispatchEvent(new Event('visibilitychange'))
  }
}

const setHidden = (page: Page, hidden: boolean) => page.evaluate((h) => (window as unknown as { __setHidden: (h: boolean) => void }).__setHidden(h), hidden)

/** Bytes this page has received so far, per kind, over its peer connections. */
const received = (page: Page) => page.evaluate(async () => {
  const bytes = { video: 0, audio: 0 }
  for (const pc of window.__pcs) {
    for (const stat of (await pc.getStats()).values()) {
      if (stat.type !== 'inbound-rtp') continue
      if (stat.kind === 'video') bytes.video += stat.bytesReceived ?? 0
      if (stat.kind === 'audio') bytes.audio += stat.bytesReceived ?? 0
    }
  }
  return bytes
})

async function growth(page: Page, ms: number) {
  const before = await received(page)
  await page.waitForTimeout(ms)
  const after = await received(page)
  return { video: after.video - before.video, audio: after.audio - before.audio }
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a hidden page gets no video from the server, keeps the audio and gets the picture back when shown', async ({ browser }) => {
  test.setTimeout(180_000)
  const sharerCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const viewerCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  await sharerCtx.addInitScript(fakeScreenAndVisibility)
  await viewerCtx.addInitScript(fakeScreenAndVisibility)
  const sharer = await sharerCtx.newPage()
  const viewer = await viewerCtx.newPage()
  let channelId = ''
  try {
    await signIn(sharer, ADMIN_USER, ADMIN_PASSWORD)
    const created = await apiFetch(sharer, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
    expect(created.status).toBe(201)
    channelId = responseString(created, 'id')
    await viewer.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(viewer, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(sharer, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(viewer, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await viewer.reload()

    const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
    for (const page of [sharer, viewer]) {
      await sidebarVoice(page).click()
      await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    }
    await sharer.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
    await viewer.getByTestId('screen-card').getByRole('button', { name: 'Ansehen' }).click()
    await expect(viewer.getByTestId('stage')).toBeVisible({ timeout: 20_000 })
    const size = () => viewer.getByTestId('stage').locator('video').evaluate((v) => (v instanceof HTMLVideoElement ? v.videoWidth : 0))
    await expect.poll(size, { timeout: 30_000 }).toBeGreaterThan(0)
    expect((await growth(viewer, 3000)).video).toBeGreaterThan(20_000)

    // Hidden: the video stops at the server. The voices do not.
    await setHidden(viewer, true)
    await viewer.waitForTimeout(2000)
    const hidden = await growth(viewer, 4000)
    expect(hidden.video).toBeLessThan(4000)
    expect(hidden.audio).toBeGreaterThan(1000)

    // Shown: the picture comes back by itself, on the same stage.
    await setHidden(viewer, false)
    await expect.poll(async () => (await growth(viewer, 2000)).video, { timeout: 30_000 }).toBeGreaterThan(20_000)
    await expect.poll(size, { timeout: 30_000 }).toBeGreaterThan(0)
    await expect(viewer.getByTestId('stage')).toBeVisible()
  } finally {
    for (const page of [viewer, sharer]) await page.getByRole('button', { name: 'Verlassen' }).first().click({ timeout: 3000 }).catch(() => undefined)
    if (channelId) await apiFetch(sharer, 'DELETE', `/api/admin/channels/${channelId}`).catch(() => undefined)
    await viewerCtx.close()
    await sharerCtx.close()
  }
})
