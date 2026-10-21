import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
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
