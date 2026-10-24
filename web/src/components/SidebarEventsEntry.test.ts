import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarEventsEntry from './SidebarEventsEntry.vue'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useEventsStore } from '../stores/events'
import { setLocale } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

describe('SidebarEventsEntry', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    setLocale('en')
  })

  it('stays hidden when events feature is disabled', () => {
    const features = useServerFeaturesStore()
    features.list = []
    const wrapper = mount(SidebarEventsEntry)
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('renders entry with badge when feature is enabled and events are upcoming', () => {
    const features = useServerFeaturesStore()
    features.list = ['events']
    const store = useEventsStore()
    store.events = [
      {
        id: fixtureId(1),
        title: 'Community Call',
        description: '',
        start_time: '2026-11-01T18:00:00Z',
        status: 'scheduled',
        created_at: FIXTURE_TIMESTAMP,
        updated_at: FIXTURE_TIMESTAMP,
        rsvp_counts: { going: 1, interested: 0, not_going: 0 }
      }
    ]

    const wrapper = mount(SidebarEventsEntry)
    expect(wrapper.find('button').exists()).toBe(true)
    expect(wrapper.text()).toContain('Events')
    expect(wrapper.text()).toContain('1')
  })

  it('opens EventsModal on button click', async () => {
    const features = useServerFeaturesStore()
    features.list = ['events']

    const wrapper = mount(SidebarEventsEntry, { attachTo: document.body })
    expect(wrapper.findComponent({ name: 'EventsModal' }).exists()).toBe(false)

    await wrapper.find('button').trigger('click')
    expect(wrapper.findComponent({ name: 'EventsModal' }).exists()).toBe(true)
  })

  it('loads the events on mount when the feature is enabled', () => {
    useServerFeaturesStore().list = ['events']
    const load = vi.spyOn(useEventsStore(), 'load').mockResolvedValue()
    mount(SidebarEventsEntry)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('does not load events when the feature is disabled', () => {
    const load = vi.spyOn(useEventsStore(), 'load').mockResolvedValue()
    mount(SidebarEventsEntry)
    expect(load).not.toHaveBeenCalled()
  })

  it('closes the EventsModal again', async () => {
    useServerFeaturesStore().list = ['events']
    vi.spyOn(useEventsStore(), 'load').mockResolvedValue()
    const wrapper = mount(SidebarEventsEntry, { attachTo: document.body })
    await wrapper.find('button').trigger('click')
    wrapper.findComponent({ name: 'EventsModal' }).vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'EventsModal' }).exists()).toBe(false)
    wrapper.unmount()
  })
})
