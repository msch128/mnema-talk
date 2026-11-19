// The saved store against the server, as a property: random saves and
// removals here (answered late, in order), saves and removals in another
// session with their saved_updated, deletes of roots with saved replies, lost
// events while the socket is down, reconnects, and pages the server reads at
// a random moment and that land in any order. At quiet the list and the saved
// marks equal the server's saved set (H-L5-02 and H-L5-03; written by the
// independent review of the fix). SAVED_PROPERTY_SEEDS=<n> runs more seeds.
// Local and remote changes use disjoint ids, and a message is deleted only
// while no own write on it is in flight: races the store cannot know of stay out.
import { ApiError, type ApiOptions } from '../shared/lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { useSavedStore } from './saved'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

function rng(seed: number) {
  let s = seed >>> 0 || 1
  return () => { s ^= s << 13; s >>>= 0; s ^= s >>> 17; s ^= s << 5; s >>>= 0; return s / 0x100000000 }
}

const ROOTS = [1, 2, 3, 4, 5, 6]
const REPLIES: Record<number, number> = { 7: 1, 8: 1, 9: 2, 10: 3 }
const ALL = [...ROOTS, ...Object.keys(REPLIES).map(Number)]

type Req = { path: string; method: string; processed: boolean; result?: unknown; error?: unknown; resolve: (v: unknown) => void; reject: (e: unknown) => void }

type Sim = {
  pick: <T>(xs: T[]) => T
  roll: () => number
  server: Map<number, number> // id -> saved order
  deleted: Set<number>
  clock: number
  pending: Req[]
  events: Array<() => void>
  connected: boolean
  saved: ReturnType<typeof useSavedStore>
  universe: number[]
  withReplies: boolean
  log: string[]
}

const idOf = (s: Sim, str: string) => s.universe.find(i => fixtureId(i) === str) as number
const lastSegment = (path: string) => path.split('/').pop() as string

const entryOf = (id: number) => ({
  message_id: fixtureId(id), note: '', saved_at: FIXTURE_TIMESTAMP, available: true,
  message: { id: fixtureId(id), parent_id: REPLIES[id] ? fixtureId(REPLIES[id]) : null }
})

function processRequest(s: Sim, q: Req) {
  q.processed = true
  if (q.method === 'GET') {
    const ids = [...s.server.entries()].sort((a, b) => b[1] - a[1]).map(([id]) => id)
    q.result = { items: ids.map(entryOf), has_more: false }
    return
  }
  const id = idOf(s, lastSegment(q.path))
  if (q.method === 'PUT') {
    if (s.deleted.has(id)) { q.error = new ApiError(404, 'NOT_FOUND', 'gone'); return }
    if (!s.server.has(id)) s.server.set(id, ++s.clock)
    q.result = entryOf(id)
    // the server tells this member's other sessions, not this one: no event here
  } else {
    s.server.delete(id)
    q.result = null
  }
}

function deliver(s: Sim, q: Req) {
  s.pending.splice(s.pending.indexOf(q), 1)
  if (q.error) q.reject(q.error); else q.resolve(q.result)
}

const emit = (s: Sim, f: () => void) => { if (s.connected) s.events.push(f) }

function deleteMessage(s: Sim, id: number) {
  s.deleted.add(id); s.server.delete(id)
  for (const [reply, root] of Object.entries(REPLIES)) if (root === id) { s.deleted.add(Number(reply)); s.server.delete(Number(reply)) }
  emit(s, () => s.saved.onMessageDeleted(fixtureId(id)))
}

function newSim(seed: number, withReplies: boolean): Sim {
  const roll = rng(seed)
  const s: Sim = {
    roll,
    pick: <T>(xs: T[]): T => xs[Math.floor(roll() * xs.length)] as T,
    server: new Map(), deleted: new Set(), clock: 0, pending: [], events: [], connected: true,
    saved: useSavedStore(), universe: withReplies ? ALL : ROOTS, withReplies, log: []
  }
  apiMock.mockImplementation((path, options) => new Promise((resolve, reject) => {
    s.pending.push({ path, method: options?.method ?? 'GET', processed: false, resolve, reject })
  }))
  return s
}

// A message is busy while an own write on it (or on one of its replies) is in flight.
function isBusy(s: Sim, m: number) {
  return s.pending.some(q => {
    if (q.method === 'GET') return false
    const id = idOf(s, lastSegment(q.path))
    return id === m || REPLIES[id] === m
  })
}

