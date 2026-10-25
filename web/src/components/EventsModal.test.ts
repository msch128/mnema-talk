import type { VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventsModal from './EventsModal.vue'
import { useEventsStore } from '../stores/events'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { setLocale } from '../i18n'
import { categoryFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent, PermissionsInfo } from '../types/domain'

const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ joinVoiceChannel })
}))

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve([]))
}))

function makePermsInfo(permissions: number): PermissionsInfo {
  return {
    permissions,
    catalog: [],
    channels: [],
    role_ids: [],
    max_upload_mb: 50,
    owner: false,
    top_position: 0
  }
}

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Game Night',
    description: 'Playing games',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 1, interested: 0, not_going: 0 },
    ...overrides
  }
}

describe('EventsModal', () => {
  let wrapper: VueWrapper

  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
    joinVoiceChannel.mockReset()
    const perms = usePermissionsStore()
    perms.info = makePermsInfo(0)
  })

  it('renders upcoming events and switches to past events tab', async () => {
    const store = useEventsStore()
    const upcoming = makeEvent({ id: fixtureId(1), title: 'Upcoming Gala', status: 'scheduled' })
    const past = makeEvent({ id: fixtureId(2), title: 'Past Tournament', status: 'completed' })
    vi.spyOn(store, 'load').mockImplementation(async () => {
      store.events = [upcoming, past]
    })
    store.events = [upcoming, past]

    wrapper = mount(EventsModal, { attachTo: document.body })
    expect(wrapper.text()).toContain('Upcoming Gala')
    expect(wrapper.text()).not.toContain('Past Tournament')

    const pastTab = wrapper.findAll('button').find(b => b.text().includes('Past'))
    await pastTab?.trigger('click')
    expect(wrapper.text()).toContain('Past Tournament')
    expect(wrapper.text()).not.toContain('Upcoming Gala')
  })

  it('loads the past events from the server when the tab opens and pages on request', async () => {
    const store = useEventsStore()
    vi.spyOn(store, 'load').mockResolvedValue()
    const loadPast = vi.spyOn(store, 'loadPast').mockImplementation(async () => {
      store.pastLoaded = true
    })
    store.pastHasMore = true

    wrapper = mount(EventsModal, { attachTo: document.body })
    expect(loadPast).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(b => b.text().includes('Past'))?.trigger('click')
    expect(loadPast).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="events-load-more"]').exists()).toBe(false)

    store.events = [makeEvent({ id: fixtureId(7), title: 'Long Ago', status: 'completed' })]
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-testid="events-load-more"]').trigger('click')
    expect(loadPast).toHaveBeenLastCalledWith(true)
  })

  it('shows create button only when member has manage_events permission', async () => {
    const perms = usePermissionsStore()
    perms.info = makePermsInfo(0)
    wrapper = mount(EventsModal)
    expect(wrapper.text()).not.toContain('Create event')

    perms.info = makePermsInfo(33554432) // manage_events
    wrapper = mount(EventsModal)
    expect(wrapper.text()).toContain('Create event')
  })

  it('allows joining active Talk channel event', async () => {
    const store = useEventsStore()
    const chat = useChatStore()
    const chId = fixtureId(10)
    chat.categories = [categoryFixture({
      id: fixtureId(2),
      name: 'Talks',
      channels: [{
        id: chId,
        category_id: fixtureId(2),
        name: 'General Talk',
        type: 'voice',
        sort_order: 0,
        created_at: FIXTURE_TIMESTAMP,
        number: 1,
        topic: '',
        slow_mode_seconds: 0,
        user_limit: 0
      }]
    })]

    const activeEv = makeEvent({
      id: fixtureId(1),
      title: 'Voice Hangout',
      status: 'active',
      channel_id: chId
    })
    vi.spyOn(store, 'load').mockImplementation(async () => {
      store.events = [activeEv]
    })
    store.events = [activeEv]

    wrapper = mount(EventsModal)
    const joinBtn = wrapper.findAll('button').find(b => b.text().includes('Join'))
    expect(joinBtn).toBeDefined()
    await joinBtn?.trigger('click')

    expect(joinVoiceChannel).toHaveBeenCalledWith(chId)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('handles quick RSVP button click', async () => {
    const store = useEventsStore()
    const ev = makeEvent({ id: fixtureId(1), my_rsvp: null })
    vi.spyOn(store, 'load').mockImplementation(async () => {
      store.events = [ev]
    })
    store.events = [ev]
    const rsvpSpy = vi.spyOn(store, 'setRSVP').mockResolvedValue({ ...ev, my_rsvp: 'going' })

    wrapper = mount(EventsModal)
    const goingBtn = wrapper.findAll('button').find(b => b.text().includes('I’m going'))
    await goingBtn?.trigger('click')

    expect(rsvpSpy).toHaveBeenCalledWith(fixtureId(1), 'going')
  })
})
