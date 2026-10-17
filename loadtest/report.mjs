// Reads the result files of one load-test run, prints a summary and exits 1
// when a threshold is violated.
//
// env: RESULT_DIR (default loadtest/results)
//      MAX_STALL_S          longest video stall after the first frame (default 3)
//      MAX_FIRST_FRAME_MS   browsers: p95 time from "watch" to first frame (default 15000)
//      MAX_HEAP_GROWTH_PCT  server heap, last vs first third of the hold phase (default 50)
//      MAX_GOROUTINE_LEAK   goroutines after everyone left vs before the run (default 30)
//      MAX_NEVER_CONNECTED  lightweight viewers that never reached ICE connected (default 0)
//      MAX_VIEWER_STALL_MS  lightweight viewers: longest gap without video bytes (default 30000)
import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const dir = process.env.RESULT_DIR || 'loadtest/results'
const T = {
  stallS: Number(process.env.MAX_STALL_S ?? 3),
  firstFrameMs: Number(process.env.MAX_FIRST_FRAME_MS ?? 15000),
  heapPct: Number(process.env.MAX_HEAP_GROWTH_PCT ?? 50),
  goroutineLeak: Number(process.env.MAX_GOROUTINE_LEAK ?? 30),
  neverConnected: Number(process.env.MAX_NEVER_CONNECTED ?? 0),
  viewerStallMs: Number(process.env.MAX_VIEWER_STALL_MS ?? 30000),
}
const read = (f) => (existsSync(join(dir, f)) ? JSON.parse(readFileSync(join(dir, f), 'utf8')) : null)
const violations = []
const lines = []
const say = (s) => lines.push(s)
const fail = (s) => violations.push(s)

const browsers = read('browsers.json')
if (browsers) {
  const s = browsers.summary
  if (s.fatal) fail(`browsers: scenario failed: ${s.fatal}`)
  else {
    say(`browsers   ${s.browsers} watching: join p50/p95 ${s.joinMs.p50}/${s.joinMs.p95} ms, first frame after watch p50/p95 ${s.firstFrameAfterWatchMs.p50}/${s.firstFrameAfterWatchMs.p95} ms, final ${JSON.stringify(s.final)}`)
    if (s.joinErrors.length) fail(`browsers: ${s.joinErrors.length} join error(s): ${JSON.stringify(s.joinErrors.slice(0, 3))}`)
    if (s.neverDecoded.length) fail(`browsers: ${s.neverDecoded.length} never decoded a frame (clients ${s.neverDecoded.join(',')})`)
    if (s.firstFrameAfterWatchMs.p95 > T.firstFrameMs) fail(`browsers: first frame p95 ${s.firstFrameAfterWatchMs.p95} ms > ${T.firstFrameMs} ms`)
  }
}

const viewers = read('viewers.json')
if (viewers) {
  say(`viewers    ${viewers.members} lightweight: never connected ${viewers.never_connected}, no video ${viewers.never_received}, flaps ${viewers.flaps}, candidate errors ${viewers.candidate_errors}, connect p50/p95 ${viewers.connect_ms_p50}/${viewers.connect_ms_p95} ms, first video p50/p95 ${viewers.first_video_ms_p50}/${viewers.first_video_ms_p95} ms, max gap ${viewers.max_stall_ms} ms, ${Math.round(viewers.avg_video_kbps)} kbit/s each${viewers.loss ? `, loss ${viewers.loss * 100}%` : ''}${viewers.jitter_ms ? `, jitter ${viewers.jitter_ms} ms` : ''}`)
  if (viewers.never_connected > T.neverConnected) fail(`viewers: ${viewers.never_connected} of ${viewers.members} never reached ICE connected (allowed ${T.neverConnected})`)
  if (viewers.never_received > T.neverConnected) fail(`viewers: ${viewers.never_received} of ${viewers.members} never received video`)
  if (viewers.failed) fail(`viewers: ${viewers.failed} signalling failure(s)`)
  if (viewers.max_stall_ms > T.viewerStallMs) fail(`viewers: longest gap without video ${viewers.max_stall_ms} ms > ${T.viewerStallMs} ms`)
}

for (const f of readdirSync(dir).filter((f) => /^join-.*\.json$/.test(f)).sort()) {
  const j = read(f)
  say(`${f.replace('.json', '').padEnd(10)} ${j.summary.members} members: stalled viewers ${j.summary.stalledViewers}, longest stall ${j.summary.longestStallS} s, never decoded ${j.summary.neverDecoded.length}`)
  if (j.summary.failures.length) fail(`${f}: ${j.summary.failures.length} member(s) failed to join: ${JSON.stringify(j.summary.failures.slice(0, 3))}`)
  if (j.summary.neverDecoded.length) fail(`${f}: viewers never decoded: ${j.summary.neverDecoded.join(',')}`)
  if (j.summary.longestStallS > T.stallS) fail(`${f}: longest stall ${j.summary.longestStallS} s > ${T.stallS} s`)
}

const reload = read('reload.json')
if (reload) {
  say(`reload     ${reload.summary.members} members, ${reload.summary.checks} audio checks, silent: ${reload.summary.silent.join('; ') || 'none'}`)
  if (reload.summary.silent.length) fail(`reload: no audio after: ${reload.summary.silent.join('; ')}`)
}

// Server metrics: epoch phase goroutines heap_alloc sys (tab separated).
if (existsSync(join(dir, 'metrics.tsv'))) {
  const rows = readFileSync(join(dir, 'metrics.tsv'), 'utf8').trim().split('\n').map((l) => l.split('\t')).filter((r) => r.length >= 5)
  const col = (phase, i) => rows.filter((r) => r[1] === phase).map((r) => Number(r[i]))
  const avg = (a) => a.reduce((s, x) => s + x, 0) / a.length
  const mb = (b) => (b / 1048576).toFixed(0)
  const pre = col('pre', 2), post = col('post', 2)
  const hold = rows.filter((r) => r[1] === 'hold')
  const peakHeap = Math.max(0, ...rows.map((r) => Number(r[3])))
  const peakGor = Math.max(0, ...rows.map((r) => Number(r[2])))
  let msg = `server     peak heap ${mb(peakHeap)} MB, peak goroutines ${peakGor}`
  if (hold.length >= 6) {
    const third = Math.floor(hold.length / 3)
    const h = hold.map((r) => Number(r[3]))
    const first = avg(h.slice(0, third)), lastAvg = avg(h.slice(-third))
    const pct = ((lastAvg - first) / first) * 100
    msg += `, hold heap ${mb(first)} -> ${mb(lastAvg)} MB (${pct >= 0 ? '+' : ''}${pct.toFixed(0)}%)`
    if (pct > T.heapPct) fail(`server: heap grew ${pct.toFixed(0)}% during hold (> ${T.heapPct}%)`)
  } else msg += ' (hold too short to judge heap growth)'
  if (pre.length && post.length) {
    const leak = post.at(-1) - pre[0]
    msg += `, goroutines ${pre[0]} -> ${post.at(-1)} after leave`
    if (leak > T.goroutineLeak) fail(`server: ${leak} goroutines left after everyone left (> ${T.goroutineLeak})`)
  }
  say(msg)
}

console.log('\n=== load test summary ===')
for (const l of lines) console.log(l)
console.log(violations.length ? `\nFAILED (${violations.length}):\n  - ${violations.join('\n  - ')}` : '\nOK: all thresholds met')
writeFileSync(join(dir, 'summary.json'), JSON.stringify({ ok: violations.length === 0, thresholds: T, lines, violations }, null, 1))
process.exit(violations.length ? 1 : 0)
