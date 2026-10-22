import type { Page, Browser } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString, responseUserId, suiteMember, trackPeerConnections, leaveTalks } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Stage ${suffix}`


// A shared "screen" is an animated canvas. Each member's has its own size, so
// the stage's resolution tells whose share it shows.
const fakeDisplayMedia = ([width, height]: [number, number]) => {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
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

// E2E_SHOT_DIR=<dir> saves screenshots of the steps for a look by eye.
async function shot(page: Page, name: string) {
  if (process.env.E2E_SHOT_DIR) await page.screenshot({ path: `${process.env.E2E_SHOT_DIR}/${name}.png` })
}

// Size of what the stage video decodes ('' while nothing plays).
const stageSize = (page: Page) => page.evaluate(() => {
  const v = document.querySelector<HTMLVideoElement>('[data-testid="stage"] video')
  return v && v.videoWidth && !v.paused ? `${v.videoWidth}x${v.videoHeight}` : ''
})

// Two members (A: the admin, B: a new member) in a new Talk. Their shared
// "screens" are 640x360 (A) and 800x450 (B).
async function twoMembers(browser: Browser, tag: string) {
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  await aCtx.addInitScript(trackPeerConnections)
  await aCtx.addInitScript(fakeDisplayMedia, [640, 360] as [number, number])
  await bCtx.addInitScript(fakeDisplayMedia, [800, 450] as [number, number])
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  for (const [name, page] of [['a', a], ['b', b]] as const) {
    page.on('console', (m) => { if (['warning', 'error'].includes(m.type())) console.log(`[${name}] ${m.text()}`) })
    page.on('pageerror', (e) => console.log(`[${name}] pageerror ${e.message}`))
  }

  const channel = `${VOICE_CHANNEL} ${tag}`
  const bName = suiteMember.username
  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(a)) await a.reload()
  expect((await apiFetch(a, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  await b.goto('/')
  // The suite's shared member: registrations are limited to ten an hour from
  // one address and the whole suite shares them (see helpers.ts).
  if (suiteMember.registered) {
    expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  } else {
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  await b.reload()
  const me = await apiFetch(b, 'GET', '/api/auth/me')
  const bId = responseUserId(me)
  expect(bId).toBeTruthy()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
  // The suite shares one API budget per account (see awaitApiBudget): a page
  // whose channel list was throttled never shows the new Talk, so reload it
  // until the Talk is listed.
  for (const page of [a, b]) {
    await expect(async () => {
      if (await sidebarVoice(page).count() === 0) {
        await awaitApiBudget(page)
        await page.reload()
      }
      await expect(sidebarVoice(page)).toBeVisible({ timeout: 5000 })
    }).toPass({ timeout: 70_000 })
  }
  await sidebarVoice(a).click()
  await sidebarVoice(b).click()
  for (const page of [a, b]) {
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
  }
  return { a, b, bId, bName, close: async () => { await leaveTalks([a, b]); await aCtx.close(); await bCtx.close() } }
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
  // The camera really decodes for A, and keeps decoding: the browser counts a
  // frame only once it opened and decoded it.
  const decoded = () => a.evaluate(async () => {
    let frames = 0
    for (const pc of window.__pcs) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'video') frames += s.framesDecoded || 0 })
    return frames
  })
  await expect.poll(decoded, { timeout: 20_000 }).toBeGreaterThan(0)
  const framesSoFar = await decoded()
  await expect.poll(decoded, { timeout: 10_000 }).toBeGreaterThan(framesSoFar + 10)
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
