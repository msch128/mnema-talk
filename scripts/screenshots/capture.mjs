/* global process */
// Captures the README screenshots from a seeded throwaway instance.
// Usage: see scripts/screenshots/run.sh. Output: SHOTS_OUT (default doc/images).
// The app has one (dark) theme, so every picture is 1440x900 at 1x.
import { createRequire } from 'node:module'
// Playwright comes from e2e/ (one install, one pinned version).
const { chromium } = createRequire(new URL('../../e2e/package.json', import.meta.url))('@playwright/test')
import { mkdirSync } from 'node:fs'
import { BASE, ADMIN_USER, ADMIN_PASSWORD, MEMBER_PASSWORD, login } from './seed.mjs'

const OUT = process.env.SHOTS_OUT || new URL('../../doc/images/', import.meta.url).pathname
mkdirSync(OUT, { recursive: true })
const only = process.argv.slice(2)
const wanted = name => only.length === 0 || only.includes(name)
const size = { width: 1440, height: 900 }

// A generic "screen" to share: an editor window with a moving cursor and chart.
// No real content; every layer of the share has something to encode.
function fakeScreen() {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const W = 1920, H = 1080
    const canvas = document.createElement('canvas')
    canvas.width = W; canvas.height = H
    const ctx = canvas.getContext('2d')
    let n = 0
    const lines = [
      ['#c586c0', 'pub fn', ' flush_lights(pass: &mut Pass, lights: &[Light]) {'],
      ['#6a9955', '    // One uniform block per batch, not per light.', ''],
      ['#c586c0', '    for', ' batch in lights.chunks(MAX_LIGHTS_PER_PASS) {'],
      ['#9cdcfe', '        pass', '.set_uniforms(&LightBlock::from(batch));'],
      ['#9cdcfe', '        pass', '.draw_indexed(0..quad.index_count, 0, 0..1);'],
      ['#d4d4d4', '    }', ''],
      ['#d4d4d4', '}', ''],
    ]
    const draw = () => {
      ctx.fillStyle = '#1e1e1e'; ctx.fillRect(0, 0, W, H)
      ctx.fillStyle = '#252526'; ctx.fillRect(0, 0, 360, H)
      ctx.fillStyle = '#333'; ctx.fillRect(0, 0, W, 56)
      ctx.fillStyle = '#d4d4d4'; ctx.font = '26px monospace'
      ctx.fillText('render/lights.rs', 380, 38)
      ;['emberfall/', '  assets/', '  render/', '    lights.rs', '    pass.rs', '    batch.rs', '  game/', 'tools/'].forEach((t, i) => {
        ctx.fillStyle = i === 3 ? '#fff' : '#9aa0a0'; ctx.fillText(t, 28, 110 + i * 44)
      })
      ctx.font = '34px monospace'
      lines.forEach(([color, a, b], i) => {
        ctx.fillStyle = '#6b6b6b'; ctx.fillText(String(i + 38), 390, 130 + i * 52)
        ctx.fillStyle = color; ctx.fillText(a, 460, 130 + i * 52)
        ctx.fillStyle = '#d4d4d4'; ctx.fillText(b, 460 + ctx.measureText(a).width, 130 + i * 52)
      })
      if (Math.floor(n / 15) % 2 === 0) { ctx.fillStyle = '#fff'; ctx.fillRect(460 + 20 * 20, 130 + 8 * 52 - 30, 3, 40) }
      // build pipeline panel with a moving graph
      ctx.fillStyle = '#181818'; ctx.fillRect(380, 640, W - 420, 400)
      ctx.fillStyle = '#2da771'; ctx.font = '30px monospace'; ctx.fillText('test render::lights  ... ok  (41 passed)  3.2s', 410, 695)
      ctx.strokeStyle = '#2da771'; ctx.lineWidth = 4; ctx.beginPath()
      for (let x = 0; x < 1400; x += 8) {
        const y = 900 + Math.sin((x + n * 6) / 70) * 60 + Math.sin((x + n * 3) / 23) * 20
        x === 0 ? ctx.moveTo(410 + x, y) : ctx.lineTo(410 + x, y)
      }
      ctx.stroke()
      n++
    }
    draw(); setInterval(draw, 66)
    return canvas.captureStream(30)
  }
}

// Every picture is taken with the pointer parked on the title-bar corner (the
// browser's own edge, no app element), so no hover chrome (sidebar "+" and
// trash icons, message toolbars) is captured by accident.
const shot = async (page, name) => {
  await page.mouse.move(0, 0)
  await page.waitForTimeout(150)
  await page.screenshot({ path: `${OUT}/${name}.png` })
  console.log('captured', name)
}

