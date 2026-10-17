// Members share a Talk and reload in turn; after each reload every member must
// hear the others again (inbound audio bytes grow).
//
// env: BASE_URL, TOKENS, CHANNEL_NAME, MEMBERS (default 2), SETTLE_MS, OUT
import {
  chromium, CHROMIUM_ARGS, newMember, joinTalk, loadTokens, writeJSON, log, sleep, num,
} from './lib.mjs'

const tokens = loadTokens()
const N = num('MEMBERS', 2)
const SETTLE_MS = num('SETTLE_MS', 12_000)
const OUT = process.env.OUT || 'loadtest/results/reload.json'
if (tokens.length < N) throw new Error(`need ${N} tokens, have ${tokens.length}`)

const browser = await chromium.launch({ args: CHROMIUM_ARGS })
const pages = []
for (let i = 0; i < N; i++) {
  const { page } = await newMember(browser, tokens[i].token)
  pages.push(page)
}
const audioIn = (page) =>
  page
    .evaluate(async () => {
      let n = 0
      for (const pc of window.__pcs || []) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') n += s.bytesReceived || 0 })
      return n
    })
    .catch(() => 0)

const checks = []
async function hear(label) {
  for (const [i, p] of pages.entries()) {
    const a = await audioIn(p)
    await sleep(3000)
    const b = await audioIn(p)
    checks.push({ label, member: i, audioBytesIn3s: b - a, ok: b - a > 0 })
    log(label, `#${i} audio bytes in 3s:`, b - a)
  }
}

await Promise.all(pages.map((p) => joinTalk(p)))
await sleep(5000)
await hear('start')
for (let i = 0; i < N; i++) {
  log(`--- reload #${i}`)
  await pages[i].reload()
  await sleep(SETTLE_MS)
  await hear(`after reload #${i}`)
}
const summary = { members: N, checks: checks.length, silent: checks.filter((c) => !c.ok).map((c) => `${c.label} #${c.member}`) }
writeJSON(OUT, { summary, checks })
log('SUMMARY', JSON.stringify(summary))
await browser.close()
process.exit(0)
