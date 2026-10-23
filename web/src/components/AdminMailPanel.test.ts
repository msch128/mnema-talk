import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminMailPanel from './AdminMailPanel.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const on = { configured: true, tls: 'starttls', from: 'Mnema <noreply@example.org>' }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminMailPanel', () => {
  it('says mail is off without offering a test', () => {
    const w = mount(AdminMailPanel, { props: { mail: { configured: false, tls: '', from: '' } } })
    expect(w.get('[data-testid="admin-mail-off"]').text()).toContain('SMTP_HOST')
    expect(w.find('[data-testid="admin-mail-test"]').exists()).toBe(false)
  })

  it('sends a test mail to the Owner by default and to a named address on request', async () => {
    apiMock.mockResolvedValue(undefined)
    const w = mount(AdminMailPanel, { props: { mail: on } })
    expect(w.get('[data-testid="admin-mail-on"]').text()).toContain('noreply@example.org')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/system/mail/test', { method: 'POST', json: {} })
    expect(useToastStore().toasts.at(-1)?.text).toBe('Test mail sent.')

    await w.get('[data-testid="admin-mail-recipient"]').setValue(' ops@example.org ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/system/mail/test', { method: 'POST', json: { to: 'ops@example.org' } })
  })

  it('shows the failing stage the server names, and the rate limit in plain words', async () => {
    const { ApiError } = await vi.importActual<typeof import('../lib/api')>('../lib/api')
    const w = mount(AdminMailPanel, { props: { mail: on } })
    apiMock.mockRejectedValueOnce(new ApiError(503, 'UNAVAILABLE', 'mail delivery failed (auth)'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('mail delivery failed (auth)')
    apiMock.mockRejectedValueOnce(new ApiError(429, 'RATE_LIMITED', 'too many requests'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('Too many test mails. Try again in a few minutes.')
  })
  it('asks for a recipient when the Owner has no address of their own', async () => {
    const { ApiError } = await vi.importActual<typeof import('../lib/api')>('../lib/api')
    const w = mount(AdminMailPanel, { props: { mail: on } })
    apiMock.mockRejectedValueOnce(new ApiError(400, 'INVALID_INPUT', 'add an e-mail address to your account or name a recipient'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.text).toBe('Enter a recipient above or add an e-mail address to your account.')
  })
})