const browser = await chromium.launch({ args: [
  '--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream',
  '--force-webrtc-ip-handling-policy=default_public_and_private_interfaces',
  '--mute-audio',
] })
const newPage = async (init) => {
  const ctx = await browser.newContext({ locale: 'en-US', viewport: size, permissions: ['microphone'], serviceWorkers: 'block' })
  await closeWelcomeFlow(ctx)
  if (init) await ctx.addInitScript(init)
  return ctx.newPage()
}
// The first sign-in of every demo account would open the welcome flow and the
// first-run tips over the picture; answer the state request as if both were done.
async function closeWelcomeFlow(ctx) {
  await ctx.route('**/api/onboarding', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    const response = await route.fetch()
    const state = await response.json().catch(() => null)
    if (!state || typeof state !== 'object') return route.fulfill({ response })
    return route.fulfill({ response, json: { ...state, welcome_done: true, dismissed_tips: ['search', 'mention', 'talk', 'notif-hint'] } })
  })
}
// Scrolls the message list so its top edge is exactly the top of a message row:
// the first row that is fully inside the list now. Whatever sat above it (a cut
// message, the welcome block) is out of view, so no glyph is clipped at the top
// edge. A list that cannot scroll that far gets bottom padding, which shows as
// empty space above the composer instead of a shifted picture.
async function alignMessageList(page) {
  await page.waitForSelector('[data-msg-id]')
  await page.evaluate(() => {
    const first = document.querySelector('[data-msg-id]')
    let list = first.parentElement
    while (list && !/(auto|scroll)/.test(getComputedStyle(list).overflowY)) list = list.parentElement
    const top = list.getBoundingClientRect().top
    const rows = [...list.querySelectorAll('[data-msg-id]')]
    const row = rows.find((r) => r.getBoundingClientRect().top >= top) || rows[0]
    const delta = row.getBoundingClientRect().top - top - 8
    const missing = list.scrollTop + delta - (list.scrollHeight - list.clientHeight)
    if (missing > 0) list.style.paddingBottom = `${parseFloat(getComputedStyle(list).paddingBottom) + missing}px`
    list.scrollTop += delta
  })
  await page.waitForTimeout(300)
}
const channel = (page, type, text) => page.locator(`[data-channel-type="${type}"]`, { hasText: text }).first()

// Members who are online while the pictures are taken (the member list shows them).
async function keepOnline(names) {
  const pages = []
  for (const name of names) {
    const page = await newPage()
    await login(page, name, MEMBER_PASSWORD)
    await page.goto(BASE + '/')
    await page.getByTestId('account-menu-button').waitFor()
    pages.push(page)
  }
  return pages
}
const online = await keepOnline(['theo', 'priya', 'ines', 'kai'])

// 1. Chat: #dev-log with Markdown, a code block, a thread, a mention and reactions
if (wanted('chat')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'dev-log').click()
  await alignMessageList(page)
  await page.mouse.move(700, 270)
  await page.waitForTimeout(1500)
  await shot(page, 'chat')
  await page.context().close()
}

// 1b. The thread under the save-bug report
if (wanted('thread')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'dev-log').click()
  await page.getByRole('button', { name: /Open thread/ }).first().click()
  await page.getByText('save.tmp').first().waitFor()
  await alignMessageList(page)
  await page.mouse.move(120, 700)
  await page.waitForTimeout(800)
  await shot(page, 'thread')
  await page.context().close()
}

// 1c. Unread messages and a mention: #general with the divider
if (wanted('unread')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await page.waitForTimeout(1200)
  await channel(page, 'text', 'general').click()
  await page.waitForTimeout(1800)
  await alignMessageList(page)
  await shot(page, 'unread')
  await page.context().close()
}

// 2. Roles editor
if (wanted('roles')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/admin/roles')
  await page.waitForTimeout(1500)
  await page.getByRole('list', { name: /roles/i }).getByText('Devs').first().click()
  await page.waitForTimeout(500)
  await shot(page, 'roles')
  await page.context().close()
}

// 3. Webhook settings (the secret URL is shown only once and never captured)
if (wanted('webhooks')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  const row = channel(page, 'text', 'dev-log')
  await row.click()
  await alignMessageList(page)
  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: /Webhooks/ }).click()
  await page.getByRole('dialog').waitFor()
  await page.waitForTimeout(500)
  await page.getByRole('dialog').getByRole('button', { name: /settings/i }).first().click()
  await page.waitForTimeout(800)
  await shot(page, 'webhooks')
  await page.context().close()
}

