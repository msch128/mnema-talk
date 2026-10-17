import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminUploadLimit from './AdminUploadLimit.vue'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminUploadLimit', () => {
  it('shows the limit and ceiling, saves a lower one and can reset it', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 50, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    expect(w.text()).toContain('50 MB')
    const input = w.find<HTMLInputElement>('input[type="number"]')
    expect(input.element.value).toBe('50')
    const save = () => w.find('button[type="submit"]')
    expect(save().attributes('disabled')).toBeDefined()
    // Reset is only offered below the ceiling.
    expect(w.findAll('button')).toHaveLength(1)

    apiMock.mockResolvedValueOnce({ max_upload_mb: 10, ceiling_mb: 50 })
    await input.setValue('10')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/upload-limit', expect.objectContaining({ method: 'PUT', json: { max_upload_mb: 10 } }))
    expect(useToastStore().toasts.map(t => t.text)).toContain('Upload limit saved.')
    expect(input.element.value).toBe('10')

    apiMock.mockResolvedValueOnce({ max_upload_mb: 50, ceiling_mb: 50 })
    await w.findAll('button').find(b => b.text().startsWith('Back to'))!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/upload-limit', expect.objectContaining({ json: { max_upload_mb: null } }))
  })

  it('refuses values above the ceiling or not whole numbers', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 20, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    const input = w.find<HTMLInputElement>('input[type="number"]')
    for (const bad of ['51', '0', '2.5']) {
      await input.setValue(bad)
      expect(w.find('[role="alert"]').text()).toContain('1 to 50')
      expect(w.find('button[type="submit"]').attributes('disabled')).toBeDefined()
    }
    await w.find('form').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('reports a failed save and a failed load', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 20, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('nope'))
    await w.find<HTMLInputElement>('input[type="number"]').setValue('5')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('nope')

    apiMock.mockRejectedValueOnce(new Error('down'))
    const failed = mount(AdminUploadLimit)
    await flushPromises()
    expect(failed.find('[data-testid="upload-limit"]').exists()).toBe(false)
    expect(useToastStore().toasts.map(t => t.text)).toContain('down')
  })
})
