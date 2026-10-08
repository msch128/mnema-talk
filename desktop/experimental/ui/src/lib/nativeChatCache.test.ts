import { expect, it } from 'vitest'
import { NativeChatDisplayCache } from './nativeChatCache'
const CHANNEL = '00000000-0000-4000-8000-000000000001'
function uuid(n: number) { return `00000000-0000-4000-8000-${n.toString(16).padStart(12, '0')}` }
function row(n: number, body = 'synthetic', channel = CHANNEL) { return { id: uuid(n), number: n, channel_id: channel, client_event_id: uuid(n), account_id: uuid(2), device_id: uuid(3), body } }
it('clones accepted projections on insert and snapshots and rejects immutable identity conflicts', () => {
  const cache = new NativeChatDisplayCache(); const scoped = cache.scope('synthetic-scope', CHANNEL)
  const source = row(1); scoped.store([source]); source.body = 'mutated input'
  const snapshot = scoped.snapshot(); expect(snapshot[0]?.body).toBe('synthetic')
  if (snapshot[0]) snapshot[0].body = 'mutated output'
  expect(scoped.snapshot()[0]?.body).toBe('synthetic')
  expect(() => scoped.store([row(1, 'conflicting')])).toThrow(); expect(scoped.snapshot()[0]?.body).toBe('synthetic')
  cache.clear(); expect(scoped.snapshot()).toEqual([])
})
it('limits channel LRU and latest rows without conflating channels or consuming crypto again', () => {
  const cache = new NativeChatDisplayCache(); const scoped = cache.scope('rows', CHANNEL)
  scoped.store(Array.from({ length: 201 }, (_, index) => row(index + 1)))
  expect(scoped.snapshot()).toHaveLength(200); expect(scoped.snapshot()[0]?.number).toBe(2)
  for (let n = 0; n < 16; n++) cache.scope(`channel-${n}`, CHANNEL).store([row(n + 1)])
  expect(scoped.snapshot()).toEqual([])
  expect(() => cache.scope('wrong', CHANNEL).store([row(1, 'synthetic', uuid(4))])).toThrow()
})
it('limits retained UTF8 JSON payload to2MiB including multibyte body and DTO metadata', () => {
  const cache = new NativeChatDisplayCache(); const scoped = cache.scope('large', CHANNEL)
  scoped.store(Array.from({ length: 200 }, (_, index) => row(index + 1, '🌲'.repeat(6144))))
  const snapshot = scoped.snapshot()
  expect(snapshot.length).toBeLessThan(200)
  expect(snapshot.reduce((total, item) => total + new TextEncoder().encode(JSON.stringify(item)).length, 0)).toBeLessThanOrEqual(2 * 1024 * 1024)
  expect(snapshot.at(-1)?.number).toBe(200)
})
