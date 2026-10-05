import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `extra_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Extras ${suffix}`
const TEXT_CHANNEL = `extras-text-${suffix}`

async function apiFetch(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const res = await fetch(path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, json: await res.json().catch(() => null) }
  }, { method, path, body })
}

// A shared "screen" is an animated canvas.
const fakeDisplayMedia = () => {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 640
    canvas.height = 360
    const ctx = canvas.getContext('2d')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 70% 50%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
    }, 50)
    return canvas.captureStream(20)
  }
}

// Headless Chromium may have no Picture-in-Picture: a stand-in that records
// the call and behaves like the API (element, enter/leave events).
const fakePictureInPicture = () => {
  let pipElement = null
  window.__pip = { requests: 0, exits: 0 }
  Object.defineProperty(Document.prototype, 'pictureInPictureEnabled', { configurable: true, get: () => true })
  Object.defineProperty(Document.prototype, 'pictureInPictureElement', { configurable: true, get: () => pipElement })
  HTMLVideoElement.prototype.requestPictureInPicture = function () {
    window.__pip.requests++
    if (this.readyState < 1) return Promise.reject(new DOMException('no metadata', 'InvalidStateError'))
    pipElement = this
    this.dispatchEvent(new Event('enterpictureinpicture'))
    return Promise.resolve({ width: 320, height: 180 })
  }
  Document.prototype.exitPictureInPicture = function () {
    window.__pip.exits++
    const el = pipElement
    pipElement = null
    el?.dispatchEvent(new Event('leavepictureinpicture'))
    return Promise.resolve()
  }
}

async function signIn(page, username, password) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// E2E_SHOT_DIR=<dir> saves screenshots of the steps for a look by eye.
async function shot(page, name) {
  if (process.env.E2E_SHOT_DIR) await page.screenshot({ path: `${process.env.E2E_SHOT_DIR}/${name}.png` })
}

