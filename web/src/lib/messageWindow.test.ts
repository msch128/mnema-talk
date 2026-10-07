import { requireValue } from '../test-fixtures.fixture'
import type { MessageWindow } from './messageWindow'
import { describe, it, expect } from 'vitest'
import {
  PAGE_SIZE, WINDOW_CAP, emptyWindow, dedupe, fromLatest, fromAround,
  prependOlder, appendNewer, appendLive, removeMessage
} from './messageWindow'

// Fake server with the same paging contract as GET /api/channels/{id}/messages.
interface SyntheticMessage { id: string; n: number }
function makeServer(count: number) {
  const all = Array.from({ length: count }, (_, i) => ({ id: `m${i}`, n: i }))
  const index = new Map(all.map((m, i) => [m.id, i]))
  return {
    all,
    push() {
      const m = { id: `m${all.length}`, n: all.length }
      index.set(m.id, all.length)
      all.push(m)
      return m
    },
    latest(limit = PAGE_SIZE) { return all.slice(Math.max(0, all.length - limit)) },
    before(id: string, limit = PAGE_SIZE) { const i = requireValue(index.get(id)); return all.slice(Math.max(0, i - limit), i) },
    after(id: string, limit = PAGE_SIZE) { const i = requireValue(index.get(id)); return all.slice(i + 1, i + 1 + limit) },
    around(id: string, limit = PAGE_SIZE) {
      const i = requireValue(index.get(id))
      const older = all.slice(Math.max(0, i + 1 - (Math.floor(limit / 2) + 1)), i + 1)
      const newer = all.slice(i + 1, i + 1 + (limit - older.length))
      return older.concat(newer)
    }
  }
}

const ns = (win: MessageWindow<SyntheticMessage>) => win.messages.map(m => m.n)

function assertContiguous(win: MessageWindow<SyntheticMessage>) {
  const n = ns(win)
  for (let i = 1; i < n.length; i++) expect(n[i]).toBe(requireValue(n[i - 1]) + 1)
  expect(new Set(win.messages.map(m => m.id)).size).toBe(win.messages.length)
  expect(win.messages.length).toBeLessThanOrEqual(WINDOW_CAP)
}

describe('messageWindow basics', () => {
  it('starts empty and dedupes by id', () => {
    // Intentionally malformed entry exercises the helper's defensive ID check.
    const missingId = {} as { id: string }
    expect(emptyWindow()).toEqual({ messages: [], hasMoreBefore: false, hasMoreAfter: false })
    expect(dedupe([{ id: '1' }, { id: '1' }, null, { id: '2' }, missingId]).map(m => m.id)).toEqual(['1', '2'])
  })

  it('fromLatest sets hasMoreBefore only for full pages', () => {
    const s = makeServer(120)
    expect(fromLatest(s.latest())).toMatchObject({ hasMoreBefore: true, hasMoreAfter: false })
    const small = makeServer(10)
    expect(fromLatest(small.latest())).toMatchObject({ hasMoreBefore: false, hasMoreAfter: false })
    expect(fromLatest(null).messages).toEqual([])
  })

  it('fromAround detects reached ends from the anchor position', () => {
    const s = makeServer(1000)
    expect(fromAround(s.around('m500'), 'm500')).toMatchObject({ hasMoreBefore: true, hasMoreAfter: true })
    const nearStart = fromAround(s.around('m3'), 'm3')
    expect(nearStart).toMatchObject({ hasMoreBefore: false, hasMoreAfter: true })
    expect(nearStart.messages).toHaveLength(PAGE_SIZE)
    const nearEnd = fromAround(s.around('m990'), 'm990')
    expect(nearEnd).toMatchObject({ hasMoreBefore: true, hasMoreAfter: false })
    const tiny = makeServer(5)
    expect(fromAround(tiny.around('m2'), 'm2')).toMatchObject({ hasMoreBefore: false, hasMoreAfter: false })
  })

  it('prepend preserves order, dedupes and trims the bottom past the cap', () => {
    let win = { messages: [{ id: 'b' }, { id: 'c' }], hasMoreBefore: true, hasMoreAfter: false }
    win = prependOlder(win, [{ id: 'a' }, { id: 'b' }], { anchorId: 'b', limit: 2 })
    expect(win.messages.map(m => m.id)).toEqual(['a', 'b', 'c'])
    expect(win.hasMoreBefore).toBe(true)
    const trimmed = prependOlder(win, [{ id: 'z' }], { anchorId: 'a', limit: 2, cap: 3 })
    expect(trimmed.messages.map(m => m.id)).toEqual(['z', 'a', 'b'])
    expect(trimmed).toMatchObject({ hasMoreBefore: false, hasMoreAfter: true })
  })

  it('discards stale page responses instead of creating gaps', () => {
    const win = { messages: [{ id: 'b' }, { id: 'c' }], hasMoreBefore: true, hasMoreAfter: true }
    expect(prependOlder(win, [{ id: 'x' }], { anchorId: 'a' })).toBe(win)
    expect(appendNewer(win, [{ id: 'x' }], { anchorId: 'b' })).toBe(win)
  })

  it('append trims the top past the cap and a full page of duplicates stops paging', () => {
    let win = { messages: [{ id: 'a' }, { id: 'b' }], hasMoreBefore: false, hasMoreAfter: true }
    win = appendNewer(win, [{ id: 'c' }, { id: 'd' }], { anchorId: 'b', limit: 2, cap: 3 })
    expect(win.messages.map(m => m.id)).toEqual(['b', 'c', 'd'])
    expect(win).toMatchObject({ hasMoreBefore: true, hasMoreAfter: true })
    const stuck = appendNewer(win, [{ id: 'c' }, { id: 'd' }], { anchorId: 'd', limit: 2 })
    expect(stuck.hasMoreAfter).toBe(false)
  })

  it('live messages append, dedupe, and refuse to create a gap', () => {
    const win = { messages: [{ id: 'a' }], hasMoreBefore: false, hasMoreAfter: false }
    const r1 = appendLive(win, { id: 'b' })
    expect(r1.status).toBe('appended')
    expect(r1.window.messages.map(m => m.id)).toEqual(['a', 'b'])
    expect(appendLive(r1.window, { id: 'b' }).status).toBe('duplicate')
    expect(appendLive({ ...win, hasMoreAfter: true }, { id: 'c' }).status).toBe('gap')
    const capped = appendLive(r1.window, { id: 'c' }, { cap: 2 })
    expect(capped.window.messages.map(m => m.id)).toEqual(['b', 'c'])
    expect(capped.window.hasMoreBefore).toBe(true)
  })

  it('removes messages by id', () => {
    const win = { messages: [{ id: 'a' }, { id: 'b' }], hasMoreBefore: false, hasMoreAfter: false }
    expect(removeMessage(win, 'a').messages.map(m => m.id)).toEqual(['b'])
    expect(removeMessage(win, 'zz')).toBe(win)
  })
})

