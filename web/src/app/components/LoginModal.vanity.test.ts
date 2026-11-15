import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { setLocale, t } from '../../i18n'

const apiMock = vi.fn<(path: string) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (path: string) => apiMock(path),
  onUnauthorized: vi.fn(() => () => true)
}))

async function signUp(rejection: Error) {
  apiMock.mockImplementation(path => (path === '/api/auth/register' ? Promise.reject(rejection) : Promise.resolve({ password_reset: false })))
  const w = mount(LoginModal, {
    props: { invite: { code: '~friends', valid: true, communityName: '', expiresAt: '', vanity: true } },
    global: { stubs: { LegalModal: true } }
  })
  await w.find('#login-username').setValue('max')
  await w.find('#login-password').setValue('member-password-123')
  await w.find('form').trigger('submit')
  await flushPromises()
  return w
}

beforeEach(async () => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('LoginModal vanity sign-up', () => {
  it('keeps the invite hidden when the name is taken', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../shared/lib/api')>('../../shared/lib/api')
    const w = await signUp(new ApiError(409, 'CONFLICT', 'username is already taken'))
    expect(w.find('#login-invite').exists()).toBe(false)
    expect(w.find('[role="alert"]').text()).toContain('taken')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock.mock.calls.filter(c => c[0] === '/api/auth/register')).toHaveLength(2)
  })

  it('asks for a code when the server refuses the invite itself', async () => {
    const { ApiError } = await vi.importActual<typeof import('../../shared/lib/api')>('../../shared/lib/api')
    const w = await signUp(new ApiError(400, 'INVALID_INPUT', t('errors.detail.invalidInvite')))
    expect(w.find('#login-invite').exists()).toBe(true)
  })

  it('starts in register mode with an invited subtitle for an active vanity link', async () => {
    apiMock.mockResolvedValue({ password_reset: false })
    const w = mount(LoginModal, {
      props: { invite: { code: '~friends', valid: true, communityName: 'Friends', expiresAt: '', vanity: true } },
      global: { stubs: { LegalModal: true } }
    })
    await flushPromises()
    expect(w.find('button[type="submit"]').text()).toBe('Register')
    expect(w.text()).toContain('You’ve been invited to join')
    expect(w.find('#login-invite').exists()).toBe(false)
  })
})
