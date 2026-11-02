import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventDetailModal from './EventDetailModal.vue'
import { useEventsStore } from '../stores/events'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useAuthStore } from '../stores/auth'
import { pendingConfirm } from '../lib/confirm'
import { setLocale } from '../i18n'
import { categoryFixture, channelFixture, fixtureId, FIXTURE_TIMESTAMP, userFixture } from '../test-fixtures.fixture'
import type { CommunityEvent, PermissionsInfo, RSVPItem } from '../types/domain'

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve([]))
}))

function makePermsInfo(permissions: number): PermissionsInfo {
  return { permissions, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
}

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Game Night',
    description: 'Playing **games**',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 2, interested: 1, not_going: 0 },
    ...overrides
  }
}

function rsvp(n: number, response: RSVPItem['response'], name = `user${n}`): RSVPItem {
  return {
    user: { id: fixtureId(n), username: name, display_name: '', avatar_url: '' },
    response
  } as unknown as RSVPItem
}

const MANAGE_EVENTS = 33554432

describe('EventDetailModal', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
    usePermissionsStore().info = makePermsInfo(0)
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    pendingConfirm.value = null
  })

  function button(label: string) {
    return wrapper?.findAll('button').find(b => b.text().includes(label))
  }

  it('shows title, cover, end time, location text and the loaded participants', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([
      rsvp(2, 'going', 'alice'),
      rsvp(3, 'interested', 'bob'),
      rsvp(4, 'not_going', 'carol')
    ])
    wrapper = mount(EventDetailModal, {
      props: {
        event: makeEvent({
          cover_url: '/cover.png',
          end_time: '2026-11-01T20:00:00Z',
          location: 'Room 42'
        })
      }
    })
    expect(wrapper.text()).toContain('Game Night')
    expect(wrapper.text()).toContain('Scheduled')
    expect(wrapper.text()).toContain('Room 42')
    expect(wrapper.find('img').attributes('src')).toBe('/cover.png')
    await flushPromises()
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.text()).toContain('carol')
    expect(wrapper.text()).toContain('Not going')
  })

  it('shows empty states without description and responses', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    wrapper = mount(EventDetailModal, { props: { event: makeEvent({ description: '' }) } })
    await flushPromises()
    expect(wrapper.text()).toContain('There is no description.')
    expect(wrapper.text().match(/No responses yet\./g)).toHaveLength(1)
  })

  it('keeps working when the responses cannot be loaded', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockRejectedValue(new Error('offline'))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await flushPromises()
    expect(wrapper.text()).toContain('Could not load the responses.')
    expect(wrapper.text()).not.toContain('No responses yet.')
    warn.mockRestore()
  })

  it('sends one RSVP while the first is still running', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    let release: (e: CommunityEvent) => void = () => {}
    const set = vi.spyOn(store, 'setRSVP').mockImplementation(() => new Promise(r => { release = r }))
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await flushPromises()
    const going = button('I’m going')
    await going?.trigger('click')
    await going?.trigger('click')
    expect(set).toHaveBeenCalledTimes(1)
    release(makeEvent({ my_rsvp: 'going' }))
    await flushPromises()
  })

  it('shows a translated toast when the error has no message', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    vi.spyOn(store, 'setRSVP').mockRejectedValue('boom')
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await button('Can’t make it')?.trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.text === 'Couldn’t save your response. Try again.')).toBe(true)
  })

  it.each([
    ['active', 'Live'],
    ['cancelled', 'Cancelled'],
    ['completed', 'Completed']
  ] as const)('labels a %s event', (status, label) => {
    vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
    wrapper = mount(EventDetailModal, { props: { event: makeEvent({ status }) } })
    expect(wrapper.text()).toContain(label)
  })

  it('names the Talk of the event and lets members join while it is live', async () => {
    vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
    const chId = fixtureId(10)
    useChatStore().categories = [categoryFixture({
      id: fixtureId(2),
      channels: [channelFixture({ id: chId, type: 'voice', name: 'Lounge' })]
    })]
    wrapper = mount(EventDetailModal, { props: { event: makeEvent({ status: 'active', channel_id: chId }) } })
    expect(wrapper.text()).toContain('Lounge')
    await button('Join')?.trigger('click')
    expect(wrapper.emitted('join')?.[0]).toEqual([chId])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('sets an RSVP and reloads the participants', async () => {
    const store = useEventsStore()
    const list = vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    const set = vi.spyOn(store, 'setRSVP').mockResolvedValue(makeEvent({ my_rsvp: 'going' }))
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await flushPromises()
    await button('I’m going')?.trigger('click')
    await flushPromises()
    expect(set).toHaveBeenCalledWith(fixtureId(1), 'going')
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('withdraws the RSVP when the same answer is clicked again', async () => {
    const store = useEventsStore()
    store.events = [makeEvent({ my_rsvp: 'interested' })]
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    const del = vi.spyOn(store, 'deleteRSVP').mockResolvedValue(makeEvent())
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await button('Maybe')?.trigger('click')
    expect(del).toHaveBeenCalledWith(fixtureId(1))
  })

  it('reports a failing RSVP with a toast', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    vi.spyOn(store, 'setRSVP').mockRejectedValue(new Error('Too late'))
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await button('Can’t make it')?.trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.text === 'Too late')).toBe(true)
  })

  it('hides moderator actions without manage_events', () => {
    vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    expect(button('Cancel event')).toBeUndefined()
    expect(button('Edit event')).toBeUndefined()
  })

  describe('as a moderator', () => {
    beforeEach(() => { usePermissionsStore().info = makePermsInfo(MANAGE_EVENTS) })

    it('asks the parent to edit the event', async () => {
      vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
      wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
      await button('Edit event')?.trigger('click')
      expect(wrapper.emitted('edit')?.[0]?.[0]).toMatchObject({ id: fixtureId(1) })
    })

    it('does not offer cancelling a finished event', () => {
      vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ status: 'completed' }) } })
      expect(button('Cancel event')).toBeUndefined()
    })

    it('cancels the event after confirmation', async () => {
      const store = useEventsStore()
      vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
      const cancel = vi.spyOn(store, 'cancel').mockResolvedValue(makeEvent({ status: 'cancelled' }))
      wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
      await button('Cancel event')?.trigger('click')
      expect(pendingConfirm.value?.body).toContain('cancel this event')
      pendingConfirm.value?.resolve(true)
      await flushPromises()
      expect(cancel).toHaveBeenCalledWith(fixtureId(1))
      expect(useToastStore().toasts.some(t => t.type === 'success')).toBe(true)
    })

    it('does nothing when the confirmation is declined', async () => {
      const store = useEventsStore()
      vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
      const cancel = vi.spyOn(store, 'cancel')
      wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
      await button('Cancel event')?.trigger('click')
      pendingConfirm.value?.resolve(false)
      await flushPromises()
      expect(cancel).not.toHaveBeenCalled()
    })

    it('reports a failing cancel with a toast', async () => {
      const store = useEventsStore()
      vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
      vi.spyOn(store, 'cancel').mockRejectedValue(new Error('Nope'))
      wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
      await button('Cancel event')?.trigger('click')
      pendingConfirm.value?.resolve(true)
      await flushPromises()
      expect(useToastStore().toasts.some(t => t.text === 'Nope' && t.type === 'error')).toBe(true)
    })
  })

  describe('roles and sign-ups', () => {
    const tank = { id: fixtureId(101), name: 'Tank', position: 0, confirmed: 1, waitlist: 0, capacity: 1 }

    beforeEach(() => {
      useServerFeaturesStore().list = ['events', 'events.roles']
      vi.spyOn(useEventsStore(), 'listRSVPs').mockResolvedValue([])
    })

    it('reads the sign-ups of the event and shows the roster', async () => {
      const store = useEventsStore()
      const detail = vi.spyOn(store, 'fetchDetail').mockResolvedValue(makeEvent({ roles: [tank] }))
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ roles: [tank] }) } })
      await flushPromises()
      expect(detail).toHaveBeenCalledWith(fixtureId(1))
      expect(wrapper.find('[data-testid="event-roster"]').exists()).toBe(true)
      expect(wrapper.find('[data-testid="event-roles-edit"]').exists()).toBe(false)
    })

    it('reads them again when the counts of a role change, and survives a failing read', async () => {
      const store = useEventsStore()
      const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
      const detail = vi.spyOn(store, 'fetchDetail').mockRejectedValue(new Error('down'))
      store.events = [makeEvent({ roles: [tank] })]
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ roles: [tank] }) } })
      await flushPromises()
      store.events = [makeEvent({ roles: [{ ...tank, waitlist: 2 }] })]
      await flushPromises()
      expect(detail).toHaveBeenCalledTimes(2)
      expect(warn).toHaveBeenCalled()
      warn.mockRestore()
    })

    it('lets the creator open the role editor without Manage events, and nobody else', async () => {
      vi.spyOn(useEventsStore(), 'fetchDetail').mockResolvedValue(makeEvent())
      useAuthStore().user = userFixture({ id: fixtureId(5) })
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ created_by: fixtureId(5) }) }, attachTo: document.body })
      await flushPromises()
      await wrapper.get('[data-testid="event-roles-edit"]').trigger('click')
      expect(document.body.querySelector('[data-testid="event-roles-editor"]')).not.toBeNull()
      wrapper.unmount()
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ created_by: fixtureId(6) }) } })
      await flushPromises()
      expect(wrapper.find('[data-testid="event-roster"]').exists()).toBe(false)
    })

    it('shows no roster and reads nothing when the server has no roles', async () => {
      useServerFeaturesStore().list = ['events']
      const detail = vi.spyOn(useEventsStore(), 'fetchDetail')
      wrapper = mount(EventDetailModal, { props: { event: makeEvent({ roles: [tank] }) } })
      await flushPromises()
      expect(detail).not.toHaveBeenCalled()
      expect(wrapper.find('[data-testid="event-roster"]').exists()).toBe(false)
    })
  })
})
