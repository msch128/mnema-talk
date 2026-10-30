import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, trackPeerConnections, leaveTalks } from './helpers'
import { randomBytes } from 'node:crypto'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `qual_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Quality ${suffix}`

declare global {
  interface Window { __displayCalls: number }
}

// Headless Chromium has no screen to pick: the shared "screen" is an animated canvas. Every pick is counted.
const fakeDisplayMedia = () => {
  window.__displayCalls = 0
  MediaDevices.prototype.getDisplayMedia = async () => {
    window.__displayCalls++
    const canvas = document.createElement('canvas')
    canvas.width = 640
    canvas.height = 360
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas 2D context unavailable')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 70% 50%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
    }, 50)
    return canvas.captureStream(30)
  }
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// What the viewer's stage plays: the stream and track it is built on, and how many frames it has presented.
const playing = (page: Page) => page.getByTestId('stage').locator('video').evaluate(v => {
  if (!(v instanceof HTMLVideoElement)) throw new Error('Expected video')
  const stream = v.srcObject instanceof MediaStream ? v.srcObject : null
  return { streamId: stream?.id ?? '', trackId: stream?.getVideoTracks()[0]?.id ?? '', frames: v.getVideoPlaybackQuality().totalVideoFrames }
})

// What the sharer sends: the screen line's track and its frame-rate cap.
const sending = (page: Page) => page.evaluate(() => {
  const sender = window.__pcs.at(-1)?.getSenders().find(s => s.track?.kind === 'video')
  return { trackId: sender?.track?.id ?? '', maxFramerate: sender?.getParameters().encodings?.[0]?.maxFramerate ?? 0 }
})

test('changing frame rate in a running stream keeps the stream, the capture and the viewer\'s picture', async ({ browser }) => {
  const sharerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const viewerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await sharerCtx.addInitScript(trackPeerConnections)
  await sharerCtx.addInitScript(fakeDisplayMedia)
  const sharer = await sharerCtx.newPage()
  const viewer = await viewerCtx.newPage()

  await signIn(sharer, ADMIN_USER, ADMIN_PASSWORD)
  const sharerId = responseUserId(await apiFetch(sharer, 'GET', '/api/auth/me'))
  expect((await apiFetch(sharer, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })).status).toBe(201)
  const invite = await apiFetch(sharer, 'POST', '/api/admin/invites', { max_uses: 1 })
  await viewer.goto('/')
  expect((await apiFetch(viewer, 'POST', '/api/auth/register', { username: USER, password: USER_PASSWORD, invite_code: responseString(invite, 'code') })).status).toBe(201)
  await viewer.reload()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(sharer).click()
  await sidebarVoice(viewer).click()
  for (const page of [sharer, viewer]) await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()

  await sharer.getByTestId('voice-panel-share').click()
  const card = viewer.getByTestId('screen-card')
  await expect(card).toBeVisible()
  await card.getByRole('button', { name: 'Ansehen' }).click()
  await expect(viewer.getByTestId('stage')).toHaveAttribute('data-stage-source', sharerId)
  await expect.poll(async () => (await playing(viewer)).frames, { timeout: 20_000 }).toBeGreaterThan(5)
  const before = { viewer: await playing(viewer), sender: await sending(sharer) }
  expect(before.sender.maxFramerate).toBe(30)

  // Right click on the stream button: frame rate 15, no restart.
  await sharer.getByTestId('voice-panel-share').click({ button: 'right' })
  await sharer.locator('[data-submenu-trigger="fps"]').click()
  await sharer.locator('[data-menu-item="fps-15"]').click()
  await expect.poll(async () => (await sending(sharer)).maxFramerate).toBe(15)

  // The viewer keeps getting new frames, on the same stream and track.
  const frames = (await playing(viewer)).frames
  await expect.poll(async () => (await playing(viewer)).frames, { timeout: 15_000 }).toBeGreaterThan(frames + 5)
  const after = { viewer: await playing(viewer), sender: await sending(sharer) }
  expect(after.viewer.streamId).toBe(before.viewer.streamId)
  expect(after.viewer.trackId).toBe(before.viewer.trackId)
  expect(after.sender.trackId).toBe(before.sender.trackId)
  expect(await sharer.evaluate(() => window.__displayCalls)).toBe(1)
  await expect(sharer.getByTestId('voice-panel-share')).toHaveAttribute('aria-pressed', 'true')

  // The menu stays open for further choices and covers the leave button: close it before leaving.
  await sharer.keyboard.press('Escape')
  await sharer.keyboard.press('Escape')
  await expect(sharer.getByRole('menu')).toHaveCount(0)

  await leaveTalks([sharer, viewer])
  await sharerCtx.close()
  await viewerCtx.close()
})
