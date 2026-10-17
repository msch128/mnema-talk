// Shared helpers of the browser scenarios. Playwright comes from e2e/ (one
// install, one pinned version); nothing is added to the repository's
// dependencies.
import { createRequire } from 'node:module'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const require = createRequire(resolve(here, '../e2e/package.json'))
export const { chromium } = require('playwright')

export const BASE_URL = process.env.BASE_URL
if (!BASE_URL) throw new Error('BASE_URL is required')
export const CHANNEL_NAME = process.env.CHANNEL_NAME || 'Loadtest'
export const host = new URL(BASE_URL).hostname
export const secure = BASE_URL.startsWith('https:')
export const num = (k, d) => (process.env[k] === undefined || process.env[k] === '' ? d : Number(process.env[k]))

export function loadTokens() {
  const tokens = JSON.parse(readFileSync(process.env.TOKENS, 'utf8'))
  // DESC=1: members join in descending user-id order (worst case for the
  // group's member ordering).
  if (process.env.DESC === '1') tokens.sort((a, b) => (a.id < b.id ? 1 : -1))
  return tokens
}

export function writeJSON(path, data) {
  mkdirSync(dirname(resolve(path)), { recursive: true })
  writeFileSync(path, JSON.stringify(data, null, 1))
}

const t0 = Date.now()
export const elapsed = () => (Date.now() - t0) / 1000
export const log = (...a) => console.log(`[${elapsed().toFixed(1).padStart(6)}s]`, ...a)
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
export const within = (promise, ms) => Promise.race([promise, sleep(ms).then(() => null)])

export const CHROMIUM_ARGS = [
  '--use-fake-ui-for-media-stream',
  '--use-fake-device-for-media-stream',
  '--autoplay-policy=no-user-gesture-required',
]

// The web app is driven in German: the UI language follows the browser
// locale, and these are the German labels.
export const LABEL = { share: 'Bildschirm teilen', watch: 'Ansehen', leave: 'Verlassen' }

// Replaces getDisplayMedia with an animated canvas, so no screen is needed.
export const fakeDisplayMedia = ({ width, fps }) => {
  navigator.mediaDevices.getDisplayMedia = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = Math.round((width * 9) / 16)
    const ctx = canvas.getContext('2d')
    let n = 0
    setInterval(() => {
      ctx.fillStyle = `hsl(${(n++ * 7) % 360} 70% 50%)`
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      ctx.fillStyle = '#fff'
      ctx.font = '48px sans-serif'
      ctx.fillText(String(n), 40, 80)
    }, 50)
    return canvas.captureStream(fps)
  }
}

// Exposes every RTCPeerConnection of the page so scenarios can read getStats.
export const trackPeerConnections = () => {
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = class extends Native {
    constructor(...args) {
      super(...args)
      window.__pcs.push(this)
    }
  }
}

export async function newMember(browser, token, { sharer = false, width = 640, fps = 20 } = {}) {
  const ctx = await browser.newContext({
    permissions: ['microphone', 'camera'],
    locale: 'de-DE',
    viewport: { width: 1280, height: 800 },
    ignoreHTTPSErrors: true,
  })
  await ctx.addCookies([
    { name: secure ? '__Host-mnema_session' : 'mnema_session', value: token, domain: host, path: '/', secure, httpOnly: true, sameSite: 'Lax' },
  ])
  await ctx.addInitScript(trackPeerConnections)
  if (sharer) await ctx.addInitScript(fakeDisplayMedia, { width, fps })
  return { ctx, page: await ctx.newPage() }
}

// Opens the app and joins the Talk; resolves when the media connection is up.
export async function joinTalk(page, timeout = 60_000) {
  await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
  const voice = page.locator('[data-channel-type="voice"]', { hasText: CHANNEL_NAME })
  await voice.waitFor({ timeout })
  await voice.click()
  await page.waitForFunction(() => window.__pcs?.at(-1)?.connectionState === 'connected', null, { timeout })
}

export const mute = (page) => page.getByTestId('toggle-mute').click({ timeout: 5000 }).catch(() => {})

export const framesDecoded = (page) =>
  page.evaluate(async () => {
    let n = 0
    for (const pc of window.__pcs || []) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'video') n += s.framesDecoded || 0 })
    return n
  })

// Clicks "watch" on the screen card, once it is visible. Returns the number of
// frames decoded before the click, or null if the card was not there yet.
export async function watchScreen(page) {
  const card = page.getByTestId('screen-card')
  if (!(await within(card.isVisible().catch(() => false), 3000))) return null
  const base = await framesDecoded(page).catch(() => 0)
  await card.getByRole('button', { name: LABEL.watch }).click({ timeout: 5000 }).catch(() => {})
  return base
}

// Summed inbound/outbound counters over all peer connections of a page.
export function stats(page) {
  return within(
    page
      .evaluate(async () => {
        const out = { state: 'none', framesDecoded: 0, videoBytesIn: 0, audioBytesIn: 0, framesEncoded: 0, videoBytesOut: 0, width: 0, fps: 0 }
        for (const pc of window.__pcs || []) {
          out.state = pc.connectionState
          const report = await pc.getStats()
          report.forEach((s) => {
            if (s.type === 'inbound-rtp' && s.kind === 'video') {
              out.framesDecoded += s.framesDecoded || 0
              out.videoBytesIn += s.bytesReceived || 0
              out.width = Math.max(out.width, s.frameWidth || 0)
              out.fps = Math.max(out.fps, s.framesPerSecond || 0)
            }
            if (s.type === 'inbound-rtp' && s.kind === 'audio') out.audioBytesIn += s.bytesReceived || 0
            if (s.type === 'outbound-rtp' && s.kind === 'video') {
              out.framesEncoded += s.framesEncoded || 0
              out.videoBytesOut += s.bytesSent || 0
            }
          })
        }
        return out
      })
      .catch(() => null),
    5000,
  )
}

export const percentile = (sorted, q) => (sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(q * sorted.length))] : null)