// 4a. Pinned messages: the rules and the release schedule
if (wanted('pins')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'announcements').click()
  await page.getByTestId('pinned-button').click()
  await page.getByTestId('pinned-item').first().waitFor()
  await page.waitForTimeout(600)
  await shot(page, 'pins')
  await page.context().close()
}

// 4b. A poll with votes, voted by the Owner as well
if (wanted('poll')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'playtests').click()
  await page.getByTestId('poll').waitFor()
  await page.getByTestId('poll').getByText('Saturday afternoon').click()
  await page.waitForTimeout(800)
  await page.mouse.move(120, 760)
  await shot(page, 'poll')
  await page.context().close()
}

// 4c. The emoji picker with the studio's own emoji
if (wanted('emoji-picker')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'general').click()
  await page.getByTestId('emoji-button').click()
  await page.getByTestId('emoji-picker').locator('emoji-picker').waitFor()
  await page.waitForTimeout(1200)
  // The studio's own emoji are the first group of the picker.
  await page.getByTestId('emoji-picker').locator('button.nav-button').first().click()
  await page.waitForTimeout(500)
  await shot(page, 'emoji-picker')
  await page.context().close()
}

// 4d. Turning on two-factor sign-in (the secret belongs to a demo account on a throwaway stack)
if (wanted('two-factor')) {
  const page = await newPage()
  await login(page, 'ines', MEMBER_PASSWORD)
  await page.goto(BASE + '/')
  await page.getByTestId('account-menu-button').click()
  await page.getByTestId('menu-settings').click()
  await page.getByTestId('two-factor-start').click()
  await page.getByTestId('two-factor-password').fill(MEMBER_PASSWORD)
  await page.getByTestId('two-factor-continue').click()
  await page.getByTestId('qr-code').waitFor()
  await page.waitForTimeout(600)
  await shot(page, 'two-factor')
  await page.context().close()
}

// 4e. The phone layout: #dev-log on a 390 px screen
if (wanted('phone')) {
  const ctx = await browser.newContext({ locale: 'en-US', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true, serviceWorkers: 'block' })
  await closeWelcomeFlow(ctx)
  const page = await ctx.newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await page.getByTestId('open-nav').tap()
  await channel(page, 'text', 'dev-log').tap()
  await page.waitForTimeout(1500)
  await shot(page, 'phone')
  await ctx.close()
}

// 5/6. Talk with a screen share (quality picker) and the safety code
if (wanted('talk') || wanted('safety-code') || wanted('operations')) {
  const sharer = await newPage(fakeScreen)
  const viewer = await newPage()
  const guest = await newPage()
  await login(sharer, 'theo', MEMBER_PASSWORD)
  await login(viewer, ADMIN_USER, ADMIN_PASSWORD)
  await login(guest, 'priya', MEMBER_PASSWORD)
  for (const p of [sharer, viewer, guest]) {
    await p.goto(BASE + '/')
    await channel(p, 'voice', 'Stand-up').click()
    await p.getByRole('button', { name: /^Leave/i }).first().waitFor({ timeout: 20000 })
  }
  await sharer.getByRole('button', { name: /Share screen/i }).first().click()
  await viewer.getByTestId('screen-card').getByRole('button', { name: /Watch/i }).click()
  await viewer.getByTestId('stage').waitFor({ timeout: 30000 })
  await viewer.getByTestId('screen-layer-button').waitFor({ timeout: 30000 })
  await viewer.waitForTimeout(6000)
  if (wanted('talk')) {
    await viewer.getByTestId('screen-layer-button').click()
    await viewer.waitForTimeout(600)
    await shot(viewer, 'talk-screenshare')
    await viewer.keyboard.press('Escape')
  }
  if (wanted('safety-code')) {
    await viewer.getByRole('button', { name: /connection|details|stats/i }).first().click()
    await viewer.getByText('Safety code').first().scrollIntoViewIfNeeded()
    await viewer.waitForTimeout(800)
    await shot(viewer, 'safety-code')
  }
  if (wanted('operations')) {
    // The operator view while the Talk above is live; give the charts a minute of samples.
    const ops = await newPage()
    await login(ops, ADMIN_USER, ADMIN_PASSWORD)
    await ops.goto(BASE + '/admin/system')
    await ops.waitForTimeout(75000)
    await ops.getByTestId('system-ops').scrollIntoViewIfNeeded()
    await ops.getByTestId('system-ops').evaluate(el => el.scrollIntoView({ block: 'start' }))
    await ops.evaluate(() => document.activeElement && document.activeElement.blur())
    await ops.waitForTimeout(800)
    await shot(ops, 'operations')
  }
  for (const p of [sharer, viewer, guest]) await p.context().close()
}

for (const p of online) await p.context().close()
await browser.close()
