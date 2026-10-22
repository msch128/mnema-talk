import { afterEach, describe, expect, it, vi } from 'vitest'
import { MemoryCaches } from './cacheStorage.fixture'
import { installWorker, type FetchLike, type WaitEvent, type WorkerScope } from './worker'
import { shellCacheName } from './shell'

type Listener = (event: never) => void

function fakeScope(fetchResponse: () => Promise<Response> = async () => new Response('x')) {
  const listeners = new Map<string, Listener>()
  const caches = new MemoryCaches()
  const scope = {
    location: { origin: 'https://app.test' },
    caches: caches.asStorage(),
    clients: { claim: vi.fn(async () => {}) },
    skipWaiting: vi.fn(async () => {}),
    fetch: vi.fn(async () => fetchResponse()),
    addEventListener(type: string, listener: Listener) { listeners.set(type, listener) },
  }
  return { scope: scope as unknown as WorkerScope, raw: scope, caches, listeners }
}

function waitEvent() {
  const work: Promise<unknown>[] = []
  const event: WaitEvent = { waitUntil: p => { work.push(p) } }
  return { event, done: () => Promise.all(work) }
}

function fetchEvent(url: string, init: { method?: string, mode?: RequestMode } = {}) {
  const request = new Request(url, { method: init.method ?? 'GET' })
  if (init.mode) Object.defineProperty(request, 'mode', { value: init.mode })
  let answer: Promise<Response> | undefined
  const event: FetchLike = { request, respondWith: p => { answer = p } }
  return { event, answer: () => answer }
}

const config = { version: 'v1', precache: ['/', '/assets/app-1.js', '/favicon.svg'] }

afterEach(() => vi.restoreAllMocks())

describe('the service worker', () => {
  it('stores its shell on install and takes over at once', async () => {
    const { scope, raw, caches, listeners } = fakeScope()
    installWorker(scope, config)
    const wait = waitEvent()
    listeners.get('install')!(wait.event as never)
    await wait.done()
    expect([...caches.stores.get(shellCacheName('v1'))!.entries.keys()]).toEqual(config.precache)
    expect(raw.skipWaiting).toHaveBeenCalled()
  })

  it('drops older shells on activation and claims its pages', async () => {
    const { scope, raw, caches, listeners } = fakeScope()
    await caches.open(shellCacheName('v0'))
    installWorker(scope, config)
    const wait = waitEvent()
    listeners.get('activate')!(wait.event as never)
    await wait.done()
    expect(await caches.keys()).toEqual([])
    expect(raw.clients.claim).toHaveBeenCalled()
  })

  it('answers a page load with the network page', async () => {
    const { scope, listeners } = fakeScope(async () => new Response('<html>page</html>', { headers: { 'content-type': 'text/html' } }))
    installWorker(scope, config)
    const e = fetchEvent('https://app.test/c/1', { mode: 'navigate' })
    listeners.get('fetch')!(e.event as never)
    expect(await (await e.answer()!).text()).toContain('page')
  })

  it('answers built assets from the cache after the first fetch', async () => {
    const { scope, raw, listeners } = fakeScope(async () => new Response('code'))
    installWorker(scope, config)
    for (let i = 0; i < 2; i++) {
      const e = fetchEvent('https://app.test/assets/app-1.js')
      listeners.get('fetch')!(e.event as never)
      expect(await (await e.answer()!).text()).toBe('code')
    }
    expect(raw.fetch).toHaveBeenCalledTimes(1)
  })

  it('does not answer API calls: they go to the network untouched', () => {
    const { scope, listeners } = fakeScope()
    installWorker(scope, config)
    const e = fetchEvent('https://app.test/api/messages')
    listeners.get('fetch')!(e.event as never)
    expect(e.answer()).toBeUndefined()
  })
})
