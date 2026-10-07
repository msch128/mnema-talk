import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { setLocale, locale } from '../i18n'
import { useAuthStore } from '../stores/auth'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

const user = { bio: '', status_text: '', locale: 'en', created_at: '2026-01-01T00:00:00Z',  id: "00000000-0000-4000-8000-0000000003e8", username: 'max', display_name: 'Max', role: 'user' }

function mountLogin() {
  return mount(LoginModal, { global: { stubs: { LegalModal: { template: '<button data-testid="legal" @click="$emit(\'close\')">Close</button>' } } } })
}

const submit = (w: VueWrapper) => w.find('form')!.trigger('submit')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

afterEach(() => {
  window.history.replaceState(null, '', '/')
})

describe('LoginModal', () => {
  it('signs in with username and password', async () => {
    apiMock.mockImplementation(url => Promise.resolve(url === '/api/auth/login' ? { user } : user))
    const w = mountLogin()
    expect(w.find<HTMLInputElement>('#login-invite')!.exists()).toBe(false)
    await w.find<HTMLInputElement>('#login-username')!.setValue('max')
    await w.find('#login-password')!.setValue('secret-password')
    await submit(w)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/auth/login', { decode: expect.any(Function),  method: 'POST', json: { username: 'max', password: 'secret-password' } })
    expect(useAuthStore().user).toEqual(user)
    expect(w.find('[role="alert"]')!.exists()).toBe(false)
  })

  it('shows the server error and re-enables the button', async () => {
    let reject: (reason?: unknown) => void = () => { throw new Error("Request was not started") }
    apiMock.mockReturnValue(new Promise((_, r) => { reject = r }))
    const w = mountLogin()
    await w.find<HTMLInputElement>('#login-username')!.setValue('max')
    await w.find('#login-password')!.setValue('wrong')
    await submit(w)
    const button = w.find('button[type="submit"]')!
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.text()).toBe('One moment …')
    reject(new Error('invalid username or password'))
    await flushPromises()
    expect(w.find('[role="alert"]')!.text()).toBe('invalid username or password')
    expect(button.attributes('disabled')).toBeUndefined()
    expect(button.text()).toBe('Sign in')
  })

  it('falls back to a generic message for errors without text', async () => {
    apiMock.mockRejectedValue(new Error(''))
    const w = mountLogin()
    await submit(w)
    await flushPromises()
    expect(w.find('[role="alert"]')!.text()).toBe("That didn't work. Please try again.")
  })

  it('registers with an invite and uses the username as display name by default', async () => {
    apiMock.mockImplementation(url => Promise.resolve(url === '/api/auth/register' ? { user } : user))
    const w = mountLogin()
    await w.findAll('button').find(b => b.text() === 'Have an invite code? Register here')!.trigger('click')
    expect(w.find<HTMLInputElement>('#login-invite')!.exists()).toBe(true)
    expect(w.find('button[type="submit"]')!.text()).toBe('Register')
    await w.find<HTMLInputElement>('#login-username')!.setValue('max')
    await w.find('#login-password')!.setValue('member-password-123')
    await w.find<HTMLInputElement>('#login-invite')!.setValue('team-code')
    await submit(w)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/auth/register', { decode: expect.any(Function),
      method: 'POST',
      json: { username: 'max', display_name: 'max', password: 'member-password-123', invite_code: 'team-code' }
    })
  })

  it('requires an invite code before registering', async () => {
    const w = mountLogin()
    await w.findAll('button').find(b => b.text().startsWith('Have an invite code'))!.trigger('click')
    await w.find<HTMLInputElement>('#login-invite')!.setValue('   ')
    await submit(w)
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()
    expect(w.find('[role="alert"]')!.text()).toBe('Please enter an invite code.')

    // Switching back to sign-in clears the error.
    await w.findAll('button').find(b => b.text().startsWith('Already registered'))!.trigger('click')
    expect(w.find('[role="alert"]')!.exists()).toBe(false)
    expect(w.find<HTMLInputElement>('#login-invite')!.exists()).toBe(false)
  })

  it('opens registration prefilled from an invite link', async () => {
    window.history.replaceState(null, '', '/?invite=from-link')
    const w = mountLogin()
    await flushPromises()
    expect(w.find<HTMLInputElement>('#login-invite')!.element.value).toBe('from-link')
    expect(w.find<HTMLInputElement>('#login-displayname')!.exists()).toBe(true)
  })

  it('also reads the invite from the hash', async () => {
    window.history.replaceState(null, '', '/#/join?invite=hash-code')
    const w = mountLogin()
    await flushPromises()
    expect(w.find<HTMLInputElement>('#login-invite')!.element.value).toBe('hash-code')
  })

  it('switches the language and opens the legal notice', async () => {
    const w = mountLogin()
    const de = w.find('button[lang="de"]')!
    await de.trigger('click')
    expect(locale.value).toBe('de')
    expect(de.attributes('aria-pressed')).toBe('true')
    expect(w.find('[data-testid="legal"]')!.exists()).toBe(false)
    await w.findAll('button').at(-1)!.trigger('click')
    expect(w.find('[data-testid="legal"]')!.exists()).toBe(true)
  })
})

it('saves an explicit registration display name and closes both legal entrypoints', async () => {
  apiMock.mockImplementation(url => Promise.resolve(url === '/api/auth/register' ? { user } : user))
  const w = mountLogin()
  await w.findAll('button').find(b => b.text().startsWith('Have an invite code'))!.trigger('click')
  await w.find('#login-username').setValue('max'); await w.find('#login-displayname').setValue('Max Display')
  await w.find('#login-password').setValue('member-password-123'); await w.find('#login-invite').setValue('team-code')
  await w.find('form button[type="button"]').trigger('click')
  await w.find('[data-testid="legal"]').trigger('click'); expect(w.find('[data-testid="legal"]').exists()).toBe(false)
  await submit(w); await flushPromises()
  expect(apiMock).toHaveBeenCalledWith('/api/auth/register', expect.objectContaining({ json: expect.objectContaining({ display_name: 'Max Display' }) }))
  w.unmount()
})
