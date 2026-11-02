import type { VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EventsModal from './EventsModal.vue'
import { useEventsStore } from '../stores/events'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { categoryFixture, channelFixture, fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent, PermissionsInfo } from '../types/domain'

const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ joinVoiceChannel })
}))

vi.mock('../lib/eventCommentsApi', () => ({
  fetchComments: vi.fn().mockResolvedValue({ comments: [], has_more: false, muted: false }),
  postComment: vi.fn(), editComment: vi.fn(), deleteComment: vi.fn(), setCommentsMuted: vi.fn()
}))
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
    description: '',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 1, interested: 0, not_going: 0 },
    ...overrides
  }
}

describe('EventsModal details and editing', () => {
  let wrapper: VueWrapper

  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
    joinVoiceChannel.mockReset()
    usePermissionsStore().info = makePermsInfo(33554432)
  })

  function seed(events: CommunityEvent[]) {
    const store = useEventsStore()
    vi.spyOn(store, 'load').mockImplementation(async () => { store.events = events })
    vi.spyOn(store, 'listRSVPs').mockResolvedValue([])
    store.events = events
    return store
  }

  const button = (label: string) => wrapper.findAll('button').find(b => b.text().includes(label))

  it('shows empty states for both tabs', async () => {
    seed([])
    wrapper = mount(EventsModal)
    expect(wrapper.text()).toContain('No upcoming events scheduled.')
    await button('Past')?.trigger('click')
    expect(wrapper.text()).toContain('No past events.')
  })

  it('shows a loading placeholder while the first load runs', () => {
    const store = seed([])
    store.loading = true
    wrapper = mount(EventsModal)
    expect(wrapper.text()).not.toContain('No upcoming events scheduled.')
  })

  it('renders card details: Talk name, location, status, description and going members', () => {
    const chId = fixtureId(10)
    useChatStore().categories = [categoryFixture({
      id: fixtureId(2),
      channels: [channelFixture({ id: chId, type: 'voice', name: 'Lounge' })]
    })]
    seed([
      makeEvent({
        id: fixtureId(1),
        title: 'Live One',
        status: 'active',
        channel_id: chId,
        cover_url: '/c.png',
        going_members: [{ id: fixtureId(5), username: 'alice', display_name: 'Alice' }]
      }),
      makeEvent({ id: fixtureId(2), title: 'Gone Talk', channel_id: fixtureId(99) }),
      makeEvent({ id: fixtureId(3), title: 'In Person', location: 'Room 42' })
    ])
    wrapper = mount(EventsModal)
    expect(wrapper.text()).toContain('Lounge')
    expect(wrapper.text()).toContain('Live')
    expect(wrapper.text()).toContain('Talk')
    expect(wrapper.text()).toContain('Room 42')
    expect(wrapper.text()).toContain('1 going')
    expect(wrapper.find('img').attributes('src')).toBe('/c.png')
  })

  it('labels cancelled and completed events in the past tab', async () => {
    seed([
      makeEvent({ id: fixtureId(1), title: 'Called Off', status: 'cancelled' }),
      makeEvent({ id: fixtureId(2), title: 'Done', status: 'completed' })
    ])
    wrapper = mount(EventsModal)
    await button('Past')?.trigger('click')
    expect(wrapper.text()).toContain('Cancelled')
    expect(wrapper.text()).toContain('Completed')
  })

  it('opens the detail view from the title and closes it again', async () => {
    seed([makeEvent({ title: 'Game Night' })])
    wrapper = mount(EventsModal)
    await wrapper.find('h3 button').trigger('click')
    const detail = wrapper.findComponent({ name: 'EventDetailModal' })
    expect(detail.exists()).toBe(true)
    detail.vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'EventDetailModal' }).exists()).toBe(false)
  })

  it('opens the detail view from the description and the details button', async () => {
    seed([makeEvent({ description: 'Bring snacks' })])
    wrapper = mount(EventsModal)
    await wrapper.find('p.line-clamp-2').trigger('click')
    expect(wrapper.findComponent({ name: 'EventDetailModal' }).exists()).toBe(true)
    wrapper.findComponent({ name: 'EventDetailModal' }).vm.$emit('close')
    await wrapper.vm.$nextTick()
    await button('Details')?.trigger('click')
    expect(wrapper.findComponent({ name: 'EventDetailModal' }).exists()).toBe(true)
  })

  it('joins the Talk from the detail view and closes', async () => {
    const chId = fixtureId(10)
    seed([makeEvent({ status: 'active', channel_id: chId })])
    wrapper = mount(EventsModal)
    await button('Details')?.trigger('click')
    wrapper.findComponent({ name: 'EventDetailModal' }).vm.$emit('join', chId)
    expect(joinVoiceChannel).toHaveBeenCalledWith(chId)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('hands an event from the detail view to the edit dialog and closes it', async () => {
    const ev = makeEvent({ location: 'Room 1' })
    seed([ev])
    wrapper = mount(EventsModal)
    await button('Details')?.trigger('click')
    wrapper.findComponent({ name: 'EventDetailModal' }).vm.$emit('edit', ev)
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'EventDetailModal' }).exists()).toBe(false)
    const edit = wrapper.findComponent({ name: 'EventEditDialog' })
    expect(edit.exists()).toBe(true)
    edit.vm.$emit('saved', ev)
    edit.vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'EventEditDialog' }).exists()).toBe(false)
  })

  it('opens the create dialog from the header button', async () => {
    seed([])
    wrapper = mount(EventsModal)
    await button('Create event')?.trigger('click')
    expect(wrapper.findComponent({ name: 'EventEditDialog' }).exists()).toBe(true)
  })

  it('withdraws an RSVP when the active answer is clicked again', async () => {
    const store = seed([makeEvent({ my_rsvp: 'going' })])
    const del = vi.spyOn(store, 'deleteRSVP').mockResolvedValue(makeEvent())
    wrapper = mount(EventsModal)
    await button('I’m going')?.trigger('click')
    expect(del).toHaveBeenCalledWith(fixtureId(1))
  })

  it('reports a failed RSVP with a toast', async () => {
    const store = seed([makeEvent()])
    vi.spyOn(store, 'setRSVP').mockRejectedValue(new Error('Full'))
    wrapper = mount(EventsModal)
    await button('Maybe')?.trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.text === 'Full')).toBe(true)
  })

  it('words a failed RSVP without a server message in the UI language', async () => {
    const store = seed([makeEvent()])
    vi.spyOn(store, 'setRSVP').mockRejectedValue('offline')
    wrapper = mount(EventsModal)
    await button('Maybe')?.trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some(t => t.text === 'Couldn’t save your response. Try again.')).toBe(true)
  })

  it('closes through the dialog', () => {
    seed([])
    wrapper = mount(EventsModal)
    wrapper.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
