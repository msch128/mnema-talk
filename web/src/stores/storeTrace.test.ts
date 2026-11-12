// The store trace: does an open client end where a freshly loaded one starts?
// Frames recorded by internal/server/store_trace_integration_test.go are
// replayed into the real stores (storeTrace.fixture.ts) as they arrived, and
// under perturbations: a dropped connection at every position, the answers of
// the reconnect early or late, and an answer made at a frame that arrives
// before or after the next frames. A value that is right after the clean run
// must stay right under every perturbation; the clean run itself is compared
// with a fresh client at every snapshot.
//
// Smoke: the checked-in traces in ./traces, every position. Deep:
// STORE_TRACE_DIR=<dir of traces> (written by the Go test with
// STORE_TRACE_OUT and STORE_TRACE_SEEDS), every position, sharded with
// STORE_TRACE_SHARD=<i>/<n>. What breaks today is in storeTrace.known.ts; the
// list may only shrink.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { classOf, difference, fresh, replay, withoutDeletedThreads, wrong, type Plan, type StoreTrace } from './storeTrace.fixture'
import { KNOWN_BROKEN } from './storeTrace.known'

const deepDir = process.env['STORE_TRACE_DIR'] ?? ''
const [shard, shards] = (process.env['STORE_TRACE_SHARD'] ?? '1/1').split('/').map(Number) as [number, number]

function loadTraces(dir: string): [string, StoreTrace][] {
  return readdirSync(dir).filter(f => f.endsWith('.json')).sort()
    .filter((_, i) => i % shards === shard - 1)
    .map(f => [f, JSON.parse(readFileSync(join(dir, f), 'utf8')) as StoreTrace])
}

/** The perturbations at frame `at`: a drop of one operation's frames (answered in order, early, late) and an answer out of line. */
function plansAt(trace: StoreTrace, at: number): Plan[] {
  const next = trace.snapshots.findIndex(s => s.frames > at)
  const until = next < 0 ? trace.snapshots.length - 1 : next
  return [
    { kind: 'drop', at, until }, { kind: 'drop', at, until, answer: 'early' }, { kind: 'drop', at, until, answer: 'late' },
    { kind: 'early', at }, { kind: 'late', at, by: 2 }
  ]
}

interface Sweep { broken: Map<string, number>; example: Map<string, string>; runs: number; perturbed: number }

function note(s: Sweep, key: string, example: string) {
  s.broken.set(key, (s.broken.get(key) ?? 0) + 1)
  if (!s.example.has(key)) s.example.set(key, example)
}

/** The clean run against a fresh client at every snapshot whose frames have all arrived. */
async function sweepClean(file: string, trace: StoreTrace, s: Sweep) {
  const want = new Map<number, Awaited<ReturnType<typeof fresh>>>()
  for (let j = 0; j < trace.snapshots.length; j++) want.set(j, await fresh(trace, j))
  const seen = new Set<string>()
  const once = (k: string, example: () => string) => { if (!seen.has(k)) { seen.add(k); note(s, k, example()) } }
  await replay(trace, { kind: 'clean' }, {
    checkpoint: (j, got) => {
      const w = want.get(j)
      if (!w) return
      s.perturbed++
      const shown = withoutDeletedThreads(got, trace, trace.snapshots[j]?.frames)
      for (const key of wrong(shown, w)) once(`clean ${classOf(key)}`, () => `${file} at op ${trace.snapshots[j]?.op}: ${difference(shown, w).join('; ')}`)
    },
    reply: (i, thread) => once('clean reply-follow', () => `${file} frame ${i}: the client decides a reply in ${thread} from a follow list the server does not have`)
  })
  s.runs++
}

async function sweepTrace(file: string, trace: StoreTrace, step: number, s: Sweep) {
  await sweepClean(file, trace, s)
  const want = await fresh(trace)
  const right = (c: Awaited<ReturnType<typeof fresh>>) => wrong(withoutDeletedThreads(c, trace), want)
  const cleanWrong = right(await replay(trace, { kind: 'clean' }))
  for (let at = 0; at < trace.frames.length; at += step) {
    for (const plan of plansAt(trace, at)) {
      const got = await replay(trace, plan)
      s.runs++
      s.perturbed++
      for (const key of right(got)) {
        if (cleanWrong.has(key)) continue
        const label = plan.kind === 'drop' ? `drop${plan.answer ? '+' + plan.answer : ''}` : plan.kind
        note(s, `${label} ${classOf(key)}`, `${file} ${JSON.stringify(plan)}: ${difference(got, want).join('; ')}`)
      }
    }
  }
}

beforeEach(() => {
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

/** `want`: which known keys must still break (smoke, nightly, or none for a small or sharded deep run). */
function check(s: Sweep, want: 'smoke' | 'nightly' | 'none') {
  // STORE_TRACE_REPORT=<file>: the counts per key and one example each, for the record.
  const report = process.env['STORE_TRACE_REPORT']
  if (report) writeFileSync(report, JSON.stringify({ runs: s.runs, compared: s.perturbed, broken: Object.fromEntries(s.broken), examples: Object.fromEntries(s.example) }, null, 1))
  const unknown = [...s.broken.keys()].filter(k => !(k in KNOWN_BROKEN)).map(k => `${k}: ${s.example.get(k)}`)
  expect(unknown, 'a perturbation breaks state that is not on the known list').toEqual([])
  const passing = Object.entries(KNOWN_BROKEN).filter(([k, v]) => want !== 'none' && v[want] && !s.broken.has(k)).map(([k]) => k)
  expect(passing, 'a known failure no longer fails: take it off storeTrace.known.ts').toEqual([])
}

describe('store trace against fresh clients', () => {
  it('the checked-in traces, every position', async () => {
    const s: Sweep = { broken: new Map(), example: new Map(), runs: 0, perturbed: 0 }
    const traces = loadTraces(join(__dirname, 'traces'))
    expect(traces.length).toBeGreaterThan(0)
    for (const [file, trace] of traces) await sweepTrace(file, trace, 1, s)
    // The sweep did what it claims: every trace ran its clean comparison and its perturbations.
    expect(s.perturbed).toBeGreaterThan(traces.length * 100)
    check(s, 'smoke')
  }, 120_000)

  it.skipIf(!deepDir)('deep: every trace in STORE_TRACE_DIR, every position', async () => {
    const s: Sweep = { broken: new Map(), example: new Map(), runs: 0, perturbed: 0 }
    const traces = loadTraces(deepDir)
    expect(traces.length).toBeGreaterThan(0)
    for (const [file, trace] of traces) await sweepTrace(file, trace, 1, s)
    expect(s.perturbed).toBeGreaterThan(traces.length * 200)
    check(s, shards === 1 && traces.length >= 20 ? 'nightly' : 'none')
  }, 3_600_000)
})
