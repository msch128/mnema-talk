import type { VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventsModal from './EventsModal.vue'
import EventDetailModal from './EventDetailModal.vue'
import { useEventsStore } from '../stores/events'
import { usePermissionsStore } from '../stores/permissions'
import { setLocale } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent, PermissionsInfo, RSVPItem } from '../types/domain'

vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel: vi.fn() }) }))
vi.mock('../lib/eventCommentsApi', () => ({
  fetchComments: vi.fn().mockResolvedValue({ comments: [], has_more: false, muted: false }),
  postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve([]))
}))

const NOW = new Date('2026-10-30T10:00:00Z')

function makePermsInfo(permissions: number): PermissionsInfo {
  return { permissions, catalog: [], channels: [], role_ids: [], max_upload_mb: 50, owner: false, top_position: 0 }
}

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Game Night',
    description: '',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 2, interested: 1, not_going: 0 },
    ...overrides
  }
}

describe('events: phone layout, avatars and RSVP wording', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(NOW)
    usePermissionsStore().info = makePermsInfo(33554432)
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.useRealTimers()
  })

  function seed(events: CommunityEvent[]) {
    const store = useEventsStore()
    vi.spyOn(store, 'load').mockImplementation(async () => { store.events = events })
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    store.events = events
    return store
  }

  it('does not let a card or its cover shrink in the scrolling list (clipped cards on a phone)', () => {
    seed([makeEvent({ cover_url: '/c.png' })])
    wrapper = mount(EventsModal)
    expect(wrapper.get('article').classes()).toContain('shrink-0')
    expect(wrapper.get('article > div').classes()).toContain('shrink-0')
  })

  it('names the members who are going instead of showing "?" avatars', () => {
    seed([makeEvent({ going_members: [{ id: fixtureId(5), username: 'alice', display_name: 'Alice' }, { id: fixtureId(6), username: 'bob', display_name: '' }] })])
    wrapper = mount(EventsModal)
    const initials = wrapper.findAll('[data-testid="event-rsvp-summary"] div.rounded-full').map(s => s.text())
    expect(initials).toEqual(['A', 'B'])
  })

  it('shows the counts as a line and the buttons as the member\'s own answers', () => {
    seed([makeEvent()])
    wrapper = mount(EventsModal)
    expect(wrapper.get('[data-testid="event-rsvp-summary"]').text()).toBe('2 going · 1 interested')
    const labels = wrapper.findAll('button[aria-pressed]').map(b => b.text())
    expect(labels).toEqual(['I’m going', 'Maybe'])
  })

  it('writes the weekday and a relative hint next to the start', () => {
    seed([makeEvent()])
    wrapper = mount(EventsModal)
    expect(wrapper.text()).toMatch(/Sun/)
    expect(wrapper.text()).toContain('in 2 days')
  })

  it('shows one dialog at a time: the list gives way to the detail and returns', async () => {
    seed([makeEvent()])
    wrapper = mount(EventsModal)
    expect(wrapper.text()).toContain('Upcoming')
    await wrapper.findAll('button').find(b => b.text().includes('Details'))?.trigger('click')
    expect(wrapper.findComponent({ name: 'EventDetailModal' }).exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Upcoming')
    wrapper.findComponent({ name: 'EventDetailModal' }).vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('Upcoming')
  })

  it('keeps an accessible name on the create button, whose text is hidden on a phone', () => {
    seed([])
    wrapper = mount(EventsModal)
    const create = wrapper.findAll('button').find(b => b.attributes('aria-label') === 'Create event')
    expect(create?.get('span').classes()).toContain('max-md:hidden')
  })

  it('shows the real names of the responses in the detail view', async () => {
    const store = seed([makeEvent()])
    const items = [{ user: { id: fixtureId(5), username: 'alice', display_name: 'Alice', avatar_url: '' }, response: 'going' }] as unknown as RSVPItem[]
    vi.spyOn(store, 'listRSVPs').mockResolvedValue(items)
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await flushPromises()
    const avatar = wrapper.get('.divide-y span.rounded-full, .divide-y div.rounded-full')
    expect(avatar.text()).toBe('A')
  })

  it('shows the counts apart from the three answer buttons and no collision with cancelling the event', async () => {
    seed([makeEvent()])
    wrapper = mount(EventDetailModal, { props: { event: makeEvent() } })
    await flushPromises()
    expect(wrapper.get('[data-testid="event-rsvp-counts"]').text()).toBe('2 going · 1 interested')
    expect(wrapper.findAll('[data-rsvp]').map(b => b.text())).toEqual(['I’m going', 'Maybe', 'Can’t make it'])
    const cancel = wrapper.findAll('button').filter(b => b.text() === 'Cancel event')
    expect(cancel).toHaveLength(1)
  })
})

describe('events: count wording', () => {
  it('uses the singular for one answer in German and keeps the English form', async () => {
    setActivePinia(createPinia())
    for (const [lang, one, many] of [['de', '1 Zusage · 1 Vielleicht', '2 Zusagen · 1 Vielleicht'], ['en', '1 going · 1 interested', '2 going · 1 interested']] as const) {
      setLocale(lang)
      for (const [going, expected] of [[1, one], [2, many]] as const) {
        const w = mount(EventDetailModal, { props: { event: makeEvent({ rsvp_counts: { going, interested: 1, not_going: 0 } }) } })
        await flushPromises()
        expect(w.get('[data-testid="event-rsvp-counts"]').text()).toBe(expected)
        w.unmount()
      }
    }
    setLocale('en')
  })
})
