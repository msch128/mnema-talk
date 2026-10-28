import type { ApiOptions } from '../lib/api'
import { userFixture, requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ProfilePassword from './ProfilePassword.vue'
import { useAuthStore } from '../stores/auth'
import { useAccountStore } from '../stores/account'
import { t, setLocale } from '../i18n'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

let wrapper: VueWrapper

beforeEach(() => {
  setLocale('de')
  mockApi.mockReset()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: '00000000-0000-4000-8000-0000000003e8', username: 'ada', role: 'user' })
  wrapper = mount(ProfilePassword, { attachTo: document.body })
})

afterEach(() => {
  wrapper.unmount()
  document.body.innerHTML = ''
})

const button = (key: Parameters<typeof t>[0]) => requireValue(wrapper.findAll('button').find(b => b.text() === t(key)), `Missing ${key} button`)

async function fill(password: string, repeated = password) {
  await button('profile.changePassword').trigger('click')
  await wrapper.get('input[autocomplete="current-password"]').setValue('previous-password')
  await wrapper.get('input[autocomplete="new-password"]').setValue(password)
  await requireValue(wrapper.findAll('input[autocomplete="new-password"]')[1]).setValue(repeated)
}

it('matches the server password minimum', () => {
  expect(MIN_PASSWORD_LENGTH).toBe(10)
})

describe('changing the password', () => {
  it('rejects passwords shorter than the server minimum', async () => {
    await fill('a'.repeat(MIN_PASSWORD_LENGTH - 1))
    const field = wrapper.get('input[autocomplete="new-password"]')
    expect(wrapper.get(`label[for="${field.attributes('id')}"]`).text()).toBe(`Neues Passwort (mind. ${MIN_PASSWORD_LENGTH} Zeichen)`)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toBe(`Das neue Passwort braucht mindestens ${MIN_PASSWORD_LENGTH} Zeichen.`)
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('rejects a repeated password that does not match before contacting the server', async () => {
    await fill('long-password', 'different-password')
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toBe(t('profile.passwordMismatch'))
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('submits a matching password and closes the editor', async () => {
    await fill('valid-password')
    mockApi.mockResolvedValue({ ok: true })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/auth/password', expect.objectContaining({
      json: { current_password: 'previous-password', new_password: 'valid-password' }
    }))
    expect(wrapper.find('input[autocomplete="current-password"]').exists()).toBe(false)
  })

  it.each([new Error('Wrong current password'), {}])('shows a failure and allows a retry (%s)', async error => {
    await fill('valid-password')
    mockApi.mockRejectedValue(error)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(error instanceof Error ? error.message : t('profile.saveFailed'))
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
  })

  it('cancels without making a request', async () => {
    await fill('valid-password')
    await button('common.cancel').trigger('click')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('asks for a current code while two-factor sign-in is on and sends it along', async () => {
    useAccountStore().info = { email_verified: false, mail_available: false, recovery_codes_remaining: 8, two_factor_enabled: true }
    await fill('valid-password')
    mockApi.mockResolvedValue({ ok: true })
    await wrapper.get('input[data-testid="password-code"]').setValue(' 123456 ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/auth/password', expect.objectContaining({
      json: { current_password: 'previous-password', new_password: 'valid-password', code: '123456' }
    }))
  })

  it('shows no code field without two-factor sign-in', async () => {
    await fill('valid-password')
    expect(wrapper.find('input[data-testid="password-code"]').exists()).toBe(false)
  })
})
