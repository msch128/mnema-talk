import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { channelFixture } from '../test-fixtures.fixture'

const openSearch = vi.fn()
vi.mock('../lib/globalSearch', () => ({ openSearch: (q?: string) => openSearch(q) }))

beforeEach(() => {
  setActivePinia(createPinia())
  openSearch.mockReset()
  setLocale('en')
})
afterEach(() => { document.body.innerHTML = '' })

describe('ChatHeader search button', () => {
  it('opens the search limited to the active channel', async () => {
    const chat = useChatStore()
    chat.uncategorized = [channelFixture({ name: 'general' })]
    chat.activeChannel = chat.uncategorized[0]!
    const wrapper = mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } })
    await wrapper.find('[data-testid="search-in-channel"]').trigger('click')
    expect(openSearch).toHaveBeenCalledWith('in:general ')
  })

  it('quotes channel names with spaces and is absent without a channel', async () => {
    const chat = useChatStore()
    chat.activeChannel = null
    const wrapper = mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } })
    expect(wrapper.find('[data-testid="search-in-channel"]').exists()).toBe(false)
    chat.activeChannel = channelFixture({ name: 'my room' })
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-testid="search-in-channel"]').trigger('click')
    expect(openSearch).toHaveBeenCalledWith('in:"my room" ')
  })
})

describe('ChatHeader overflow', () => {
  let width = 1000
  let fire: (w: number) => void = () => {}
  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb: (entries: { contentRect: { width: number } }[]) => void) { fire = w => cb([{ contentRect: { width: w } }]) }
      observe() { fire(width) }
      disconnect() {}
    })
  })
  afterEach(() => vi.unstubAllGlobals())

  function mountHeader() {
    const chat = useChatStore()
    chat.uncategorized = [channelFixture({ name: 'general' })]
    chat.activeChannel = chat.uncategorized[0]!
    return mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } })
  }

  it('keeps all buttons in a wide header', async () => {
    width = 900
    const wrapper = mountHeader()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="header-more"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="mentions-inbox-button"]').exists()).toBe(true)
  })

  it('folds mentions, saved and later into a More menu in a narrow header', async () => {
    width = 500
    const wrapper = mountHeader()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="mentions-inbox-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="search-in-channel"]').exists()).toBe(true)
    await wrapper.find('[data-testid="header-more"]').trigger('click')
    const [, , items] = wrapper.emitted('menu')![0] as [number, number, { id: string; action: () => void }[]]
    expect(items.map(i => i.id)).toContain('mentions')
    items.find(i => i.id === 'mentions')!.action()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="mentions-inbox"]').exists()).toBe(true)
  })

  it('lists saved and later in the More menu when the server has them, and marks unread mentions', async () => {
    width = 500
    useServerFeaturesStore().list = ['saved', 'reminders']
    useChatStore().readStates = { c1: { mention_count: 2 } } as never
    const wrapper = mountHeader()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="header-more-badge"]').exists()).toBe(true)
    await wrapper.find('[data-testid="header-more"]').trigger('click')
    const [, , items] = wrapper.emitted('menu')![0] as [number, number, { id: string; action: () => void }[]]
    expect(items.map(i => i.id)).toEqual(['mentions', 'saved', 'later'])
    for (const item of items) {
      item.action()
      await wrapper.vm.$nextTick()
    }
    expect(wrapper.find('[data-testid="mentions-inbox"]').exists()).toBe(false)
  })

  it('unfolds again when the header widens', async () => {
    width = 500
    const wrapper = mountHeader()
    await wrapper.vm.$nextTick()
    fire(900)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="header-more"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="mentions-inbox-button"]').exists()).toBe(true)
  })
})
