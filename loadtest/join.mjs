// Video stalls while members join a Talk with a running screen share.
// A stall is more than 1 s without a newly decoded frame after the first one.
//
// env: BASE_URL, TOKENS, CHANNEL_NAME, MEMBERS (incl. sharer), MODE
//      (seq | parallel), GAP_MS between sequential joins, HOLD_S, DESC=1
//      (join in descending user-id order), OUT
import {
  chromium, CHROMIUM_ARGS, LABEL, newMember, joinTalk, mute, watchScreen,
  loadTokens, writeJSON, log, sleep, num, elapsed,
} from './lib.mjs'

const tokens = loadTokens()
const N = num('MEMBERS', 6)
const GAP_MS = num('GAP_MS', 8000)
const HOLD_S = num('HOLD_S', 30)
const MODE = process.env.MODE || 'seq'
const STALL_S = 1
const OUT = process.env.OUT || 'loadtest/results/join.json'
if (tokens.length < N) throw new Error(`need ${N} tokens, have ${tokens.length}`)

const browser = await chromium.launch({ args: CHROMIUM_ARGS })
const clients = []
const events = []
const now = () => +elapsed().toFixed(1)

async function open(i) {
  const { page } = await newMember(browser, tokens[i].token, { sharer: i === 0 })
  const c = { i, id: tokens[i].id.slice(0, 8), page, samples: [], watching: null }
  page.on('console', (m) => {
    const t = m.text()
    if (/media|mls|epoch|key|group|rekey/i.test(t)) events.push({ t: now(), who: i, msg: t.slice(0, 160) })
  })
  await joinTalk(page)
  events.push({ t: now(), who: i, msg: 'joined' })
  if (i === 0) await page.getByRole('button', { name: LABEL.share }).first().click()
  else await mute(page)
  clients.push(c)
}

async function sample() {
  for (const c of clients) {
    if (c.i === 0) continue
    if (c.watching === null && (await watchScreen(c.page)) !== null) c.watching = now()
    const f = await c.page
      .evaluate(async () => {
        let n = 0
        for (const pc of window.__pcs || []) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'video') n += s.framesDecoded || 0 })
        return n
      })
      .catch(() => -1)
    c.samples.push([now(), f])
  }
}

let stop = false
const loop = (async () => {
  while (!stop) {
    await sample()
    await sleep(500)
  }
})()

const failures = []
const tryOpen = (i) => open(i).catch((e) => { failures.push({ viewer: i, error: String(e).slice(0, 160) }) })
if (MODE === 'parallel') {
  await open(0)
  await sleep(3000)
  await Promise.all(Array.from({ length: N - 1 }, (_, k) => tryOpen(k + 1)))
} else {
  for (let i = 0; i < N; i++) {
    await (i === 0 ? open(i) : tryOpen(i))
    await sleep(i === 0 ? 3000 : GAP_MS)
  }
}
await sleep(HOLD_S * 1000)
stop = true
await loop

const report = clients.filter((c) => c.i > 0).map((c) => {
  const stalls = []
  let last = null, since = null, first = null
  for (const [t, f] of c.samples) {
    if (f > 0 && first === null) first = t
    if (last !== null && f === last) since ??= t - 0.5
    else {
      if (since !== null && t - since > STALL_S) stalls.push([since, t])
      since = null
    }
    last = f
  }
  if (since !== null && c.samples.at(-1)[0] - since > STALL_S) stalls.push([since, 'end'])
  const after = first === null ? [] : stalls.filter((s) => s[0] >= first)
  const longest = after.reduce((m, s) => Math.max(m, (s[1] === 'end' ? c.samples.at(-1)[0] : s[1]) - s[0]), 0)
  return { viewer: c.i, watching: c.watching, firstFrame: first, neverDecoded: first === null, stalls: after, longestStallS: +longest.toFixed(1) }
})
const summary = {
  mode: MODE,
  desc: process.env.DESC === '1',
  members: N,
  failures,
  neverDecoded: report.filter((r) => r.neverDecoded).map((r) => r.viewer),
  stalledViewers: report.filter((r) => r.stalls.length).length,
  longestStallS: Math.max(0, ...report.map((r) => r.longestStallS)),
}
writeJSON(OUT, { summary, report, events })
log('SUMMARY', JSON.stringify(summary))
await browser.close()
process.exit(0)
