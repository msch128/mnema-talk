import type { ApiOptions } from '../lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { useAccountStore } from './account'
import { useForwardQuotesStore } from './forwardQuotes'
import { useEventsStore } from './events'
import { useLaterStore } from './later'
import { useOnboardingStore } from './onboarding'
import { useAuthStore } from './auth'
import { useServerFeaturesStore } from './serverFeatures'
import { fixtureId, FIXTURE_TIMESTAMP, userFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

/** A request the test settles by hand. */
function deferred() {
  let resolve: (value: unknown) => void = () => undefined
  let reject: (reason: unknown) => void = () => undefined
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const event = (id: number) => ({
  id: fixtureId(id), title: 't', description: '', start_time: '2026-11-01T18:00:00Z', status: 'scheduled',
  created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP, rsvp_counts: { going: 0, interested: 0, not_going: 0 }
})

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  localStorage.clear()
  useServerFeaturesStore().list = ['events']
})

describe('account store', () => {
  const info = { email: 'a@example.org', email_confirmed: true, totp_enabled: false }

  it('drops an answer that arrives after the dialog closed', async () => {
    const account = useAccountStore()
    const reply = deferred()
    apiMock.mockReturnValueOnce(reply.promise)
    account.open()
    const loading = account.load()
    account.close()
    reply.resolve(info)
    await loading
    expect(account.info).toBeNull()
  })

  it('still applies an answer while the dialog stays open', async () => {
    const account = useAccountStore()
    apiMock.mockResolvedValueOnce(info)
    account.open()
    await account.load()
    expect(account.info?.email).toBe('a@example.org')
  })
})

describe('forward quotes store', () => {
  it('keeps the newer request pending when an invalidated one finishes', async () => {
    const quotes = useForwardQuotesStore()
    const first = deferred()
    const second = deferred()
    apiMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    void quotes.ensure('a')
    quotes.invalidate()
    void quotes.ensure('a')
    first.reject(new Error('stale'))
    await flushPromises()
    void quotes.ensure('a')
    expect(apiMock).toHaveBeenCalledTimes(2)
  })
})

describe('events store', () => {
  it('applies only the latest of overlapping loads', async () => {
    const events = useEventsStore()
    const slow = deferred()
    const fast = deferred()
    apiMock.mockReturnValueOnce(slow.promise).mockReturnValueOnce(fast.promise)
    const older = events.load()
    const newer = events.load()
    fast.resolve([event(2)])
    await newer
    slow.resolve([event(1)])
    await older
    expect(events.events.map(e => e.id)).toEqual([fixtureId(2)])
    expect(events.loading).toBe(false)
  })

  it('drops a load that answers after reset', async () => {
    const events = useEventsStore()
    const reply = deferred()
    apiMock.mockReturnValueOnce(reply.promise)
    const loading = events.load()
    events.reset()
    reply.resolve([event(1)])
    await loading
    expect(events.events).toEqual([])
  })

  it('drops a past page that answers after reset and pages on from the last fetched event even after it was deleted', async () => {
    const events = useEventsStore()
    const page = Array.from({ length: 30 }, (_, i) => ({ ...event(100 + i), status: 'completed', start_time: `2026-09-${String(30 - i).padStart(2, '0')}T18:00:00Z` }))
    apiMock.mockResolvedValueOnce(page)
    await events.loadPast()
    events.handleEventDelete(fixtureId(129))
    apiMock.mockResolvedValueOnce([page[28]])
    await events.loadPast(true)
    expect(String(apiMock.mock.calls[1]?.[0])).toContain(`before_id=${fixtureId(129)}`)
    expect(events.pastEvents.filter(e => e.id === fixtureId(128))).toHaveLength(1)

    const reply = deferred()
    apiMock.mockReturnValueOnce(reply.promise)
    const late = events.loadPast(true)
    events.reset()
    reply.resolve([event(7)])
    await late
    expect(events.pastEvents).toEqual([])
    expect(events.pastLoading).toBe(false)
  })

  it('ignores a past page that fails after reset', async () => {
    const events = useEventsStore()
    const reply = deferred()
    apiMock.mockReturnValueOnce(reply.promise)
    const late = events.loadPast()
    events.reset()
    reply.reject(new Error('late'))
    await late
    expect(events.error).toBeNull()
    expect(events.pastLoaded).toBe(false)
  })

  it('starts at the top without a cursor and ends at an empty further page', async () => {
    const events = useEventsStore()
    apiMock.mockResolvedValueOnce([])
    await events.loadPast(true)
    expect(String(apiMock.mock.calls[0]?.[0])).not.toContain('before_id')

    const page = Array.from({ length: 30 }, (_, i) => ({ ...event(200 + i), status: 'completed', start_time: `2026-08-${String(30 - i).padStart(2, '0')}T18:00:00Z` }))
    apiMock.mockResolvedValueOnce(page)
    await events.loadPast()
    apiMock.mockResolvedValueOnce([])
    await events.loadPast(true)
    expect(String(apiMock.mock.calls[2]?.[0])).toContain(`before_id=${fixtureId(229)}`)
    expect(events.pastHasMore).toBe(false)
  })
})

