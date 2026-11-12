import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { FAILURE_RECHECK_GAP_MS, HEALTH_TIMEOUT_MS, OUTAGE_RECHECK_MS, useServerFeaturesStore } from './serverFeatures'
import StorageBanner from '../app/components/StorageBanner.vue'
import { setLocale } from '../i18n'
import { api } from '../shared/lib/api'

const about = { version: '3.0.3', web_client_api: 1, community_name: 'Harbour', features: ['dm', 'messages.nonce'] }
const healthy = { status: 'ok', ...about }
const down = (...unavailable: string[]) => ({
  error: { code: 'UNAVAILABLE', message: unavailable.includes('database') ? 'database unavailable' : 'file storage unavailable' },
  status: 'unavailable',
  unavailable,
  ...about
})

/** A request the server never answers: it ends only when the client gives it up. */
const NO_ANSWER = 0

/** The server answers /api/health with these, in order; the last one repeats. */
function serverAnswers(...answers: Array<[status: number, body: unknown]>) {
  let asked = 0
  const fetchMock = vi.fn<typeof fetch>((_url, init) => {
    const [status, body] = answers[Math.min(asked++, answers.length - 1)] ?? [500, null]
    if (status === NO_ANSWER) {
      return new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')))
      })
    }
    return Promise.resolve(new Response(JSON.stringify(body), { status }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.useFakeTimers()
  // Node's own timer is not the faked one: the limit follows the test clock instead.
  vi.spyOn(AbortSignal, 'timeout').mockImplementation(ms => {
    const limit = new AbortController()
    setTimeout(() => limit.abort(new DOMException('timeout', 'TimeoutError')), ms)
    return limit.signal
  })
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('starting while object storage is down', () => {
  it('knows the features and the community from the 503 and says that storage is down', async () => {
    serverAnswers([503, down('storage')])
    const features = useServerFeaturesStore()
    const banner = mount(StorageBanner)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(false)
    await features.load()
    expect(features.loaded).toBe(true)
    expect(features.dm).toBe(true)
    expect(features.messageNonce).toBe(true)
    expect(features.communityName).toBe('Harbour')
    expect(features.storageDown).toBe(true)
    await nextTick()
    const notice = banner.get('[data-testid="storage-banner"]')
    expect(notice.attributes('role')).toBe('status')
    expect(notice.text()).toBe('File storage is unavailable – uploads and pictures do not work right now. Chat and Talks keep working.')
  })

  it('asks again while the outage lasts and takes the notice away when storage is back', async () => {
    const fetchMock = serverAnswers([503, down('storage')], [503, down('storage')], [200, healthy])
    const features = useServerFeaturesStore()
    const banner = mount(StorageBanner)
    await features.load()
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features.storageDown).toBe(true)
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(features.storageDown).toBe(false)
    expect(features.dm).toBe(true)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(false)
    // Healthy again: nothing is asked any more.
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS * 10)
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('keeps asking when a check during the outage gets no answer', async () => {
    const fetchMock = serverAnswers([503, down('storage')], [502, null], [200, healthy])
    const features = useServerFeaturesStore()
    await features.load()
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(features.storageDown).toBe(true)
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(features.storageDown).toBe(false)
  })

  it('shows no storage notice when the database is what is down', async () => {
    serverAnswers([503, down('database')])
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.loaded).toBe(true)
    expect(features.unavailable).toEqual(['database'])
    expect(features.storageDown).toBe(false)
  })

  it('reads the answer of a later server that names a dependency this app does not know', async () => {
    serverAnswers([503, down('storage', 'search')])
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.loaded).toBe(true)
    expect(features.dm).toBe(true)
    expect(features.communityName).toBe('Harbour')
    expect(features.unavailable).toEqual(['storage', 'search'])
    expect(features.storageDown).toBe(true)
  })

  it('confirms nothing from the bare error an older server sends', async () => {
    serverAnswers([503, { error: { code: 'UNAVAILABLE', message: 'file storage unavailable' } }])
    const features = useServerFeaturesStore()
    await features.load()
    expect(features.loaded).toBe(false)
    expect(features.storageDown).toBe(false)
    expect(features.dm).toBe(false)
  })
})

describe('storage that goes down after a healthy start', () => {
  /** Health answers in order; every other path is an upload that storage refuses. */
  function uploadsFailWith503(...answers: Array<[status: number, body: unknown]>) {
    let asked = 0
    const fetchMock = vi.fn<typeof fetch>(url => {
      if (url === '/api/health') {
        const [status, body] = answers[Math.min(asked++, answers.length - 1)] ?? [500, null]
        return Promise.resolve(new Response(JSON.stringify(body), { status }))
      }
      return Promise.resolve(new Response(JSON.stringify(down('storage')), { status: 503 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    return () => fetchMock.mock.calls.filter(([url]) => url === '/api/health').length
  }

  it('is not polled while healthy', async () => {
    const fetchMock = serverAnswers([200, healthy], [503, down('storage')])
    const features = useServerFeaturesStore()
    await features.load()
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS * 20)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(features.storageDown).toBe(false)
  })

  it('is noticed when an upload fails with 503, and the notice goes when storage is back', async () => {
    const healthChecks = uploadsFailWith503([200, healthy], [503, down('storage')], [200, healthy])
    const features = useServerFeaturesStore()
    const banner = mount(StorageBanner)
    await features.load()
    await expect(api('/api/channels/1/upload', { method: 'POST' })).rejects.toMatchObject({ status: 503 })
    await vi.advanceTimersByTimeAsync(0)
    await nextTick()
    expect(healthChecks()).toBe(2)
    expect(features.storageDown).toBe(true)
    expect(features.dm).toBe(true)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    await nextTick()
    expect(features.storageDown).toBe(false)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(false)
  })

  it('does not ask once per failed upload', async () => {
    const healthChecks = uploadsFailWith503([200, healthy], [200, healthy])
    const features = useServerFeaturesStore()
    await features.load()
    for (let i = 0; i < 5; i++) await expect(api('/api/channels/1/upload', { method: 'POST' })).rejects.toBeDefined()
    await vi.advanceTimersByTimeAsync(0)
    expect(healthChecks()).toBe(2)
    await vi.advanceTimersByTimeAsync(FAILURE_RECHECK_GAP_MS)
    await expect(api('/api/channels/1/upload', { method: 'POST' })).rejects.toBeDefined()
    await vi.advanceTimersByTimeAsync(0)
    expect(healthChecks()).toBe(3)
  })

  it('is noticed at once when the socket reopens (refresh), and a request on its way is joined', async () => {
    const fetchMock = serverAnswers([200, healthy], [503, down('storage')])
    const features = useServerFeaturesStore()
    await features.load()
    await Promise.all([features.refresh(), features.refresh()])
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features.storageDown).toBe(true)
  })

  it('keeps what it knows when a check during the outage fails', async () => {
    serverAnswers([503, down('storage')], [502, null], [200, healthy])
    const features = useServerFeaturesStore()
    await features.load()
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(features.storageDown).toBe(true)
    expect(features.dm).toBe(true)
  })
})

describe('a check the server never answers', () => {
  it('is given up after the timeout, so it cannot hang the start-up, and asked again', async () => {
    const fetchMock = serverAnswers([NO_ANSWER, null], [200, healthy])
    const features = useServerFeaturesStore()
    const first = features.load()
    await vi.advanceTimersByTimeAsync(HEALTH_TIMEOUT_MS - 1)
    expect(features.loaded).toBe(false)
    await vi.advanceTimersByTimeAsync(1)
    await first
    expect(features.loaded).toBe(false)
    await features.load()
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features.loaded).toBe(true)
  })

  it('does not stay waiting during an outage either', async () => {
    const fetchMock = serverAnswers([503, down('storage')], [NO_ANSWER, null], [200, healthy])
    const features = useServerFeaturesStore()
    await features.load()
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS + HEALTH_TIMEOUT_MS)
    expect(features.storageDown).toBe(true)
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(features.storageDown).toBe(false)
  })
})
