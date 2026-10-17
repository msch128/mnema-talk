// Captures the README screenshots from a seeded throwaway instance.
// Usage: see e2e/screenshots/run.sh. Output: SHOTS_OUT (default doc/images).
// The app has one (dark) theme, so every picture is 1440x900 at 1x.
import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { BASE, ADMIN_USER, ADMIN_PASSWORD, MEMBER_PASSWORD, api, login } from './seed.mjs'

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
      ['#c586c0', 'func', ' handleJoin(ctx context.Context, room *Room) error {'],
      ['#9cdcfe', '    peer', ', err := room.NewPeer(ctx)'],
      ['#c586c0', '    if', ' err != nil {'],
      ['#c586c0', '        return', ' fmt.Errorf("join: %w", err)'],
      ['#d4d4d4', '    }', ''],
      ['#6a9955', '    // Forward RTP, never transcode.', ''],
      ['#9cdcfe', '    room', '.Forward(peer, layers)'],
      ['#c586c0', '    return', ' nil'],
      ['#d4d4d4', '}', ''],
    ]
    const draw = () => {
      ctx.fillStyle = '#1e1e1e'; ctx.fillRect(0, 0, W, H)
      ctx.fillStyle = '#252526'; ctx.fillRect(0, 0, 360, H)
      ctx.fillStyle = '#333'; ctx.fillRect(0, 0, W, 56)
      ctx.fillStyle = '#d4d4d4'; ctx.font = '26px monospace'
      ctx.fillText('sfu/room.go', 380, 38)
      ;['cmd/', 'internal/', '  sfu/', '    room.go', '    peer.go', '    layers.go', '  ws/', 'web/'].forEach((t, i) => {
        ctx.fillStyle = i === 3 ? '#fff' : '#9aa0a0'; ctx.fillText(t, 28, 110 + i * 44)
      })
      ctx.font = '34px monospace'
      lines.forEach(([color, a, b], i) => {
        ctx.fillStyle = '#6b6b6b'; ctx.fillText(String(i + 12), 390, 130 + i * 52)
        ctx.fillStyle = color; ctx.fillText(a, 460, 130 + i * 52)
        ctx.fillStyle = '#d4d4d4'; ctx.fillText(b, 460 + ctx.measureText(a).width, 130 + i * 52)
      })
      if (Math.floor(n / 15) % 2 === 0) { ctx.fillStyle = '#fff'; ctx.fillRect(460 + 20 * 20, 130 + 8 * 52 - 30, 3, 40) }
      // build pipeline panel with a moving graph
      ctx.fillStyle = '#181818'; ctx.fillRect(380, 640, W - 420, 400)
      ctx.fillStyle = '#2da771'; ctx.font = '30px monospace'; ctx.fillText('PASS  internal/sfu  (race)  4.2s', 410, 695)
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

const shot = async (page, name) => {
  await page.screenshot({ path: `${OUT}/${name}.png` })
  console.log('captured', name)
}

const browser = await chromium.launch({ args: [
  '--use-fake-ui-for-media-stream', '--use-fake-device-for-media-stream',
  '--force-webrtc-ip-handling-policy=default_public_and_private_interfaces',
] })
const newPage = async (init) => {
  const ctx = await browser.newContext({ locale: 'en-US', viewport: size, permissions: ['microphone'] })
  if (init) await ctx.addInitScript(init)
  return ctx.newPage()
}
const channel = (page, type, text) => page.locator(`[data-channel-type="${type}"]`, { hasText: text }).first()

// 1. Chat
if (wanted('chat')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  await channel(page, 'text', 'general').click()
  await page.mouse.move(700, 450)
  await page.waitForTimeout(1500)
  await shot(page, 'chat')
  await page.context().close()
}

// 2. Roles editor
if (wanted('roles')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/admin/roles')
  await page.waitForTimeout(1500)
  await page.getByRole('list', { name: /roles/i }).getByText('Moderator').first().click()
  await page.waitForTimeout(500)
  await shot(page, 'roles')
  await page.context().close()
}

// 3. Webhook settings (the secret URL is shown only once and never captured)
if (wanted('webhooks')) {
  const page = await newPage()
  await login(page, ADMIN_USER, ADMIN_PASSWORD)
  await page.goto(BASE + '/')
  const row = channel(page, 'text', 'releases')
  await row.click()
  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: /Webhooks/ }).click()
  await page.getByRole('dialog').waitFor()
  await page.waitForTimeout(500)
  await page.getByRole('dialog').getByRole('button', { name: /settings/i }).first().click()
  await page.waitForTimeout(800)
  await shot(page, 'webhooks')
  await page.context().close()
}

// 5/6. Talk with a screen share (quality picker) and the safety code
if (wanted('talk') || wanted('safety-code') || wanted('operations')) {
  const sharer = await newPage(fakeScreen)
  const viewer = await newPage()
  const guest = await newPage()
  await login(sharer, 'mira', MEMBER_PASSWORD)
  await login(viewer, ADMIN_USER, ADMIN_PASSWORD)
  await login(guest, 'nora', MEMBER_PASSWORD)
  for (const p of [sharer, viewer, guest]) {
    await p.goto(BASE + '/')
    await channel(p, 'voice', 'Lounge').click()
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

await browser.close()