// Two members (A: the admin, B: a new member) in a new Talk.
async function twoMembers(browser, tag, { pip = false } = {}) {
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  for (const ctx of [aCtx, bCtx]) await ctx.addInitScript(fakeDisplayMedia)
  if (pip) await aCtx.addInitScript(fakePictureInPicture)
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  for (const [name, page] of [['a', a], ['b', b]]) {
    page.on('console', (m) => { if (['warning', 'error'].includes(m.type())) console.log(`[${name}] ${m.text()}`) })
    page.on('pageerror', (e) => console.log(`[${name}] pageerror ${e.message}`))
  }

  const channel = `${VOICE_CHANNEL} ${tag}`
  const bName = `${USER}_${tag}`
  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  expect((await apiFetch(a, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
  await b.goto('/')
  expect((await apiFetch(b, 'POST', '/api/auth/register', { username: bName, password: USER_PASSWORD, invite_code: invite.json.code })).status).toBe(201)
  await b.reload()
  const me = await apiFetch(b, 'GET', '/api/auth/me')
  const bId = me.json?.user?.id || me.json?.id
  expect(bId).toBeTruthy()

  const sidebarVoice = (page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
  await sidebarVoice(a).click()
  await sidebarVoice(b).click()
  for (const page of [a, b]) {
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
  }
  return { a, b, bId, bName, channel, close: async () => { await aCtx.close(); await bCtx.close() } }
}

test('the streamer sees who watches their screen share', async ({ browser }) => {
  const { a, b, bName, close } = await twoMembers(browser, 'view')

  // A shares: their own share is on the stage, nobody watches yet.
  await a.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  const aStage = a.getByTestId('stage')
  await expect(aStage).toHaveAttribute('data-stage-source', 'own')
  const viewers = aStage.getByTestId('screen-viewers-button')
  await expect(viewers).toHaveText('0')
  await expect(viewers).toHaveAttribute('aria-label', 'Noch schaut niemand zu')

  // B watches: A sees one viewer, by name.
  await b.getByTestId('screen-card').getByRole('button', { name: 'Ansehen' }).click()
  await expect(b.getByTestId('stage')).toBeVisible({ timeout: 20_000 })
  await expect(viewers).toHaveText('1', { timeout: 15_000 })
  await expect(viewers).toHaveAttribute('aria-label', `1 Zuschauer: ${bName}`)
  // B sees itself among the viewers of A's share.
  await expect(b.getByTestId('stage').getByTestId('screen-viewers-button')).toHaveText('1')

  // Hovering lists the viewers.
  await viewers.hover()
  const list = aStage.getByTestId('screen-viewers-list')
  await expect(list).toContainText(bName)
  await shot(a, 'extras-viewers')

  // B stops watching: back to nobody.
  await b.getByTestId('stage').getByRole('button', { name: 'Stream beenden' }).click()
  await expect(viewers).toHaveText('0', { timeout: 15_000 })

  await close()
})

test('the grid fits the window and can hide participants without video', async ({ browser }) => {
  const { a, b, bName, close } = await twoMembers(browser, 'grid')
  const grid = a.getByTestId('talk-grid')
  const tiles = grid.locator('[data-participant-tile]')
  await expect(tiles).toHaveCount(2)
  const width = async () => (await tiles.first().boundingBox()).width

  // Two 16:9 tiles, as large as fit into the area; they shrink with the window.
  await expect(grid).toHaveAttribute('data-grid-cols', /^[12]$/)
  const big = await width()
  const area = await grid.boundingBox()
  for (const tile of await tiles.all()) {
    const box = await tile.boundingBox()
    expect(Math.abs(box.width / box.height - 16 / 9)).toBeLessThan(0.02)
    expect(box.x).toBeGreaterThanOrEqual(area.x - 1)
    expect(box.y).toBeGreaterThanOrEqual(area.y - 1)
    expect(box.x + box.width).toBeLessThanOrEqual(area.x + area.width + 1)
    expect(box.y + box.height).toBeLessThanOrEqual(area.y + area.height + 1)
  }
  await a.setViewportSize({ width: 1000, height: 640 })
  await expect.poll(width).toBeLessThan(big - 50)
  await shot(a, 'extras-grid-small')
  await a.setViewportSize({ width: 1400, height: 900 })
  await expect.poll(width).toBe(big)

  // Only videos: B turns on the camera, A hides everyone without video
  // (A itself too, like Discord).
  await b.getByRole('button', { name: 'Kamera einschalten' }).first().click()
  const bTile = tiles.filter({ hasText: bName })
  await expect(bTile.locator('video')).toBeVisible({ timeout: 20_000 })
  const toggle = a.getByTestId('hide-no-video')
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-pressed', 'true')
  await expect(tiles).toHaveCount(1)
  await expect(bTile).toBeVisible()
  // One video tile now gets the whole area.
  await expect.poll(width).toBeGreaterThan(big)
  await shot(a, 'extras-only-video')

  // B's camera goes off: nobody has video, a hint instead of a blank area.
  await b.getByRole('button', { name: 'Kamera ausschalten' }).first().click()
  await expect(a.getByTestId('no-video-hint')).toBeVisible({ timeout: 15_000 })
  // The choice is remembered across a reload.
  await a.reload()
  await expect(a.getByTestId('hide-no-video')).toHaveAttribute('aria-pressed', 'true', { timeout: 20_000 })
  await a.getByTestId('no-video-hint').getByRole('button', { name: 'Alle Teilnehmer zeigen' }).click()
  await expect(a.getByTestId('talk-grid').locator('[data-participant-tile]')).toHaveCount(2)

  await close()
})

test('double-click and F toggle full screen of the stage', async ({ browser }) => {
  const { a, b, bId, bName, close } = await twoMembers(browser, 'fs')
  const fullscreenSource = () => a.evaluate(() => document.fullscreenElement?.getAttribute('data-stage-source') ?? null)
  await b.getByRole('button', { name: 'Kamera einschalten' }).first().click()
  const bTile = a.locator('[data-participant-tile]', { hasText: bName })
  await expect(bTile.locator('video')).toBeVisible({ timeout: 20_000 })

  // A double-click on B's camera: on the stage, in full screen.
  await bTile.dblclick({ position: { x: 20, y: 20 } })
  const aStage = a.getByTestId('stage')
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)
  await expect.poll(fullscreenSource).toBe(`camera:${bId}`)
  await shot(a, 'extras-fullscreen')

  // F leaves full screen, F again enters it.
  await a.keyboard.press('f')
  await expect.poll(fullscreenSource).toBe(null)
  await a.keyboard.press('f')
  await expect.poll(fullscreenSource).toBe(`camera:${bId}`)

  // A double-click on the stage toggles it.
  await aStage.locator('video').dblclick()
  await expect.poll(fullscreenSource).toBe(null)
  await aStage.locator('video').dblclick()
  await expect.poll(fullscreenSource).toBe(`camera:${bId}`)
  await a.keyboard.press('f')
  await expect.poll(fullscreenSource).toBe(null)

  // Not while typing in the Talk chat.
  await a.getByRole('button', { name: 'Chat einblenden' }).click()
  const composer = a.getByRole('textbox', { name: /^Nachricht an/ })
  await composer.click()
  await a.keyboard.type('ff')
  await expect(composer).toHaveValue('ff')
  expect(await fullscreenSource()).toBe(null)

  await close()
})

test('picture-in-picture keeps playing after leaving the Talk view', async ({ browser }) => {
  const { a, b, bId, bName, close } = await twoMembers(browser, 'pip', { pip: true })
  expect((await apiFetch(a, 'POST', '/api/admin/channels', { name: TEXT_CHANNEL, type: 'text' })).status).toBe(201)

  const pipVideo = a.getByTestId('pip-video')
  // The video PiP plays: its stream and whether it shows frames.
  const pipState = () => a.evaluate(() => {
    const v = document.querySelector('[data-testid="pip-video"]')
    const track = v?.srcObject?.getVideoTracks?.()[0]
    return {
      inPip: document.pictureInPictureElement === v,
      live: track?.readyState === 'live',
      playing: !!v && v.videoWidth > 0 && !v.paused,
      requests: window.__pip.requests,
      exits: window.__pip.exits,
    }
  })

  // B's camera on A's stage, then into PiP from the stage's button.
  await b.getByRole('button', { name: 'Kamera einschalten' }).first().click()
  const bTile = a.locator('[data-participant-tile]', { hasText: bName })
  await expect(bTile.locator('video')).toBeVisible({ timeout: 20_000 })
  await bTile.click({ position: { x: 20, y: 20 } })
  const aStage = a.getByTestId('stage')
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)
  await aStage.getByRole('button', { name: 'Bild-im-Bild' }).click()
  await expect.poll(pipState).toMatchObject({ inPip: true, live: true, playing: true, requests: 1 })
  // The stage says where the video is now.
  await expect(aStage.getByTestId('stage-in-pip')).toBeVisible()
  await shot(a, 'extras-pip-stage')
  await expect(pipVideo).toHaveCount(1)

  // To a text channel: the Talk view goes, PiP keeps playing.
  await a.locator('[data-channel-type="text"]', { hasText: TEXT_CHANNEL }).click()
  await expect(aStage).toHaveCount(0)
  await a.waitForTimeout(1000)
  await expect.poll(pipState).toMatchObject({ inPip: true, live: true, playing: true })
  await shot(a, 'extras-pip-text-channel')

  // The stage empties (B's camera goes off): PiP closes.
  await b.getByRole('button', { name: 'Kamera ausschalten' }).first().click()
  await expect.poll(pipState, { timeout: 20_000 }).toMatchObject({ inPip: false, exits: 1 })

  await close()
})