// Returns false when the step does nothing and must not yield to the event loop.
function ownAndRemoteStep(s: Sim, roll: number, id: number): boolean {
  if (roll < 0.12) { s.log.push(`save ${id}`); s.saved.save(fixtureId(id)).catch(() => undefined); return true }
  if (roll < 0.2) { s.log.push(`remove ${id}`); s.saved.remove(fixtureId(id)).catch(() => undefined); return true }
  if (roll < 0.3) {
    if (s.deleted.has(id)) return false
    s.log.push(`remoteSave ${id}`); if (!s.server.has(id)) s.server.set(id, ++s.clock)
    emit(s, () => s.saved.onUpdated({ message_id: fixtureId(id), saved: true }))
    return true
  }
  if (roll < 0.38) {
    s.log.push(`remoteRemove ${id}`); s.server.delete(id)
    emit(s, () => s.saved.onUpdated({ message_id: fixtureId(id), saved: false }))
    return true
  }
  if (s.deleted.has(id) || isBusy(s, id)) return false
  s.log.push(`delete ${id}`); deleteMessage(s, id)
  return true
}

function processStep(s: Sim) {
  let q = s.pick(s.pending.filter(x => !x.processed))
  if (q.method !== 'GET') q = s.pending.find(x => !x.processed && x.method !== 'GET') as Req
  s.log.push(`process ${q.method}`); processRequest(s, q)
}

function deliverStep(s: Sim): boolean {
  let q = s.pick(s.pending.filter(x => x.processed))
  if (q.method !== 'GET') q = s.pending.find(x => x.method !== 'GET') as Req
  if (!q.processed) return false
  s.log.push(`deliver ${q.method}`); deliver(s, q)
  return true
}

function networkStep(s: Sim, roll: number): boolean {
  if (roll < 0.48 && s.connected) { s.log.push('disconnect'); s.connected = false; s.events.length = 0 }
  else if (roll < 0.55 && !s.connected) { s.log.push('reconnect'); s.connected = true; void s.saved.refresh() }
  else if (roll < 0.75 && s.pending.some(q => !q.processed)) processStep(s)
  else if (roll < 0.9 && s.pending.some(q => q.processed)) return deliverStep(s)
  else if (s.events.length) { s.log.push('event'); (s.events.shift() as () => void)() }
  return true
}

async function step(s: Sim) {
  const roll = s.roll()
  const localIds = s.withReplies ? [1, 2, 3, 7, 8] : [1, 2, 3]
  const remoteIds = s.withReplies ? [4, 5, 6, 9, 10] : [4, 5, 6]
  const id = roll < 0.2 ? s.pick(localIds) : roll < 0.38 ? s.pick(remoteIds) : s.pick(s.universe)
  const act = roll < 0.43 ? ownAndRemoteStep(s, roll, id) : networkStep(s, roll)
  if (act) await flushPromises()
}

async function settle(s: Sim) {
  if (!s.connected) { s.connected = true; void s.saved.refresh() }
  for (let guard = 0; guard < 1000 && (s.pending.length || s.events.length); guard++) {
    await flushPromises()
    if (s.events.length) { (s.events.shift() as () => void)(); continue }
    const q = s.pending[0] as Req
    if (!q.processed) processRequest(s, q)
    deliver(s, q)
  }
  await flushPromises()
}

async function run(seed: number, steps: number, withReplies: boolean) {
  const s = newSim(seed, withReplies)
  void s.saved.ensureLoaded()
  while (s.pending.length) { const q = s.pending[0] as Req; processRequest(s, q); deliver(s, q) }
  await flushPromises()
  for (let i = 0; i < steps; i++) await step(s)
  await settle(s)
  const want = [...s.server.keys()].sort((a, b) => a - b)
  const got = s.saved.items.map(e => idOf(s, e.message_id)).sort((a, b) => a - b)
  const marks = s.universe.filter(i => s.saved.isSaved(fixtureId(i))).sort((a, b) => a - b)
  return { want, got, marks, log: s.log }
}

const SEEDS = Number(process.env['SAVED_PROPERTY_SEEDS'] ?? 1000)

const range = (n: number) => Array.from({ length: n }, (_, i) => i + 1)

async function sweep(withReplies: boolean, seeds: number[]) {
  const bad: string[] = []
  for (const seed of seeds) {
    setActivePinia(createPinia())
    const res = await run(seed, 60, withReplies)
    if (JSON.stringify(res.want) !== JSON.stringify(res.got) || JSON.stringify(res.want) !== JSON.stringify(res.marks)) {
      bad.push(`seed ${seed}: server ${res.want} items ${res.got} marks ${res.marks}\n  ${res.log.join(', ')}`)
    }
  }
  return bad
}

describe('the saved store against the server', () => {
  it('ends with the server\'s list and marks after random changes, lost events and late answers', async () => {
    expect((await sweep(false, range(SEEDS))).slice(0, 3)).toEqual([])
  }, 120_000)

  it('the same with thread replies whose root is deleted', async () => {
    expect((await sweep(true, range(SEEDS))).slice(0, 3)).toEqual([])
  }, 120_000)

  // Found at 5,000 seeds: a reply saved elsewhere while the socket was down
  // came back with a page that was on its way when its root was deleted.
  it('keeps a deleted root\'s saved replies out of a page that was on its way (seeds 2790 and 4690)', async () => {
    expect(await sweep(true, [2790, 4690])).toEqual([])
  })
})
