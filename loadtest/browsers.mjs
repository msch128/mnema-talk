// One member shares a screen in a Talk, real browsers watch it. Every browser
// runs the instance's web app, so media is sealed and opened with the MLS keys
// exactly as in production.
//
// env: BASE_URL, TOKENS, CHANNEL_NAME, BROWSERS (watching browsers, besides
//      the sharer), PER_BROWSER (contexts per Chromium process), BATCH,
//      BATCH_MS, HOLD_S, WIDTH, FPS, OUT, READY_FILE (created when the sharer
//      shares, so the lightweight viewers can start), HOLD_FILE (hold begins),
//      STOP_FILE (hold is over)
import { writeFileSync } from 'node:fs'
import { loadavg } from 'node:os'
import {
  chromium, CHROMIUM_ARGS, LABEL, newMember, joinTalk, mute, watchScreen, stats,
  loadTokens, writeJSON, log, sleep, within, num, percentile,
} from './lib.mjs'

const tokens = loadTokens()
const BROWSERS = num('BROWSERS', 3)
const N = BROWSERS + 1
const PER_BROWSER = num('PER_BROWSER', 20)
const BATCH = num('BATCH', 10)
const BATCH_MS = num('BATCH_MS', 5000)
const HOLD_S = num('HOLD_S', 30)
const WIDTH = num('WIDTH', 640)
const FPS = num('FPS', 20)
const OUT = process.env.OUT || 'loadtest/results/browsers.json'
if (tokens.length < N) throw new Error(`need ${N} tokens, have ${tokens.length}`)

const browsers = []
const clients = []

async function newClient(index) {
  const b = Math.floor(index / PER_BROWSER)
  browsers[b] ??= await chromium.launch({ args: CHROMIUM_ARGS })
  const { ctx, page } = await newMember(browsers[b], tokens[index].token, { sharer: index === 0, width: WIDTH, fps: FPS })
  const client = { index, id: tokens[index].id, ctx, page, errors: 0, joinMs: null, firstFrameAt: null, watchAt: null, last: null, watching: false }
  page.on('pageerror', () => { client.errors++ })
  clients.push(client)
  return client
}

async function join(client) {
  const start = Date.now()
  await joinTalk(client.page)
  client.joinMs = Date.now() - start
  // Viewers listen only: their microphones stay off the SFU.
  if (client.index !== 0) await mute(client.page)
}

async function watch(client) {
  if (client.watching || client.index === 0) return
  const base = await watchScreen(client.page)
  if (base !== null) {
    client.watching = true
    client.watchAt = Date.now()
    // Time to the first decoded frame, measured at 100 ms resolution.
    client.page
      .waitForFunction(async (from) => {
        let n = 0
        for (const pc of window.__pcs || []) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'video') n += s.framesDecoded || 0 })
        return n > from
      }, base, { polling: 100, timeout: 120_000 })
      .then(() => { client.firstFrameAt ??= Date.now() })
      .catch(() => {})
  }
}

async function poll() {
  const rows = await Promise.all(clients.map(async (c) => ({ c, s: await stats(c.page) })))
  let connected = 0, decoding = 0, watching = 0, totalFps = 0
  for (const { c, s } of rows) {
    if (!s) continue
    if (s.state === 'connected') connected++
    if (c.watching) watching++
    const before = c.last
    c.last = s
    if (c.index === 0) continue
    if (before && s.framesDecoded > before.framesDecoded) {
      decoding++
      totalFps += s.fps
    }
  }
  const sharer = rows.find((r) => r.c.index === 0)?.s
  return {
    hostLoad: +loadavg()[0].toFixed(1), connected, watching, decoding, viewers: clients.length - 1,
    avgFps: decoding ? +(totalFps / decoding).toFixed(1) : 0,
    sharerFramesEncoded: sharer?.framesEncoded ?? 0, sharerBytesOut: sharer?.videoBytesOut ?? 0,
  }
}

const timeline = []
const t0 = Date.now()
async function main() {
  log(`browsers=${BROWSERS} per_browser=${PER_BROWSER} batch=${BATCH}/${BATCH_MS}ms hold=${HOLD_S}s`)
  const sharer = await newClient(0)
  await join(sharer)
  await sharer.page.getByRole('button', { name: LABEL.share }).first().click()
  log('sharer joined and sharing')
  if (process.env.READY_FILE) writeFileSync(process.env.READY_FILE, String(sharer.id))
  for (let i = 1; i < N; i += BATCH) {
    const batch = []
    for (let j = i; j < Math.min(i + BATCH, N); j++) {
      batch.push(newClient(j).then((c) => within(join(c), 90_000).then((r) => { if (r === null) c.joinError = 'timeout' }).catch((e) => { c.joinError = String(e).slice(0, 160) })))
    }
    await Promise.all(batch)
    await Promise.all(clients.map(watch))
    const p = await poll()
    timeline.push({ t: Date.now() - t0, ...p })
    log(`joined ${clients.length}/${N}`, JSON.stringify(p))
    await sleep(BATCH_MS)
  }
  if (process.env.HOLD_FILE) writeFileSync(process.env.HOLD_FILE, String(Date.now()))
  const holdUntil = Date.now() + HOLD_S * 1000
  while (Date.now() < holdUntil) {
    await Promise.all(clients.map(watch))
    const p = await poll()
    timeline.push({ t: Date.now() - t0, ...p })
    log('hold', JSON.stringify(p))
    await sleep(10_000)
  }
  // Tells the lightweight viewers the hold is over, before the sharer leaves.
  if (process.env.STOP_FILE) writeFileSync(process.env.STOP_FILE, String(Date.now()))
  const viewers = clients.filter((c) => c.index !== 0)
  const ttf = viewers.filter((c) => c.firstFrameAt && c.watchAt).map((c) => c.firstFrameAt - c.watchAt).sort((a, b) => a - b)
  const joins = viewers.filter((c) => c.joinMs).map((c) => c.joinMs).sort((a, b) => a - b)
  const summary = {
    browsers: BROWSERS,
    joinErrors: viewers.filter((c) => c.joinError).map((c) => ({ i: c.index, e: c.joinError })),
    pageErrors: clients.reduce((s, c) => s + c.errors, 0),
    neverDecoded: viewers.filter((c) => !c.firstFrameAt).map((c) => c.index),
    joinMs: { p50: percentile(joins, 0.5), p95: percentile(joins, 0.95), max: joins.at(-1) ?? null },
    firstFrameAfterWatchMs: { p50: percentile(ttf, 0.5), p95: percentile(ttf, 0.95), max: ttf.at(-1) ?? null },
    final: timeline.at(-1),
  }
  writeJSON(OUT, { summary, timeline })
  log('SUMMARY', JSON.stringify(summary))
}

try {
  await main()
} catch (e) {
  log('FAILED', e)
  writeJSON(OUT, { summary: { fatal: String(e) }, timeline })
  process.exitCode = 1
} finally {
  // Leave the Talk cleanly before closing.
  await Promise.all(clients.map((c) => c.page.getByRole('button', { name: LABEL.leave }).first().click({ timeout: 2000 }).catch(() => {})))
  await within(Promise.all(browsers.map((b) => b?.close().catch(() => {}))), 20_000)
  process.exit(process.exitCode ?? 0)
}
