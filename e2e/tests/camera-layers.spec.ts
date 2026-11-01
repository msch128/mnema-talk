import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, trackPeerConnections, leaveTalks } from './helpers'
import { randomBytes } from 'node:crypto'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `camlay_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Camlayers ${suffix}`

// A shared "screen" is an animated canvas (the sharer's camera is the browser's fake device).
const fakeDisplayMedia = () => {
  MediaDevices.prototype.getDisplayMedia = async () => {
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
    return canvas.captureStream(20)
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

// The picture a tile or the stage is showing of the camera, and how many frames it has presented.
const showing = (page: Page, selector: string) => page.locator(selector).evaluate(v => {
  if (!(v instanceof HTMLVideoElement)) throw new Error('Expected video')
  return { height: v.videoHeight, frames: v.getVideoPlaybackQuality().totalVideoFrames }
})

// What the sharer sends: the video lines that are on, and how far each is scaled down.
const sendingLayers = (page: Page) => page.evaluate(() => {
  const senders = window.__pcs.at(-1)?.getSenders().filter(s => s.track?.kind === 'video') ?? []
  return senders.flatMap(s => s.getParameters().encodings ?? []).filter(e => e.active !== false).map(e => e.scaleResolutionDownBy ?? 1)
})

test('a small tile gets the camera\'s thumbnail layer and the stage its best one, without a break', async ({ browser }) => {
  const sharerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const viewerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await sharerCtx.addInitScript(trackPeerConnections)
  await sharerCtx.addInitScript(fakeDisplayMedia)
  const sharer = await sharerCtx.newPage()
  const viewer = await viewerCtx.newPage()

  // A failure must not leave the sharer in the Talk: the next spec signs in as the same account.
  try {
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

    // The sharer turns on the camera and shares a screen; the viewer watches the screen, so the camera is a small tile under the stage.
    await sharer.getByRole('button', { name: 'Kamera einschalten' }).first().click()
    await sharer.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
    await viewer.getByTestId('screen-card').getByRole('button', { name: 'Ansehen' }).click()
    const stage = viewer.getByTestId('stage')
    await expect(stage).toHaveAttribute('data-stage-source', sharerId, { timeout: 20_000 })

    const tile = `[data-participant-tile][data-user-id="${sharerId}"]`
    const tileVideo = `${tile} video`
    await expect(viewer.locator(tileVideo)).toBeVisible({ timeout: 20_000 })
    // The thumbnail: 180 pixels tall, whatever the camera's best layer is.
    await expect.poll(async () => (await showing(viewer, tileVideo)).height, { timeout: 30_000 }).toBe(180)
    const small = await showing(viewer, tileVideo)
    await expect.poll(async () => (await showing(viewer, tileVideo)).frames, { timeout: 15_000 }).toBeGreaterThan(small.frames + 5)
    // The sharer sends the thumbnail (a scaled-down layer is on), and the best layer only while someone needs it.
    await expect.poll(async () => (await sendingLayers(sharer)).some(scale => scale > 1.5), { timeout: 20_000 }).toBe(true)

    // The viewer puts the camera on the stage: the picture grows, the SFU moves them to the best layer on the same track.
    await viewer.locator(tile).click({ position: { x: 6, y: 6 } })
    await expect(stage).toHaveAttribute('data-stage-source', `camera:${sharerId}`)
    const stageVideo = '[data-testid="stage"] video'
    await expect.poll(async () => (await showing(viewer, stageVideo)).height, { timeout: 30_000 }).toBeGreaterThanOrEqual(360)
    const big = await showing(viewer, stageVideo)
    await expect.poll(async () => (await showing(viewer, stageVideo)).frames, { timeout: 15_000 }).toBeGreaterThan(big.frames + 5)
    await expect.poll(async () => (await sendingLayers(sharer)).some(scale => scale === 1), { timeout: 20_000 }).toBe(true)

    // And back: the stage shows the screen again, the tile the thumbnail.
    await stage.getByTestId('stage-unfocus-camera').click()
    await expect(stage).toHaveAttribute('data-stage-source', sharerId)
    await expect.poll(async () => (await showing(viewer, tileVideo)).height, { timeout: 30_000 }).toBe(180)

    await leaveTalks([sharer, viewer])
  } finally {
    await sharerCtx.close()
    await viewerCtx.close()
  }
})
