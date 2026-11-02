import type { ApiOptions } from '../lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import * as rolesApi from '../lib/eventRolesApi'
import { minutesUntil, useEventsStore } from './events'
import { eventsRequest } from '../lib/eventsDialog'
import { useServerFeaturesStore } from './serverFeatures'
import { useToastStore } from './toast'
import { t } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent, RSVPItem } from '../types/domain'

vi.mock('../lib/eventRolesApi', () => ({
  fetchEvent: vi.fn(), setEventRoles: vi.fn(), deleteEventRole: vi.fn(), signUpForRole: vi.fn(), leaveRole: vi.fn(), removeSignup: vi.fn()
}))

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

  it('keeps this client\'s own RSVP when another member\'s answer is pushed', () => {
    const store = useEventsStore()
    const id = fixtureId(21)
    store.handleEventUpdate(makeEvent({ id, title: 'Quiz' }))
    store.events[0] = makeEvent({ id, title: 'Quiz', my_rsvp: 'not_going' })
    store.handleEventUpdate(makeEvent({ id, title: 'Quiz', rsvp_counts: { going: 3, interested: 0, not_going: 1 } }))
    expect(store.events[0]?.my_rsvp).toBe('not_going')
    expect(store.events[0]?.rsvp_counts.going).toBe(3)
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

  it('names the event and the minutes left in the reminder, and opens the event from it', () => {
    const store = useEventsStore()
    const now = Date.now()
    store.handleEventReminder({ event_id: fixtureId(20), title: 'Game night', start_time: new Date(now + 15 * 60_000).toISOString() })
    const toast = useToastStore().toasts.at(-1)
    expect(toast?.text).toMatch(/Game night.*(15 minutes|15 Minuten)/)
    void toast?.action?.onClick()
    expect(eventsRequest.value).toMatchObject({ eventId: fixtureId(20) })
  })

  it('counts at least one minute, and falls back to the usual lead for an unreadable time', () => {
    expect(minutesUntil('2026-01-01T00:00:00Z', Date.parse('2026-01-01T00:00:10Z'))).toBe(1)
    expect(minutesUntil('2026-01-01T00:14:40Z', Date.parse('2026-01-01T00:00:00Z'))).toBe(15)
    expect(minutesUntil('soon')).toBe(15)
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

describe('past events', () => {
  it('pages past events with a cursor and merges them with the loaded ones', async () => {
    const page = Array.from({ length: 30 }, (_, i) => makeEvent({ id: fixtureId(100 + i), start_time: `2026-09-${String(30 - i).padStart(2, '0')}T18:00:00Z`, status: 'completed' }))
    const rest = [makeEvent({ id: fixtureId(200), start_time: '2026-08-01T18:00:00Z', status: 'cancelled' })]
    apiMock.mockResolvedValueOnce(page).mockResolvedValueOnce(rest)
    const store = useEventsStore()

    await store.loadPast()
    expect(apiMock.mock.calls[0]?.[0]).toBe('/api/events?when=past&limit=30')
    expect(store.pastHasMore).toBe(true)
    expect(store.pastLoaded).toBe(true)

    await store.loadPast(true)
    const url = String(apiMock.mock.calls[1]?.[0])
    expect(url).toContain(`before_id=${page[29]?.id}`)
    expect(url).toContain('before=2026-09-01T18%3A00%3A00Z')
    expect(store.pastHasMore).toBe(false)
    expect(store.pastEvents).toHaveLength(31)
    expect(store.pastEvents[0]?.id).toBe(page[0]?.id)
    expect(store.pastEvents.at(-1)?.id).toBe(fixtureId(200))

    await store.loadPast(true)
    expect(apiMock).toHaveBeenCalledTimes(2)
  })

  it('does not list an event twice and applies live updates and deletes to fetched pages', async () => {
    const old = makeEvent({ id: fixtureId(5), status: 'completed', title: 'Old' })
    apiMock.mockResolvedValueOnce([old])
    const store = useEventsStore()
    await store.loadPast()

    store.handleEventUpdate({ ...old, title: 'Renamed' })
    expect(store.pastEvents.map(e => e.title)).toEqual(['Renamed'])
    store.handleEventDelete(old.id)
    expect(store.pastEvents).toEqual([])
  })

  it('keeps the error and allows a retry when a page fails', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const store = useEventsStore()
    await store.loadPast()
    expect(store.error).toBe('offline')
    expect(store.pastLoading).toBe(false)
    expect(store.pastLoaded).toBe(false)
  })

  it('names a failure that carries no message', async () => {
    apiMock.mockRejectedValueOnce('boom')
    const store = useEventsStore()
    await store.loadPast()
    expect(store.error).toBe(t('events.loadFailed'))
  })

  it('does nothing while the feature is off', async () => {
    useServerFeaturesStore().list = []
    await useEventsStore().loadPast()
    expect(apiMock).not.toHaveBeenCalled()
  })

  describe('roles and sign-ups', () => {
    const roleId = fixtureId(30)

    it('keeps the members own sign-up and the sign-ups when a broadcast leaves them out', () => {
      const store = useEventsStore()
      const mine = { role_id: roleId, state: 'waitlist' as const, waitlist_position: 2 }
      const signups = [{ user: { id: fixtureId(2), username: 'a', display_name: 'a' }, role_id: roleId, state: 'waitlist' as const, created_at: FIXTURE_TIMESTAMP }]
      store.events = [makeEvent({ id: fixtureId(20), my_signup: mine, signups })]
      store.handleEventUpdate(makeEvent({ id: fixtureId(20), title: 'Moved' }))
      expect(store.events[0]).toMatchObject({ title: 'Moved', my_signup: mine, signups })
    })

    it('tells the member that they moved up, or were removed, and reads the event again', async () => {
      const store = useEventsStore()
      const read = vi.mocked(rolesApi.fetchEvent).mockResolvedValue(makeEvent({ id: fixtureId(20), title: 'Fresh' }))
      store.handleSignupNotice({ event_id: fixtureId(20), title: 'Raid', kind: 'promoted', role_name: 'Tank', start_time: FIXTURE_TIMESTAMP })
      store.handleSignupNotice({ event_id: fixtureId(20), title: 'Raid', kind: 'removed', role_name: 'DPS', start_time: FIXTURE_TIMESTAMP })
      await flushPromises()
      const texts = useToastStore().toasts.map(t => t.text)
      expect(texts[0]).toMatch(/Tank.*Raid|Raid.*Tank/)
      expect(texts[1]).toMatch(/DPS/)
      expect(read).toHaveBeenCalledTimes(2)
      expect(store.events[0]?.title).toBe('Fresh')
    })

    it('does not fail when the event cannot be read after a notice', async () => {
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
      vi.mocked(rolesApi.fetchEvent).mockRejectedValue(new Error('down'))
      useEventsStore().handleSignupNotice({ event_id: fixtureId(20), title: 'Raid', kind: 'promoted', role_name: 'Tank', start_time: FIXTURE_TIMESTAMP })
      await flushPromises()
      expect(warn).toHaveBeenCalled()
      warn.mockRestore()
    })

    it('puts a waitlisted notice into words', async () => {
      vi.mocked(rolesApi.fetchEvent).mockResolvedValue(makeEvent({ id: fixtureId(20) }))
      useEventsStore().handleSignupNotice({ event_id: fixtureId(20), title: 'Raid', kind: 'waitlisted', role_name: 'DPS', start_time: FIXTURE_TIMESTAMP })
      await flushPromises()
      expect(useToastStore().toasts[0]?.text).toMatch(/Warteliste.*DPS/)
    })

    it('drops the event when leaving it answers that it can no longer be viewed', async () => {
      const store = useEventsStore()
      store.events = [makeEvent({ id: fixtureId(20) }), makeEvent({ id: fixtureId(21) })]
      vi.mocked(rolesApi.leaveRole).mockResolvedValue(null)
      expect(await store.leaveRole(fixtureId(20))).toBeNull()
      expect(store.events.map(e => e.id)).toEqual([fixtureId(21)])
    })

    it('stores what each roster action answers', async () => {
      const store = useEventsStore()
      const answer = makeEvent({ id: fixtureId(20), title: 'After' })
      for (const [name, spy] of [
        ['setEventRoles', () => store.setRoles(fixtureId(20), [{ name: 'Tank' }])],
        ['deleteEventRole', () => store.deleteRole(fixtureId(20), roleId, fixtureId(31))],
        ['signUpForRole', () => store.signUp(fixtureId(20), roleId)],
        ['leaveRole', () => store.leaveRole(fixtureId(20))],
        ['removeSignup', () => store.removeSignup(fixtureId(20), fixtureId(2))],
        ['fetchEvent', () => store.fetchDetail(fixtureId(20))]
      ] as const) {
        vi.mocked(rolesApi[name]).mockResolvedValue(answer)
        store.events = []
        await spy()
        expect(store.events[0]?.title).toBe('After')
      }
    })
  })
})
