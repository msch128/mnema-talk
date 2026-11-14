import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useLifetimeStore, threadLifetimeKey } from './lifetime'
import { useServerFeaturesStore } from './serverFeatures'

// The lifetimes a member chose per conversation stay on this device, and
// what the community allows comes from the server (doc/timed-messages.md).

const KEY = 'mnema.lifetimes.v1'
let answer: () => Promise<Response>

function settings(enabled: boolean, max = 2592000) {
  return new Response(JSON.stringify({ message_lifetimes_enabled: enabled, message_lifetime_max_seconds: max }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  answer = async () => settings(true)
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => answer()))
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function availableStore() {
  useServerFeaturesStore().list = ['messages.lifetime']
  const store = useLifetimeStore()
  await store.load()
  return store
}

describe('the lifetime choices kept on this device', () => {
  it('reads back the valid choices and drops what is not a lifetime', async () => {
    localStorage.setItem(KEY, JSON.stringify({ a: 3600, b: 5, c: 'soon', [threadLifetimeKey('t')]: 86400 }))
    const store = await availableStore()
    expect(store.forChannel('a')).toBe(3600)
    expect(store.forChannel('b')).toBe(0)
    expect(store.forChannel('c')).toBe(0)
    expect(store.forChannel('thread:t')).toBe(86400)
  })

  it.each([['an array', '[3600]'], ['not JSON', '{oops'], ['null', 'null']])('starts with none when the store holds %s', async (_name, raw) => {
    localStorage.setItem(KEY, raw)
    const store = await availableStore()
    expect(store.forChannel('a')).toBe(0)
  })

  it('takes a choice off with 0 and keeps the 200 most recent', async () => {
    const store = await availableStore()
    store.choose('a', 3600)
    store.choose('a', 0)
    expect(store.forChannel('a')).toBe(0)
    for (let n = 0; n < 201; n++) store.choose(`c${n}`, 60)
    expect(store.forChannel('c0')).toBe(0)
    expect(store.forChannel('c1')).toBe(60)
    expect(Object.keys(JSON.parse(localStorage.getItem(KEY) ?? '{}'))).toHaveLength(200)
  })

  it('keeps a choice for this page when the storage refuses it', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('QuotaExceededError') })
    const store = await availableStore()
    store.choose('a', 3600)
    expect(store.forChannel('a')).toBe(3600)
  })
})

describe('what the community allows', () => {
  it('offers nothing while the server does not know lifetimes, without asking it', async () => {
    const store = useLifetimeStore()
    await store.load()
    expect(store.available).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('offers nothing when the settings cannot be read', async () => {
    answer = async () => new Response('{}', { status: 500 })
    const store = await availableStore()
    expect(store.available).toBe(false)
  })

  it('keeps the answer of the latest load when an older one arrives late', async () => {
    useServerFeaturesStore().list = ['messages.lifetime']
    const store = useLifetimeStore()
    let releaseFirst!: (r: Response) => void
    answer = () => new Promise<Response>(resolve => { releaseFirst = resolve })
    const first = store.load()
    answer = async () => settings(true, 3600)
    await store.load()
    releaseFirst(settings(false))
    await first
    expect(store.available).toBe(true)
    expect(store.maxSeconds).toBe(3600)
  })

  it('lets an older load that fails late change nothing', async () => {
    useServerFeaturesStore().list = ['messages.lifetime']
    const store = useLifetimeStore()
    let failFirst!: (e: Error) => void
    answer = () => new Promise<Response>((_resolve, reject) => { failFirst = reject })
    const first = store.load()
    answer = async () => settings(true)
    await store.load()
    failFirst(new TypeError('network down'))
    await first
    expect(store.available).toBe(true)
  })

  it('counts a choice beyond a lowered ceiling as none', async () => {
    localStorage.setItem(KEY, JSON.stringify({ a: 86400 }))
    answer = async () => settings(true, 3600)
    const store = await availableStore()
    expect(store.forChannel('a')).toBe(0)
  })
})
