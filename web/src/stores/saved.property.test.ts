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

async function run(seed: number, steps: number, WITH_REPLIES: boolean) {
  const r = rng(seed)
  const pick = <T>(xs: T[]): T => xs[Math.floor(r() * xs.length)] as T
  const server = new Map<number, number>() // id -> saved order
  const deleted = new Set<number>()
  let clock = 0
  const pending: Req[] = []
  const events: Array<() => void> = []
  let connected = true
  const saved = useSavedStore()
  const universe = WITH_REPLIES ? ALL : ROOTS
  const entryOf = (id: number) => ({
    message_id: fixtureId(id), note: '', saved_at: FIXTURE_TIMESTAMP, available: true,
    message: { id: fixtureId(id), parent_id: REPLIES[id] ? fixtureId(REPLIES[id]) : null }
  })
  const idOf = (s: string) => universe.find(i => fixtureId(i) === s) as number
  apiMock.mockImplementation((path, options) => new Promise((resolve, reject) => {
    pending.push({ path, method: options?.method ?? 'GET', processed: false, resolve, reject })
  }))
  const process = (q: Req) => {
    q.processed = true
    if (q.method === 'GET') {
      const ids = [...server.entries()].sort((a, b) => b[1] - a[1]).map(([id]) => id)
      q.result = { items: ids.map(entryOf), has_more: false }
      return
    }
    const id = idOf(q.path.split('/').pop() as string)
    if (q.method === 'PUT') {
      if (deleted.has(id)) { q.error = new ApiError(404, 'NOT_FOUND', 'gone'); return }
      if (!server.has(id)) server.set(id, ++clock)
      q.result = entryOf(id)
      // the server tells this member's other sessions, not this one: no event here
    } else {
      server.delete(id)
      q.result = null
    }
  }
  const deliver = (q: Req) => {
    pending.splice(pending.indexOf(q), 1)
    if (q.error) q.reject(q.error); else q.resolve(q.result)
  }
  const emit = (f: () => void) => { if (connected) events.push(f) }
  const deleteMessage = (id: number) => {
    deleted.add(id); server.delete(id)
    for (const [reply, root] of Object.entries(REPLIES)) if (root === id) { deleted.add(Number(reply)); server.delete(Number(reply)) }
    emit(() => saved.onMessageDeleted(fixtureId(id)))
  }

  void saved.ensureLoaded()
  while (pending.length) { const q = pending[0] as Req; process(q); deliver(q) }
  await flushPromises()

  const log: string[] = []
  for (let i = 0; i < steps; i++) {
    const roll = r()
    const localIds = WITH_REPLIES ? [1, 2, 3, 7, 8] : [1, 2, 3]
    const remoteIds = WITH_REPLIES ? [4, 5, 6, 9, 10] : [4, 5, 6]
    const id = roll < 0.2 ? pick(localIds) : roll < 0.38 ? pick(remoteIds) : pick(universe)
    const busy = (m: number) => pending.some(q => q.method !== 'GET' && (idOf(q.path.split('/').pop() as string) === m || REPLIES[idOf(q.path.split('/').pop() as string)] === m))
    if (roll < 0.12) { log.push(`save ${id}`); saved.save(fixtureId(id)).catch(() => undefined) }
    else if (roll < 0.2) { log.push(`remove ${id}`); saved.remove(fixtureId(id)).catch(() => undefined) }
    else if (roll < 0.3) {
      if (deleted.has(id)) continue
      log.push(`remoteSave ${id}`); if (!server.has(id)) server.set(id, ++clock)
      emit(() => saved.onUpdated({ message_id: fixtureId(id), saved: true }))
    } else if (roll < 0.38) {
      log.push(`remoteRemove ${id}`); server.delete(id)
      emit(() => saved.onUpdated({ message_id: fixtureId(id), saved: false }))
    } else if (roll < 0.43) {
      if (deleted.has(id) || busy(id)) continue
      log.push(`delete ${id}`); deleteMessage(id)
    } else if (roll < 0.48 && connected) { log.push('disconnect'); connected = false; events.length = 0 }
    else if (roll < 0.55 && !connected) { log.push('reconnect'); connected = true; void saved.refresh() }
    else if (roll < 0.75 && pending.some(q => !q.processed)) { let q = pick(pending.filter(q => !q.processed)); if (q.method !== 'GET') q = pending.find(x => !x.processed && x.method !== 'GET') as Req; log.push(`process ${q.method}`); process(q) }
    else if (roll < 0.9 && pending.some(q => q.processed)) { let q = pick(pending.filter(q => q.processed)); if (q.method !== 'GET') q = pending.find(x => x.method !== 'GET') as Req; if (!q.processed) continue; log.push(`deliver ${q.method}`); deliver(q) }
    else if (events.length) { log.push('event'); (events.shift() as () => void)() }
    await flushPromises()
  }
  if (!connected) { connected = true; void saved.refresh() }
  for (let guard = 0; guard < 1000 && (pending.length || events.length); guard++) {
    await flushPromises()
    if (events.length) { (events.shift() as () => void)(); continue }
    const q = pending[0] as Req
    if (!q.processed) process(q)
    deliver(q)
  }
  await flushPromises()
  const want = [...server.keys()].sort((a, b) => a - b)
  const got = saved.items.map(e => idOf(e.message_id)).sort((a, b) => a - b)
  const marks = universe.filter(i => saved.isSaved(fixtureId(i))).sort((a, b) => a - b)
  return { want, got, marks, log }
}

const SEEDS = Number(process.env['SAVED_PROPERTY_SEEDS'] ?? 1000)

async function sweep(withReplies: boolean) {
  const bad: string[] = []
  for (let seed = 1; seed <= SEEDS; seed++) {
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
    expect((await sweep(false)).slice(0, 3)).toEqual([])
  }, 120_000)

  it('the same with thread replies whose root is deleted', async () => {
    expect((await sweep(true)).slice(0, 3)).toEqual([])
  }, 120_000)
})
