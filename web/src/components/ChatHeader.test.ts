import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useAuthStore } from '../stores/auth'
import { useFirstRunStore } from '../stores/firstRun'
import { useOnboardingStore } from '../stores/onboarding'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { channelFixture, onboardingStateFixture, userFixture } from '../test-fixtures.fixture'

const openSearch = vi.fn()
vi.mock('../lib/globalSearch', async () => ({ openSearch: (q?: string) => openSearch(q), searchOpen: (await import('vue')).ref(false) }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string) => {
    if (url === '/api/health') return Promise.resolve({ features: useServerFeaturesStore().list })
    if (url.startsWith('/api/me/saved')) return Promise.resolve({ items: [], has_more: false })
    if (url.startsWith('/api/mentions')) return Promise.resolve({ messages: [], has_more: false })
    if (url.startsWith('/api/me/reminders') || url.startsWith('/api/me/scheduled')) return Promise.resolve({ items: [] })
    return Promise.resolve([])
  })
}))

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

  it('folds mentions and saved & reminders into a More menu in a narrow header', async () => {
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

  it('lists saved & reminders in the More menu when the server has them, and marks unread mentions', async () => {
    width = 500
    useServerFeaturesStore().list = ['saved', 'reminders']
    useChatStore().readStates = { c1: { mention_count: 2 } } as never
    const wrapper = mountHeader()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="header-more-badge"]').exists()).toBe(true)
    await wrapper.find('[data-testid="header-more"]').trigger('click')
    const [, , items] = wrapper.emitted('menu')![0] as [number, number, { id: string; action: () => void }[]]
    expect(items.map(i => i.id)).toEqual(['mentions', 'saved'])
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

describe('ChatHeader and the Owner\'s checklist', () => {
  function mountOwnerHeader() {
    const chat = useChatStore()
    chat.uncategorized = [channelFixture({ name: 'general' })]
    chat.activeChannel = chat.uncategorized[0]!
    useAuthStore().user = userFixture({ role: 'admin' })
    useOnboardingStore().state = onboardingStateFixture({ welcome_done: true, dismissed_tips: [] })
    useOnboardingStore().ensureLoaded = vi.fn().mockResolvedValue(undefined)
    const firstRun = useFirstRunStore()
    firstRun.ensureLoaded = vi.fn().mockResolvedValue(undefined)
    return { firstRun, wrapper: mount(ChatHeader, { global: { plugins: [i18nPlugin], directives: { tooltip } } }) }
  }
  const steps = [{ id: 'name', done: false, optional: false }]

  it('shows the checklist instead of a tip while it is open, and the tip once it is closed', async () => {
    const { firstRun, wrapper } = mountOwnerHeader()
    firstRun.state = { steps, hidden: false, complete: false }
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="first-run"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="tip-bar"]').exists()).toBe(false)
    firstRun.state = { steps, hidden: true, complete: false }
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="first-run"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="tip-bar"]').exists()).toBe(true)
  })
})
