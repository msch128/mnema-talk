import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'
import { randomBytes } from 'node:crypto'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Layers ${suffix}`
// Registrations are limited to ten an hour from one address and the whole
// suite shares them: viewer A is the suite's shared member.
const VIEWER_B = `layers_b_${suffix}`
const PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`

// The shared "screen" is an animated 1280x720 canvas with moving detail, so
// every layer has something to encode. The sharer's peer connections are
// kept in window.__pcs to look at what it sends.
const fakeScreen = () => {
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
      for (let i = 0; i < 12; i++) ctx.fillText(`layers ${n} ${i}`, 40 + ((n * 7 + i * 90) % 800), 60 + i * 55)
      n++
    }, 33)
    return canvas.captureStream(30)
  }
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) { super(...args); window.__pcs.push(this) }
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

interface SentStream { width: number; height: number; bytes: number }

// What the sharer's video senders send now: the size and bytes of every
// outbound video stream, twice, so growth tells the streams that are live.
async function sending(page: Page): Promise<SentStream[]> {
  return page.evaluate(async () => {
    const sample = async () => {
      const out = new Map<string, { width: number; height: number; bytes: number }>()
      for (const pc of window.__pcs) {
        for (const stat of (await pc.getStats()).values()) {
          if (stat.type !== 'outbound-rtp' || stat.kind !== 'video') continue
          out.set(stat.id, { width: stat.frameWidth ?? 0, height: stat.frameHeight ?? 0, bytes: stat.bytesSent ?? 0 })
        }
      }
      return out
    }
    const before = await sample()
    await new Promise(resolve => setTimeout(resolve, 2000))
    const after = await sample()
    return [...after].filter(([id, now]) => now.bytes > (before.get(id)?.bytes ?? 0)).map(([, now]) => now)
  })
}

// Frame presentation times of the stage's video, to find stalls.
async function watchFrames(page: Page) {
  await page.getByTestId('stage').locator('video').evaluate((v) => {
    if (!(v instanceof HTMLVideoElement)) throw new Error('Expected video')
    const state = { last: performance.now(), maxGap: 0, frames: 0 }
    ;(window as unknown as { __frames: typeof state }).__frames = state
    const tick = (now: number) => {
      state.maxGap = Math.max(state.maxGap, now - state.last)
      state.last = now
      state.frames++
      v.requestVideoFrameCallback(tick)
    }
    v.requestVideoFrameCallback(tick)
  })
}

async function frames(page: Page) {
  return page.evaluate(() => (window as unknown as { __frames: { maxGap: number, frames: number } }).__frames)
}

const videoSize = (page: Page) => page.getByTestId('stage').locator('video').evaluate(
  v => (v instanceof HTMLVideoElement ? `${v.videoWidth}x${v.videoHeight}` : ''))

// Engines round a scaled-down width differently (852 or 853 of 1280 / 1.5).
function sameSize(actual: string, expected: string): boolean {
  const [w, h] = actual.split('x').map(Number)
  const [ew, eh] = expected.split('x').map(Number)
  return w !== undefined && h !== undefined && ew !== undefined && eh !== undefined && Math.abs(w - ew) <= 1 && Math.abs(h - eh) <= 1
}

async function chooseLayer(page: Page, id: 'auto' | 0 | 1 | 2) {
  await page.getByTestId('screen-layer-button').click()
  await page.locator(`[data-menu-item="layer-${id}"]`).click()
  await page.keyboard.press('Escape')
}

// Whatever the test did, the channel it made and the browsers it opened go:
// a failed run must not leave a live 720p share encoding (it starves the specs
// that follow) or an extra channel in the sidebar (the drag specs need the
// whole list on screen).
const cleanups: Array<() => Promise<unknown>> = []
test.afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup().catch(() => {})
})

