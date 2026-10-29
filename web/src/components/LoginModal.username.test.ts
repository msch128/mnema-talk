import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import LoginModal from './LoginModal.vue'
import { setLocale } from '../i18n'

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve({ password_reset: false })),
  onUnauthorized: vi.fn(() => () => true)
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

afterEach(() => {
  window.history.replaceState(null, '', '/')
})

describe('LoginModal username field', () => {
  it('normalises the name while the person types in the sign-up form', async () => {
    window.history.replaceState(null, '', '/?invite=abcdefghijkl')
    const w = mount(LoginModal, { global: { stubs: { LegalModal: true } } })
    const input = w.find<HTMLInputElement>('#login-username')
    await input.setValue('Max Muster')
    expect(input.element.value).toBe('max_muster')
    expect(w.find('[data-testid="username-hint"]').exists()).toBe(true)
  })

  it('leaves what is typed at sign-in alone, since older names keep capitals and dashes', async () => {
    const w = mount(LoginModal, { global: { stubs: { LegalModal: true } } })
    const input = w.find<HTMLInputElement>('#login-username')
    await input.setValue('Old-Timer')
    expect(input.element.value).toBe('Old-Timer')
    expect(w.find('[data-testid="username-hint"]').exists()).toBe(false)
  })
})