describe('later store', () => {
  it('drops lists that arrive after reset', async () => {
    const later = useLaterStore()
    const reply = deferred()
    apiMock.mockReturnValueOnce(reply.promise)
    const loading = later.load({ reminders: true, scheduled: false })
    later.reset()
    reply.resolve([{
      id: fixtureId(1), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true,
      message: { id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: 'g', dm: false, author: 'A', snippet: 's' }
    }])
    await loading
    expect(later.reminders).toEqual([])
    expect(later.loaded).toBe(false)
    expect(later.loading).toBe(false)
  })
})

describe('onboarding store', () => {
  const record = (tips: string[]) => ({
    community_name: '', welcome_text: '', rules_text: '', rules_version: 0, rules_accepted_version: 0,
    rules_pending: false, start_channel_id: null, dismissed_tips: tips, welcome_done: true
  })

  it('does not lend the next account the previous record when its load fails', async () => {
    const auth = useAuthStore()
    const onboarding = useOnboardingStore()
    auth.user = userFixture({ id: fixtureId(1) })
    apiMock.mockResolvedValueOnce(record(['search']))
    await onboarding.ensureLoaded()
    expect(onboarding.state?.dismissed_tips).toEqual(['search'])
    onboarding.reset() // what signing out does
    auth.user = userFixture({ id: fixtureId(2) })
    apiMock.mockRejectedValue(new Error('offline'))
    vi.useFakeTimers()
    const loading = onboarding.ensureLoaded()
    await vi.runAllTimersAsync()
    await loading
    vi.useRealTimers()
    expect(onboarding.state).toBeNull()
  })

  it('forgets the record on reset', async () => {
    const auth = useAuthStore()
    const onboarding = useOnboardingStore()
    auth.user = userFixture({ id: fixtureId(1) })
    apiMock.mockResolvedValueOnce(record(['search']))
    await onboarding.ensureLoaded()
    onboarding.reset()
    expect(onboarding.state).toBeNull()
    apiMock.mockResolvedValueOnce(record([]))
    await onboarding.ensureLoaded()
    expect(onboarding.state?.dismissed_tips).toEqual([])
  })

  it('keeps a tip closed when the answer to an earlier save arrives late', async () => {
    const auth = useAuthStore()
    const onboarding = useOnboardingStore()
    auth.user = userFixture({ id: fixtureId(1) })
    apiMock.mockResolvedValueOnce(record([]))
    await onboarding.ensureLoaded()
    const first = deferred()
    const second = deferred()
    apiMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const a = onboarding.dismiss('search')
    const b = onboarding.dismiss('mention')
    first.resolve(record(['search']))
    await a
    expect(onboarding.state?.dismissed_tips).toContain('mention')
    second.resolve(record(['search', 'mention']))
    await b
    expect(onboarding.state?.dismissed_tips).toEqual(['search', 'mention'])
  })
})
