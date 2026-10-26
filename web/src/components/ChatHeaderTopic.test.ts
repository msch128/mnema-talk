import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatHeader from './ChatHeader.vue'
import { useChatStore } from '../stores/chat'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { channelFixture } from '../test-fixtures.fixture'

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string) => {
    if (url === '/api/health') return Promise.resolve({ features: [] })
    return Promise.resolve([])
  })
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => { document.body.innerHTML = '' })

describe('ChatHeader channel description', () => {
  it('shows the topic as one line and expands it on click', async () => {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ name: 'general', topic: 'Release planning\nand questions' })
    const wrapper = mount(ChatHeader, mountOpts)
    const line = wrapper.get('[data-testid="channel-topic"]')
    expect(line.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('[data-testid="channel-topic-full"]').exists()).toBe(false)
    await line.trigger('click')
    expect(line.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[data-testid="channel-topic-full"]').text()).toContain('and questions')
    chat.activeChannel = channelFixture({ id: '00000000-0000-4000-8000-000000000999', name: 'other', topic: 'Other' })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="channel-topic-full"]').exists()).toBe(false)
  })

  it('shows nothing for a channel without a topic', () => {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ topic: '' })
    const wrapper = mount(ChatHeader, mountOpts)
    expect(wrapper.find('[data-testid="channel-topic"]').exists()).toBe(false)
  })
})
