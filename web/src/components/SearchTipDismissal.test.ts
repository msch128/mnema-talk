import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import SearchResultItem from './SearchResultItem.vue'
import { searchText } from '../lib/globalSearch'
import { useOnboardingStore } from '../stores/onboarding'
import { messageFixture } from '../test-fixtures.fixture'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

beforeEach(() => {
  mockApi.mockReset()
  searchText.value = ''
  setActivePinia(createPinia())
  setLocale('en')
})

afterEach(() => {
  document.body.innerHTML = ''
  setLocale('de')
})

describe('the search tip', () => {
  it('is dismissed for the account after the first search that finds its way to the server', async () => {
    const onboarding = useOnboardingStore()
    onboarding.state = {
      community_name: 'Mnema', dismissed_tips: [], rules_accepted_version: 0, rules_pending: false, rules_text: '',
      rules_version: 0, start_channel_id: null, welcome_done: true, welcome_text: ''
    }
    mockApi.mockImplementation(async path => (path.startsWith('/api/search')
      ? { messages: [], has_more: false, roots: {} }
      : onboarding.state))
    const wrapper = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
    await wrapper.setProps({ modelValue: true })
    await nextTick()
    expect(onboarding.state?.dismissed_tips).toEqual([])

    searchText.value = 'hello'
    await nextTick()
    await vi.waitFor(() => expect(onboarding.state?.dismissed_tips).toContain('search'))
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/onboarding/tips', expect.objectContaining({ method: 'PUT', json: { dismissed_tips: ['search'] } }))
    wrapper.unmount()
  })
})

describe('SearchResultItem markers', () => {
  const props = { index: 0, selected: false, voice: false, terms: [] }

  it('marks pinned messages and polls', () => {
    const plain = mount(SearchResultItem, { ...mountOpts, props: { ...props, msg: messageFixture({ is_pinned: false }) } })
    expect(plain.find('[data-search-marker]').exists()).toBe(false)

    const pinned = mount(SearchResultItem, { ...mountOpts, props: { ...props, msg: messageFixture({ is_pinned: true }) } })
    expect(pinned.find('[data-search-marker="pinned"]').text()).toBe('Pinned')

    const poll = { question: 'q', options: [], multiple: false, anonymous: false, ends_at: '2026-01-01T00:00:00Z', ended: false, total_votes: 0 }
    const withPoll = mount(SearchResultItem, { ...mountOpts, props: { ...props, msg: messageFixture({ poll } as never) } })
    expect(withPoll.find('[data-search-marker="poll"]').text()).toBe('Poll')
  })
})
