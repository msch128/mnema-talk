import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import { ApiError } from '../lib/api'
import { searchText } from '../lib/globalSearch'
import { filtersUnsupported, markFiltersSupported } from '../lib/searchSupport'
import { suggestFor } from '../lib/searchSuggest'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }
const lookup = { members: [], channels: [] }

beforeEach(() => {
  mockApi.mockReset()
  searchText.value = ''
  markFiltersSupported()
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => {
  document.body.replaceChildren()
  markFiltersSupported()
  setLocale('de')
})

describe('search filters against a server from before 1.1', () => {
  it('explains a refused new filter instead of failing, and stops suggesting the new filters', async () => {
    mockApi.mockRejectedValue(new ApiError(400, 'INVALID_INPUT', 'has must be file, image, video or link'))
    const wrapper = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
    await wrapper.setProps({ modelValue: true })
    await nextTick()
    expect(suggestFor('is:', lookup)?.items.map(i => i.value)).toEqual(['pinned'])

    searchText.value = 'has:poll '
    await vi.waitFor(() => expect(document.querySelector('[data-testid="search-filter-unsupported"]')).not.toBeNull())
    expect(document.querySelector('[data-testid="search-filter-unsupported"]')?.textContent).toContain('doesn’t support this filter')
    expect(filtersUnsupported.value).toBe(true)

    expect(suggestFor('is:', lookup)).toBeNull()
    expect(suggestFor('has:', lookup)?.items.map(i => i.value)).toEqual(['file', 'image', 'video', 'link'])
    expect(suggestFor('has:p', lookup)).toBeNull()
    wrapper.unmount()
  })

  it('keeps the plain failure message when the refused search used no new filter', async () => {
    mockApi.mockRejectedValue(new ApiError(400, 'INVALID_INPUT', 'bad'))
    const wrapper = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
    await wrapper.setProps({ modelValue: true })
    searchText.value = 'has:image '
    await vi.waitFor(() => expect(document.querySelector('[role="alert"]')).not.toBeNull())
    expect(document.querySelector('[data-testid="search-filter-unsupported"]')).toBeNull()
    expect(filtersUnsupported.value).toBe(false)
    wrapper.unmount()
  })
})
