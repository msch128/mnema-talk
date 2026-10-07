import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId } from './helpers'
import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `viewer_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Stream ${suffix}`


// Headless Chromium has no screen to pick: a shared "screen" is an animated
// canvas, so the test exercises the app's real publish/subscribe path.
const fakeDisplayMedia = () => {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 640
    canvas.height = 360
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas 2D context unavailable')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 70% 50%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.fillStyle = '#fff'
      ctx.fillText(String(n), 20, 40)
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

test('a viewer sees a shared screen after choosing to watch it', async ({ browser }) => {
  const sharerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const viewerCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await sharerCtx.addInitScript(fakeDisplayMedia)
  const sharer = await sharerCtx.newPage()
  const viewer = await viewerCtx.newPage()
  // Browser warnings and errors end up in the test output on failure.
  for (const [name, page] of [['sharer', sharer], ['viewer', viewer]] as const) {
    page.on('console', (m) => { if (['warning', 'error'].includes(m.type())) console.log(`[${name}] ${m.text()}`) })
    page.on('pageerror', (e) => console.log(`[${name}] pageerror ${e.message}`))
  }

  await signIn(sharer, ADMIN_USER, ADMIN_PASSWORD)
  const me = await apiFetch(sharer, 'GET', '/api/auth/me')
  const sharerId = responseUserId(me)
  expect(sharerId).toBeTruthy()
  const voice = await apiFetch(sharer, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
  expect(voice.status).toBe(201)
  const invite = await apiFetch(sharer, 'POST', '/api/admin/invites', { max_uses: 1 })
  await viewer.goto('/')
  const reg = await apiFetch(viewer, 'POST', '/api/auth/register', {
    username: USER, password: USER_PASSWORD, invite_code: responseString(invite, 'code'),
  })
  expect(reg.status).toBe(201)
  await viewer.reload()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(sharer).click()
  await sidebarVoice(viewer).click()
  for (const page of [sharer, viewer]) {
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
  }

  // Start sharing: the viewer is offered the stream and opts in.
  await sharer.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  const card = viewer.getByTestId('screen-card')
  await expect(card).toBeVisible()
  await card.getByRole('button', { name: 'Ansehen' }).click()

  // Observe the selected publisher's stage, not any video on the page. A
  // frozen frame can still have a size and report !paused, so also require
  // advancing presentation times and changing pixels from the animated source.
  const stage = viewer.getByTestId('stage')
  await expect(stage).toHaveAttribute('data-stage-source', sharerId)
  const video = stage.locator('video')
  await expect.poll(() => video.evaluate(v => { if (!(v instanceof HTMLVideoElement)) throw new Error("Expected video"); return `${v.videoWidth}x${v.videoHeight}` }),
    { timeout: 20_000 }).toBe('640x360')
  const samples = await video.evaluate(v => new Promise<Array<{ time: number; color: string }>>(resolve => {
    if (!(v instanceof HTMLVideoElement)) throw new Error("Expected video")
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 1
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas 2D context unavailable')
    const frames: Array<{ time: number; color: string }> = []
    let callback: number | null = null
    let nextSample: ReturnType<typeof setTimeout> | null = null
    const finish = () => {
      clearTimeout(deadline)
      if (nextSample !== null) clearTimeout(nextSample)
      if (callback !== null) v.cancelVideoFrameCallback(callback)
      resolve(frames)
    }
    const deadline = setTimeout(finish, 5_000)
    const sample: VideoFrameRequestCallback = (_, metadata) => {
      ctx.drawImage(v, 0, 0, 1, 1, 0, 0, 1, 1)
      frames.push({ time: metadata.mediaTime, color: [...ctx.getImageData(0, 0, 1, 1).data].join(',') })
      if (frames.length === 4) finish()
      else nextSample = setTimeout(() => { callback = v.requestVideoFrameCallback(sample) }, 300)
    }
    callback = v.requestVideoFrameCallback(sample)
  }))
  expect(samples).toHaveLength(4)
  for (let i = 1; i < samples.length; i++) { const current = samples[i]; const previous = samples[i - 1]; if (!current || !previous) throw new Error('Expected frame samples'); expect(current.time).toBeGreaterThan(previous.time) }
  expect(new Set(samples.map(frame => frame.color)).size).toBeGreaterThan(1)

  await sharerCtx.close()
  await viewerCtx.close()
})
