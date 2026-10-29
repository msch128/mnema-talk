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
  const save = (w: ReturnType<typeof mount>) => w.find<HTMLButtonElement>('[data-testid="unsaved-save"]').element

  it('shows the limit and ceiling, saves a lower one and can go back to the default', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 50, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    expect(w.text()).toContain('50 MB')
    const input = w.find<HTMLInputElement>('input[type="number"]')
    expect(input.element.value).toBe('50')
    expect(save(w).disabled).toBe(true)
    // "Back to the default" is only offered below the ceiling.
    expect(w.findAll('button').some(b => b.text().startsWith('Back to'))).toBe(false)

    await input.setValue('10')
    expect(apiMock).toHaveBeenCalledTimes(1)
    apiMock.mockResolvedValueOnce({ max_upload_mb: 10, ceiling_mb: 50 })
    await save(w).click()
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/upload-limit', expect.objectContaining({ method: 'PUT', json: { max_upload_mb: 10 } }))
    expect(useToastStore().toasts.map(t => t.text)).toContain('Upload limit saved.')
    expect(input.element.value).toBe('10')
    expect(save(w).disabled).toBe(true)

    // The default only changes the draft: nothing is stored before Save.
    await w.findAll('button').find(b => b.text().startsWith('Back to'))!.trigger('click')
    expect(input.element.value).toBe('50')
    expect(apiMock).toHaveBeenCalledTimes(2)
    expect(save(w).disabled).toBe(false)
    apiMock.mockResolvedValueOnce({ max_upload_mb: 50, ceiling_mb: 50 })
    await save(w).click()
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/upload-limit', expect.objectContaining({ json: { max_upload_mb: null } }))
  })

  it('treats a value written differently as unchanged and resets the draft', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 20, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    const input = w.find<HTMLInputElement>('input[type="number"]')
    await input.setValue('020')
    expect(save(w).disabled).toBe(true)
    await input.setValue('30')
    expect(save(w).disabled).toBe(false)
    await w.find('[data-testid="unsaved-reset"]').trigger('click')
    expect(input.element.value).toBe('20')
  })

  it('refuses values above the ceiling or not whole numbers', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 20, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    const input = w.find<HTMLInputElement>('input[type="number"]')
    for (const bad of ['51', '0', '2.5']) {
      await input.setValue(bad)
      expect(w.find('[role="alert"]').text()).toContain('1 to 50')
      expect(save(w).disabled).toBe(true)
    }
    await w.find('form').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('reports a failed save inline and a failed load as a toast', async () => {
    apiMock.mockResolvedValueOnce({ max_upload_mb: 20, ceiling_mb: 50 })
    const w = mount(AdminUploadLimit)
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('nope'))
    await w.find<HTMLInputElement>('input[type="number"]').setValue('5')
    await w.find('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="unsaved-error"]').text()).toContain('nope')
    expect(save(w).disabled).toBe(false)

    apiMock.mockRejectedValueOnce(new Error('down'))
    const failed = mount(AdminUploadLimit)
    await flushPromises()
    expect(failed.find('[data-testid="upload-limit"]').exists()).toBe(false)
    expect(useToastStore().toasts.map(t => t.text)).toContain('down')
  })
})