test('viewers pick their own layer of a shared screen and switch mid-stream', async ({ browser }) => {
  test.setTimeout(240_000)
  const sharerCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  cleanups.push(() => sharerCtx.close(), () => aCtx.close(), () => bCtx.close())
  await sharerCtx.addInitScript(fakeScreen)
  const sharer = await sharerCtx.newPage()
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  for (const [name, page] of [['sharer', sharer], ['a', a], ['b', b]] as const) {
    page.on('console', (m) => { if (['warning', 'error'].includes(m.type())) console.log(`[${name}] ${m.text()}`) })
    page.on('pageerror', (e) => console.log(`[${name}] pageerror ${e.message}`))
  }

  await signIn(sharer, ADMIN_USER, ADMIN_PASSWORD)
  const created = await apiFetch(sharer, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
  expect(created.status).toBe(201)
  cleanups.push(() => apiFetch(sharer, 'DELETE', `/api/admin/channels/${responseString(created, 'id')}`))
  await a.goto('/')
  if (suiteMember.registered) {
    expect((await apiFetch(a, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  } else {
    const invite = await apiFetch(sharer, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(a, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  await a.reload()
  const invite = await apiFetch(sharer, 'POST', '/api/admin/invites', { max_uses: 1 })
  await b.goto('/')
  expect((await apiFetch(b, 'POST', '/api/auth/register', { username: VIEWER_B, password: PASSWORD, invite_code: responseString(invite, 'code') })).status).toBe(201)
  await b.reload()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  for (const page of [sharer, a, b]) {
    await sidebarVoice(page).click()
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
  }

  await sharer.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  for (const page of [a, b]) {
    await page.getByTestId('screen-card').getByRole('button', { name: 'Ansehen' }).click()
    await expect(page.getByTestId('stage')).toBeVisible({ timeout: 20_000 })
    // The sharer declared its layers: the picker appears (Auto by default).
    await expect(page.getByTestId('screen-layer-button')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByTestId('screen-layer-label')).toHaveText('Automatisch')
  }

  // Automatic starts on the sharer's own quality: the whole 1280x720 picture.
  await expect.poll(() => videoSize(a), { timeout: 30_000 }).toBe('1280x720')
  await expect.poll(() => videoSize(b), { timeout: 30_000 }).toBe('1280x720')

  // The media is sealed in the sharer's browser, on every layer's line.
  const sealed = await sharer.evaluate(() => window.__pcs.flatMap(pc => pc.getSenders()).filter(s => s.track?.kind === 'video').map(s => !!s.transform))
  expect(sealed.length).toBeGreaterThanOrEqual(3)
  expect(sealed.every(Boolean)).toBe(true)

  // B pins the best layer. Auto may step down on a starved machine (that is
  // what it is for), which would make B's size depend on the load rather than
  // on A; a chosen layer is never adapted.
  await chooseLayer(b, 0)
  await expect(b.getByTestId('screen-layer-label')).toHaveText('Quelle')
  await expect.poll(() => videoSize(b), { timeout: 30_000 }).toBe('1280x720')

  // A moves to the smallest layer; B stays. Both keep decoding, and the
  // sharer only sends the two layers somebody receives.
  await chooseLayer(a, 2)
  await expect.poll(() => videoSize(a), { timeout: 30_000 }).toBe('640x360')
  expect(await videoSize(b)).toBe('1280x720')
  await expect(a.getByTestId('screen-layer-label')).toHaveText('360p')
  await expect.poll(async () => (await sending(sharer)).map(s => s.width).sort((x, y) => y - x), { timeout: 30_000 }).toEqual([1280, 640])

  // Switching while the stream runs never stalls it for long.
  await watchFrames(a)
  // Auto after a pick carries on from the picked layer (the server only moves
  // it up after a quiet stretch, see adaptCleanFor), so it still shows 360p.
  const sizes: Array<['auto' | 0 | 1 | 2, string]> = [[1, '852x480'], [0, '1280x720'], [2, '640x360'], ['auto', '640x360'], [1, '852x480']]
  for (const [layer, size] of sizes) {
    await chooseLayer(a, layer)
    await expect.poll(async () => sameSize(await videoSize(a), size), { timeout: 30_000 }).toBe(true)
    await a.waitForTimeout(1500)
    if (layer === 'auto') await expect(a.getByTestId('screen-layer-label')).toHaveText('Automatisch')
  }
  const seen = await frames(a)
  console.log(`[layers] ${seen.frames} frames while switching, longest gap ${Math.round(seen.maxGap)} ms`)
  expect(seen.frames).toBeGreaterThan(60)
  expect(seen.maxGap).toBeLessThan(1500)
  // B was not disturbed by A's choices.
  expect(await videoSize(b)).toBe('1280x720')

  // Leave the sidebar as it was: other specs drag channels around by position.
  for (const page of [a, b, sharer]) await page.getByRole('button', { name: 'Verlassen' }).first().click()
  expect((await apiFetch(sharer, 'DELETE', `/api/admin/channels/${responseString(created, 'id')}`)).status).toBe(204)
})