describe('messageWindow 15k paging simulation', () => {
  const TOTAL = 15000

  it('pages from the newest to the very first message and back without gaps or duplicates', () => {
    const s = makeServer(TOTAL)
    let win = fromLatest(s.latest())
    const seen = new Set(win.messages.map(m => m.id))
    let olderLoads = 0

    while (win.hasMoreBefore) {
      win = prependOlder(win, s.before(requireValue(win.messages[0]).id), { anchorId: requireValue(win.messages[0]).id })
      win.messages.forEach(m => seen.add(m.id))
      assertContiguous(win)
      olderLoads++
      expect(olderLoads).toBeLessThan(TOTAL)
    }
    expect(requireValue(win.messages[0]).n).toBe(0)
    expect(win.hasMoreAfter).toBe(true)
    expect(seen.size).toBe(TOTAL)

    let newerLoads = 0
    while (win.hasMoreAfter) {
      const last = requireValue(win.messages[win.messages.length - 1]).id
      win = appendNewer(win, s.after(last), { anchorId: last })
      assertContiguous(win)
      newerLoads++
      expect(newerLoads).toBeLessThan(TOTAL)
    }
    expect(requireValue(win.messages[win.messages.length - 1]).n).toBe(TOTAL - 1)
    expect(win.messages).toHaveLength(WINDOW_CAP)
  }, 30_000)

  it('jumps around randomly, mixes live messages and stays contiguous', () => {
    const s = makeServer(TOTAL)
    let seed = 42
    const rand = () => (seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648

    let win = fromLatest(s.latest())
    for (let step = 0; step < 1500; step++) {
      const r = rand()
      if (r < 0.03) {
        const target = requireValue(s.all[Math.floor(rand() * s.all.length)]).id
        win = fromAround(s.around(target), target)
        expect(win.messages.some(m => m.id === target)).toBe(true)
      } else if (r < 0.45 && win.hasMoreBefore) {
        const first = requireValue(win.messages[0]).id
        win = prependOlder(win, s.before(first), { anchorId: first })
      } else if (r < 0.85 && win.hasMoreAfter) {
        const last = requireValue(win.messages[win.messages.length - 1]).id
        win = appendNewer(win, s.after(last), { anchorId: last })
      } else {
        const msg = s.push()
        const res = appendLive(win, msg)
        expect(res.status).toBe(win.hasMoreAfter ? 'gap' : 'appended')
        win = res.window
      }
      assertContiguous(win)
      // Flags must agree with the server's real boundaries.
      if (!win.hasMoreBefore) expect(requireValue(win.messages[0]).n).toBe(0)
      if (!win.hasMoreAfter) expect(requireValue(win.messages[win.messages.length - 1]).n).toBe(s.all.length - 1)
    }
  }, 30_000)

  it('ignores stale responses that race with a window change', () => {
    const s = makeServer(TOTAL)
    let win = fromLatest(s.latest())
    for (let i = 0; i < 6; i++) win = prependOlder(win, s.before(requireValue(win.messages[0]).id), { anchorId: requireValue(win.messages[0]).id })
    // An "after" request goes out, then a prepend trims the bottom before it returns.
    const lastBefore = requireValue(win.messages[win.messages.length - 1]).id
    const pendingAfter = s.after(lastBefore)
    win = prependOlder(win, s.before(requireValue(win.messages[0]).id), { anchorId: requireValue(win.messages[0]).id })
    const result = appendNewer(win, pendingAfter, { anchorId: lastBefore })
    expect(result).toBe(win)
    assertContiguous(result)
  })
})
