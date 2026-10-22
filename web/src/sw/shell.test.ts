import { describe, expect, it } from 'vitest'
import { MemoryCaches } from './cacheStorage.fixture'
import { SHELL_KEY, answerAsset, answerPage, dropOldShells, precache, shellCacheName, strategyFor } from './shell'

const ORIGIN = 'https://app.test'
const precached = new Set(['/favicon.svg', '/manifest.webmanifest'])

function strategy(path: string, init: { method?: string, mode?: string, origin?: string } = {}) {
  return strategyFor({
    method: init.method ?? 'GET',
    url: new URL(path, init.origin ?? ORIGIN),
    mode: init.mode ?? 'cors',
    origin: ORIGIN,
    precached,
  })
}

describe('which requests the worker answers', () => {
  it('serves page loads from the shell and built assets from the cache', () => {
    expect(strategy('/c/42', { mode: 'navigate' })).toBe('shell')
    expect(strategy('/assets/index-abc123.js')).toBe('asset')
    expect(strategy('/favicon.svg')).toBe('asset')
  })

  it('never touches the API, even for a page load of an API address', () => {
    expect(strategy('/api/messages')).toBe('network')
    expect(strategy('/api/docs', { mode: 'navigate' })).toBe('network')
    expect(strategy('/api/ws')).toBe('network')
  })

  it('leaves writes and other origins to the network', () => {
    expect(strategy('/assets/index-abc123.js', { method: 'POST' })).toBe('network')
    expect(strategy('/assets/index-abc123.js', { origin: 'https://cdn.test' })).toBe('network')
    expect(strategy('/uploads/file.png')).toBe('network')
  })
})

describe('the cache of one build', () => {
  it('is named after the build and cleared of other builds, and of nothing else', async () => {
    const caches = new MemoryCaches()
    await caches.open(shellCacheName('old'))
    await caches.open(shellCacheName('new'))
    await caches.open('someone-elses')
    await dropOldShells(caches.asStorage(), shellCacheName('new'))
    expect(await caches.keys()).toEqual([shellCacheName('new'), 'someone-elses'])
  })

  it('stores every fixed file at install', async () => {
    const caches = new MemoryCaches()
    await precache(caches.asStorage(), 'shell-x', ['/', '/assets/a.js'])
    expect([...caches.stores.get('shell-x')!.entries.keys()]).toEqual(['/', '/assets/a.js'])
  })
})

describe('page loads', () => {
  const html = () => new Response('<html>fresh</html>', { status: 200, headers: { 'content-type': 'text/html; charset=utf-8' } })

  it('use the network and keep the page for offline use', async () => {
    const caches = new MemoryCaches()
    const res = await answerPage(caches.asStorage(), 'shell', async () => html())
    expect(await res.text()).toContain('fresh')
    const kept = await caches.stores.get('shell')!.match(SHELL_KEY)
    expect(await kept!.text()).toContain('fresh')
  })

  it('fall back to the kept page when the network is gone', async () => {
    const caches = new MemoryCaches()
    await answerPage(caches.asStorage(), 'shell', async () => html())
    const res = await answerPage(caches.asStorage(), 'shell', async () => { throw new TypeError('offline') })
    expect(await res.text()).toContain('fresh')
  })

  it('fail like the browser would when nothing was kept', async () => {
    const res = await answerPage(new MemoryCaches().asStorage(), 'shell', async () => { throw new TypeError('offline') })
    expect(res.type).toBe('error')
  })

  it('do not keep what is not the app page', async () => {
    const caches = new MemoryCaches()
    for (const odd of [
      new Response('{"error":"x"}', { status: 200, headers: { 'content-type': 'application/json' } }),
      new Response('<html>missing</html>', { status: 404, headers: { 'content-type': 'text/html' } }),
    ]) {
      await answerPage(caches.asStorage(), 'shell', async () => odd)
    }
    expect(caches.stores.get('shell')).toBeUndefined()
  })
})

describe('built assets', () => {
  const request = new Request(`${ORIGIN}/assets/app-1.js`)

  it('are fetched once and then served from the cache', async () => {
    const caches = new MemoryCaches()
    let fetched = 0
    const fetchIt = async () => { fetched++; return new Response('code', { status: 200 }) }
    await answerAsset(caches.asStorage(), 'shell', request, fetchIt)
    const again = await answerAsset(caches.asStorage(), 'shell', request, fetchIt)
    expect(await again.text()).toBe('code')
    expect(fetched).toBe(1)
  })

  it('are not kept when the server did not answer 200', async () => {
    const caches = new MemoryCaches()
    const res = await answerAsset(caches.asStorage(), 'shell', request, async () => new Response('nope', { status: 404 }))
    expect(res.status).toBe(404)
    expect(await caches.stores.get('shell')!.match(request)).toBeUndefined()
  })
})
