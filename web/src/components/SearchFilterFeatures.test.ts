import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import SearchModal from './SearchModal.vue'
import { filtersUnsupported, markFiltersSupported } from '../lib/searchSupport'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: () => Promise.resolve({ messages: [], has_more: false })
}))

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

beforeEach(() => {
  setActivePinia(createPinia())
  markFiltersSupported()
})
afterEach(() => {
  document.body.replaceChildren()
  markFiltersSupported()
})

describe('search filters and the server feature list', () => {
  it('count as unsupported once the list is known and lacks search.filters.v2 (a 1.0.x server sends none)', async () => {
    const features = useServerFeaturesStore()
    const w = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
    expect(filtersUnsupported.value).toBe(false)
    features.list = []
    features.loaded = true
    await nextTick()
    expect(filtersUnsupported.value).toBe(true)
    w.unmount()
  })

  it('stay available when the list names them', async () => {
    const features = useServerFeaturesStore()
    const w = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: false } })
    features.list = ['dm', 'search.filters.v2']
    features.loaded = true
    await nextTick()
    expect(filtersUnsupported.value).toBe(false)
    w.unmount()
  })

  it('hides the v3 filter row and sort toggle without search.filters.v3', async () => {
    const features = useServerFeaturesStore()
    features.list = ['dm', 'search.filters.v2']
    features.loaded = true
    const w = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: true } })
    await nextTick()
    expect(document.querySelector('[data-search-filter-row]')).toBeNull()
    expect(document.querySelector('[data-testid="search-results-header"]')).toBeNull()
    w.unmount()
  })

  it('shows v3 features when search.filters.v3 is present', async () => {
    const features = useServerFeaturesStore()
    features.list = ['dm', 'search.filters.v2', 'search.filters.v3']
    features.loaded = true
    const w = mount(SearchModal, { ...mountOpts, attachTo: document.body, props: { modelValue: true } })
    await nextTick()
    expect(document.querySelector('[data-search-filter-row]')).not.toBeNull()
    w.unmount()
  })
})
