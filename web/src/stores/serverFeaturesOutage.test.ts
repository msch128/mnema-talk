import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { HEALTHY_RECHECK_MS, HEALTH_TIMEOUT_MS, OUTAGE_RECHECK_MS, useServerFeaturesStore } from './serverFeatures'
import StorageBanner from '../app/components/StorageBanner.vue'
import { setLocale } from '../i18n'

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
})
afterEach(() => {
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
    // Healthy again: only the slow check runs now, not the one of an outage.
    await vi.advanceTimersByTimeAsync(HEALTHY_RECHECK_MS - 1)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(fetchMock).toHaveBeenCalledTimes(4)
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
  it('is noticed by the slow check, and the notice goes when storage is back', async () => {
    const fetchMock = serverAnswers([200, healthy], [503, down('storage')], [200, healthy])
    const features = useServerFeaturesStore()
    const banner = mount(StorageBanner)
    await features.load()
    expect(features.storageDown).toBe(false)
    await vi.advanceTimersByTimeAsync(HEALTHY_RECHECK_MS - 1)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await nextTick()
    expect(features.storageDown).toBe(true)
    expect(features.dm).toBe(true)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(OUTAGE_RECHECK_MS)
    await nextTick()
    expect(features.storageDown).toBe(false)
    expect(banner.find('[data-testid="storage-banner"]').exists()).toBe(false)
  })

  it('is noticed at once when the socket reopens (refresh), and a request on its way is joined', async () => {
    const fetchMock = serverAnswers([200, healthy], [503, down('storage')])
    const features = useServerFeaturesStore()
    await features.load()
    await Promise.all([features.refresh(), features.refresh()])
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features.storageDown).toBe(true)
  })

  it('asks nothing in a tab that is hidden, and asks again once it is shown', async () => {
    const fetchMock = serverAnswers([200, healthy], [503, down('storage')])
    const features = useServerFeaturesStore()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    await features.load()
    await vi.advanceTimersByTimeAsync(HEALTHY_RECHECK_MS * 5)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue(false)
    await vi.advanceTimersByTimeAsync(HEALTHY_RECHECK_MS)
    expect(features.storageDown).toBe(true)
    hidden.mockRestore()
  })

  it('keeps what it knows when a check fails, and checks again later', async () => {
    const fetchMock = serverAnswers([200, healthy], [502, null], [200, healthy])
    const features = useServerFeaturesStore()
    await features.load()
    await vi.advanceTimersByTimeAsync(HEALTHY_RECHECK_MS * 2)
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(features.loaded).toBe(true)
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
