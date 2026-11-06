import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminReportsTab from './AdminReportsTab.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminReportsTab', () => {
  it('says the reports could not be loaded, with a retry, instead of "no reports"', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const w = mount(AdminReportsTab)
    await flushPromises()
    expect(w.find('[data-testid="reports-error"]').exists()).toBe(true)
    expect(w.find('[data-testid="reports-empty"]').exists()).toBe(false)
    apiMock.mockReset().mockResolvedValue({ reports: [], has_more: false })
    await w.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(w.find('[data-testid="reports-error"]').exists()).toBe(false)
    expect(w.find('[data-testid="reports-empty"]').exists()).toBe(true)
  })
})
