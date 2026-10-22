import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(() => () => true)
}))

const stubs = { LegalModal: true, ForgotPasswordDialog: { template: '<div data-testid="forgot-password" />' } }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('LoginModal password reset link', () => {
  it('offers "Forgot your password?" only when the server can send mail', async () => {
    apiMock.mockResolvedValue({ password_reset: false })
    let w = mount(LoginModal, { global: { stubs } })
    await flushPromises()
    expect(w.find('[data-testid="forgot-password-link"]').exists()).toBe(false)
    w.unmount()

    apiMock.mockResolvedValue({ password_reset: true })
    w = mount(LoginModal, { global: { stubs } })
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/auth/options', { decode: expect.any(Function) })
    expect(w.get('[data-testid="forgot-password-link"]').text()).toBe('Forgot your password?')
    await w.get('[data-testid="forgot-password-link"]').trigger('click')
    expect(w.find('[data-testid="forgot-password"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps the link off the registration form and survives an unreachable server', async () => {
    apiMock.mockResolvedValue({ password_reset: true })
    const w = mount(LoginModal, { global: { stubs } })
    await flushPromises()
    await w.findAll('button').find(b => b.text().startsWith('Have an invite code'))!.trigger('click')
    expect(w.find('[data-testid="forgot-password-link"]').exists()).toBe(false)
    w.unmount()

    apiMock.mockRejectedValue(new Error('offline'))
    const offline = mount(LoginModal, { global: { stubs } })
    await flushPromises()
    expect(offline.find('#login-username').exists()).toBe(true)
    offline.unmount()
  })
})
