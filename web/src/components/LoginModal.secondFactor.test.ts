import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(() => () => true)
}))

const user = { bio: '', status_text: '', locale: 'en', created_at: '2026-01-01T00:00:00Z', id: '00000000-0000-4000-8000-0000000003e8', username: 'max', display_name: 'Max', role: 'user' }
const CHALLENGE = 'C'.repeat(43)
const stubs = { LegalModal: true, ForgotPasswordDialog: true }

// The real api() runs the decoder over the body; the mock has to as well.
const answer = (body: unknown) => async (_path: string, options?: ApiOptions) => {
  const decode = (options as { decode?: (v: unknown) => unknown } | undefined)?.decode
  return decode ? decode(body) : body
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

async function passwordStep() {
  apiMock.mockImplementation(answer({ two_factor_required: true, challenge: CHALLENGE, expires_in: 300 }))
  const w = mount(LoginModal, { global: { stubs } })
  await w.get('#login-username').setValue('max')
  await w.get('#login-password').setValue('member-password-123')
  await w.get('form').trigger('submit')
  await flushPromises()
  return w
}

describe('LoginModal second factor', () => {
  it('asks for the code after the password and signs in with it', async () => {
    const w = await passwordStep()
    expect(w.find('#login-password').exists()).toBe(false)
    expect(useAuthStore().user).toBeNull()

    apiMock.mockImplementationOnce(answer({ user }))
    await w.get('[data-testid="second-factor-code"]').setValue('123 456')
    await w.get('[data-testid="second-factor"]').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/auth/login/2fa', expect.objectContaining({ method: 'POST', json: { challenge: CHALLENGE, code: '123 456' } }))
    expect(useAuthStore().user).toEqual(user)
    expect(useToastStore().toasts).toHaveLength(0)
    w.unmount()
  })

  it('shows a security notice when wrong codes were entered since the last sign-in', async () => {
    const w = await passwordStep()
    apiMock.mockImplementationOnce(answer({ user, security_notice: { failed_attempts: 6 } }))
    await w.get('[data-testid="second-factor-code"]').setValue('654321')
    await w.get('[data-testid="second-factor"]').trigger('submit')
    await flushPromises()
    const toast = useToastStore().toasts.at(-1)
    expect(toast?.type).toBe('error')
    expect(toast?.text).toContain('6 wrong codes')
    w.unmount()
  })

  it('keeps the step after a wrong code and goes back to the password on request', async () => {
    const w = await passwordStep()
    apiMock.mockRejectedValueOnce(new Error('That code did not work.'))
    await w.get('[data-testid="second-factor-code"]').setValue('000000')
    await w.get('[data-testid="second-factor"]').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('That code did not work.')
    expect(useAuthStore().user).toBeNull()

    await w.get('[data-testid="second-factor-back"]').trigger('click')
    expect(w.find('#login-password').exists()).toBe(true)
    expect(w.find('[data-testid="second-factor"]').exists()).toBe(false)
    w.unmount()
  })

  it('drops the step when switching to registration', async () => {
    const w = await passwordStep()
    await w.findAll('button').find(b => b.text().startsWith('Have an invite code'))!.trigger('click')
    expect(w.find('[data-testid="second-factor"]').exists()).toBe(false)
    w.unmount()
  })
})
