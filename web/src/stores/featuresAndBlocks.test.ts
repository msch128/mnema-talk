import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useServerFeaturesStore } from './serverFeatures'
import { useBlocksStore } from './blocks'
import { useToastStore } from './toast'
import { fixtureId } from '../test-fixtures.fixture'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import { setLocale } from '../i18n'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))

const ada = fixtureId(2)
const blockedAda = { user_id: ada, username: 'ada', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => []
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

describe('the direct message flags of the server features', () => {
  it('read the list of the health answer, and a 1.0 server (no list) has none', async () => {
    http.handle = () => ({ status: 'ok', version: '1.1.0', web_client_api: 1, features: ['dm', 'blocks'] })
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.dm).toBe(true)
    expect(features.blocks).toBe(true)
    expect(features.reports).toBe(false)
    setActivePinia(createPinia())
    http.handle = () => ({ status: 'ok', version: '1.0.1', web_client_api: 1 })
    const old = useServerFeaturesStore()
    await old.load()
    expect(old.dm || old.blocks || old.reports).toBe(false)
  })
})

describe('blocks', () => {
  function setup() {
    useServerFeaturesStore().list = ['blocks']
    return useBlocksStore()
  }

  it('does nothing on a server without blocking', async () => {
    const blocks = useBlocksStore()
    http.handle = () => [blockedAda]
    await blocks.load()
    expect(blocks.blocked).toEqual([])
    expect(blocks.isBlocked(ada)).toBe(false)
  })

  it('folds the messages of a blocked member until the reader shows one', async () => {
    const blocks = setup()
    http.handle = () => [blockedAda]
    await blocks.load()
    const message = { id: fixtureId(9), user_id: ada }
    expect(blocks.hides(message)).toBe(true)
    expect(blocks.hides({ id: fixtureId(10), user_id: fixtureId(3) })).toBe(false)
    blocks.reveal(message.id)
    expect(blocks.hides(message)).toBe(false)
    expect(blocks.hides({ id: fixtureId(11), user_id: ada })).toBe(true)
  })

  it('blocks and unblocks through the member route, then reads the list', async () => {
    const blocks = setup()
    const seen: string[] = []
    http.handle = (url, options) => {
      seen.push(`${options?.method ?? 'GET'} ${url}`)
      return url === '/api/me/blocks' ? [blockedAda] : undefined
    }
    expect(await blocks.block(ada)).toBe(true)
    expect(seen).toEqual([`PUT /api/me/blocks/${ada}`, 'GET /api/me/blocks'])
    expect(blocks.isBlocked(ada)).toBe(true)
    blocks.reveal(fixtureId(9))
    http.handle = (url) => (url === '/api/me/blocks' ? [] : undefined)
    expect(await blocks.unblock(ada)).toBe(true)
    expect(blocks.isBlocked(ada)).toBe(false)
    expect(blocks.hides({ id: fixtureId(9), user_id: ada })).toBe(false)
  })

  it('says so when the list is full or a request fails', async () => {
    const blocks = setup()
    const toasts = useToastStore()
    http.handle = () => { throw new ApiError(409, 'CONFLICT', 'full') }
    expect(await blocks.block(ada)).toBe(false)
    http.handle = () => { throw new Error('offline') }
    expect(await blocks.unblock(ada)).toBe(false)
    expect(toasts.toasts.map(t => t.text)).toEqual([
      'Your block list is full. Unblock someone first.',
      'Couldn’t unblock the member. Try again.'
    ])
  })

  it('keeps the newest answer when two loads overlap and clears on reset', async () => {
    const blocks = setup()
    let release: (value: unknown) => void = () => {}
    http.handle = () => new Promise(resolve => { release = resolve }) as never
    const slow = blocks.load()
    http.handle = () => []
    await blocks.load()
    release([blockedAda])
    await slow
    expect(blocks.blocked).toEqual([])
    blocks.blocked = [blockedAda]
    blocks.reset()
    expect(blocks.blocked).toEqual([])
  })
})
