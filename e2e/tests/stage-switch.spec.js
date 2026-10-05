import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `stage_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Stage ${suffix}`

async function apiFetch(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const res = await fetch(path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, json: await res.json().catch(() => null) }
  }, { method, path, body })
}

// A shared "screen" is an animated canvas. Each member's has its own size, so
// the stage's resolution tells whose share it shows.
const fakeDisplayMedia = ([width, height]) => {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 70% 50%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
    }, 50)
    return canvas.captureStream(20)
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

// Size of what the stage video decodes ('' while nothing plays).
const stageSize = (page) => page.evaluate(() => {
  const v = document.querySelector('[data-testid="stage"] video')
  return v && v.videoWidth && !v.paused ? `${v.videoWidth}x${v.videoHeight}` : ''
})

// Two members (A: the admin, B: a new member) in a new Talk. Their shared
// "screens" are 640x360 (A) and 800x450 (B).
async function twoMembers(browser, tag) {
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  await aCtx.addInitScript(fakeDisplayMedia, [640, 360])
  await bCtx.addInitScript(fakeDisplayMedia, [800, 450])
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
  return { a, b, bId, bName, close: async () => { await aCtx.close(); await bCtx.close() } }
}

// Regression: while sharing my own screen, a share I watched never reached the
// stage and had no card, so there was nothing to switch to.
test('two members share their screens and switch the stage both ways', async ({ browser }) => {
  const { a, b, bId, close } = await twoMembers(browser, 'screens')

  // A shares: their own share is on the stage.
  await a.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  const aStage = a.getByTestId('stage')
  await expect(aStage).toHaveAttribute('data-stage-source', 'own')
  await expect.poll(() => stageSize(a)).toBe('640x360')

  // B shares too: A is offered B's share as a card and watches it.
  await b.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  await expect(b.getByTestId('stage')).toHaveAttribute('data-stage-source', 'own')
  const bCard = a.locator(`[data-screen-card="${bId}"]`)
  await expect(bCard).toBeVisible()
  await bCard.getByRole('button', { name: 'Ansehen' }).click()

  // B's share takes the stage once its frames arrive; A's own is a card now.
  await expect(aStage).toHaveAttribute('data-stage-source', bId, { timeout: 20_000 })
  await expect.poll(() => stageSize(a), { timeout: 20_000 }).toBe('800x450')
  const ownCard = a.locator('[data-screen-card="own"]')
  await expect(ownCard).toContainText('Eigener Bildschirm')
  await expect(aStage.getByRole('button', { name: 'Stream beenden' })).toBeVisible()
  await shot(a, 'stage-remote')

  // Back to A's own share; B's stays received as a card.
  await ownCard.getByRole('button', { name: 'Auf die Bühne' }).click()
  await expect(aStage).toHaveAttribute('data-stage-source', 'own')
  await expect.poll(() => stageSize(a)).toBe('640x360')
  await expect(bCard.getByRole('button', { name: 'Auf die Bühne' })).toBeVisible()
  await shot(a, 'stage-own')

  // And to B's again.
  await bCard.getByRole('button', { name: 'Auf die Bühne' }).click()
  await expect(aStage).toHaveAttribute('data-stage-source', bId)
  await expect.poll(() => stageSize(a)).toBe('800x450')

  // B stops sharing: A's stage falls back to A's own share.
  await b.getByRole('button', { name: 'Teilen beenden' }).first().click()
  await expect(aStage).toHaveAttribute('data-stage-source', 'own', { timeout: 20_000 })
  await expect.poll(() => stageSize(a)).toBe('640x360')
  await expect(a.getByTestId('screen-card')).toHaveCount(0)

  await close()
})

test('a camera goes on the stage, switches with a screen share and back to the grid', async ({ browser }) => {
  const { a, b, bId, bName, close } = await twoMembers(browser, 'camera')
  const aStage = a.getByTestId('stage')
  const bTile = a.locator('[data-participant-tile]', { hasText: bName })
  // A corner of the tile: its middle holds the Watch button while B shares.
  const clickTile = () => bTile.click({ position: { x: 6, y: 6 } })

  // B turns on their (fake) camera; A receives it on B's tile.
  await b.getByRole('button', { name: 'Kamera einschalten' }).first().click()
  await expect(bTile.locator('video')).toBeVisible({ timeout: 20_000 })
  await expect(bTile).toHaveAttribute('aria-label', `Kamera von ${bName} vergrößern`)

  // A click puts it on the stage, a second click takes it off: the grid again.
  await clickTile()
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)
  await expect.poll(() => stageSize(a)).not.toBe('')
  await expect(aStage.getByTestId('viewer-stream-volume-slider')).toHaveCount(0)
  await expect(bTile).toHaveAttribute('aria-pressed', 'true')
  await shot(a, 'stage-camera')
  await clickTile()
  await expect(aStage).toHaveCount(0)

  // B shares: A watches it, then puts B's camera on the stage over it.
  await b.getByRole('button', { name: 'Bildschirm teilen' }).first().click()
  const bCard = a.locator(`[data-screen-card="${bId}"]`)
  await bCard.getByRole('button', { name: 'Ansehen' }).click()
  await expect(aStage).toHaveAttribute('data-stage-source', bId, { timeout: 20_000 })
  await expect.poll(() => stageSize(a), { timeout: 20_000 }).toBe('800x450')
  await clickTile()
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)
  await expect.poll(() => stageSize(a)).not.toBe('800x450')

  // The card switches back to the screen, the tile to the camera again.
  await bCard.getByRole('button', { name: 'Auf die Bühne' }).click()
  await expect(aStage).toHaveAttribute('data-stage-source', bId)
  await expect.poll(() => stageSize(a)).toBe('800x450')
  await clickTile()
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)

  // X takes the camera off the stage: B's screen share is back.
  await aStage.getByTestId('stage-unfocus-camera').click()
  await expect(aStage).toHaveAttribute('data-stage-source', bId)
  await expect.poll(() => stageSize(a)).toBe('800x450')

  // B's camera stops while on the stage: the stage falls back to the screen.
  await clickTile()
  await expect(aStage).toHaveAttribute('data-stage-source', `camera:${bId}`)
  await b.getByRole('button', { name: 'Kamera ausschalten' }).first().click()
  await expect(aStage).toHaveAttribute('data-stage-source', bId, { timeout: 20_000 })

  await close()
})
