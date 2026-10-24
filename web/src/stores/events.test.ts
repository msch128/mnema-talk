import type { ApiOptions } from '../lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useEventsStore } from './events'
import { useServerFeaturesStore } from './serverFeatures'
import { useToastStore } from './toast'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent, RSVPItem } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Community Game Night',
    description: 'Come play games with us!',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 1, interested: 0, not_going: 0 },
    ...overrides
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  useServerFeaturesStore().list = ['events']
})

describe('events store', () => {
  it('does not load when events feature is disabled', async () => {
    useServerFeaturesStore().list = []
    const store = useEventsStore()
    await store.load()
    expect(apiMock).not.toHaveBeenCalled()
    expect(store.events).toEqual([])
  })

  it('loads events and separates upcoming and past events', async () => {
    const ev1 = makeEvent({ id: fixtureId(1), start_time: '2026-11-01T18:00:00Z', status: 'scheduled' })
    const ev2 = makeEvent({ id: fixtureId(2), start_time: '2026-10-01T18:00:00Z', status: 'completed' })
    const ev3 = makeEvent({ id: fixtureId(3), start_time: '2026-11-02T18:00:00Z', status: 'active' })
    apiMock.mockResolvedValueOnce([ev1, ev2, ev3])

    const store = useEventsStore()
    await store.load()

    expect(apiMock).toHaveBeenCalledWith('/api/events', expect.anything())
    expect(store.events).toHaveLength(3)
    expect(store.upcomingCount).toBe(2)
    expect(store.upcomingEvents.map(e => e.id)).toEqual([fixtureId(1), fixtureId(3)])
    expect(store.pastEvents.map(e => e.id)).toEqual([fixtureId(2)])
  })

  it('handles load errors gracefully', async () => {
    apiMock.mockRejectedValueOnce(new Error('Network failure'))
    const store = useEventsStore()
    await store.load()
    expect(store.error).toBe('Network failure')
    expect(store.loading).toBe(false)
  })

  it('creates an event and updates local store', async () => {
    const created = makeEvent({ id: fixtureId(5), title: 'New Event' })
    apiMock.mockResolvedValueOnce(created)

    const store = useEventsStore()
    const result = await store.create({
      title: 'New Event',
      description: 'Desc',
      start_time: '2026-11-01T18:00:00Z'
    })

    expect(result.id).toBe(fixtureId(5))
    expect(store.events).toContainEqual(created)
  })

  it('updates an event in place', async () => {
    const initial = makeEvent({ id: fixtureId(1), title: 'Old Title' })
    const updated = makeEvent({ id: fixtureId(1), title: 'New Title' })
    const store = useEventsStore()
    store.events = [initial]

    apiMock.mockResolvedValueOnce(updated)
    const result = await store.update(fixtureId(1), { title: 'New Title' })

    expect(result.title).toBe('New Title')
    expect(store.events[0]?.title).toBe('New Title')
  })

  it('cancels an event in place', async () => {
    const initial = makeEvent({ id: fixtureId(1), status: 'scheduled' })
    const cancelled = makeEvent({ id: fixtureId(1), status: 'cancelled' })
    const store = useEventsStore()
    store.events = [initial]

    apiMock.mockResolvedValueOnce(cancelled)
    await store.cancel(fixtureId(1))

    expect(store.events[0]?.status).toBe('cancelled')
  })

  it('sets and deletes RSVP', async () => {
    const initial = makeEvent({ id: fixtureId(1), my_rsvp: null })
    const withRsvp = makeEvent({ id: fixtureId(1), my_rsvp: 'going' })
    const store = useEventsStore()
    store.events = [initial]

    apiMock.mockResolvedValueOnce(withRsvp)
    await store.setRSVP(fixtureId(1), 'going')
    expect(store.events[0]?.my_rsvp).toBe('going')

    apiMock.mockResolvedValueOnce(initial)
    await store.deleteRSVP(fixtureId(1))
    expect(store.events[0]?.my_rsvp).toBeNull()
  })

  it('uploads and deletes cover image', async () => {
    const withCover = makeEvent({ id: fixtureId(1), cover_url: '/api/media/cover.png' })
    const withoutCover = makeEvent({ id: fixtureId(1), cover_url: null })
    const store = useEventsStore()
    store.events = [withoutCover]

    apiMock.mockResolvedValueOnce(withCover)
    const file = new File(['image-bytes'], 'cover.png', { type: 'image/png' })
    await store.uploadCover(fixtureId(1), file)
    expect(store.events[0]?.cover_url).toBe('/api/media/cover.png')

    apiMock.mockResolvedValueOnce(withoutCover)
    await store.deleteCover(fixtureId(1))
    expect(store.events[0]?.cover_url).toBeNull()
  })

  it('lists RSVPs for an event', async () => {
    const rsvpItem: RSVPItem = {
      user: { id: fixtureId(10), username: 'alice', display_name: 'Alice', avatar_url: null },
      response: 'going',
      updated_at: FIXTURE_TIMESTAMP
    }
    apiMock.mockResolvedValueOnce([rsvpItem])
    const store = useEventsStore()
    const list = await store.listRSVPs(fixtureId(1))
    expect(list).toEqual([rsvpItem])
  })

  it('handles event_update and event_reminder events', () => {
    const store = useEventsStore()
    const ev = makeEvent({ id: fixtureId(20), title: 'Live Update' })
    store.handleEventUpdate(ev)
    expect(store.events).toContainEqual(ev)

    const toast = useToastStore()
    const toastSpy = vi.spyOn(toast, 'info')
    store.handleEventReminder({
      event_id: fixtureId(20),
      title: 'Live Update',
      start_time: FIXTURE_TIMESTAMP
    })
    expect(toastSpy).toHaveBeenCalled()
  })

  it('drops an event on event_delete', () => {
    const store = useEventsStore()
    const keep = makeEvent({ id: fixtureId(21) })
    const gone = makeEvent({ id: fixtureId(22) })
    store.events = [keep, gone]
    store.handleEventDelete(gone.id)
    expect(store.events).toEqual([keep])
  })

  it('resets store state', () => {
    const store = useEventsStore()
    store.events = [makeEvent()]
    store.loading = true
    store.error = 'error'

    store.reset()
    expect(store.events).toEqual([])
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })
})
