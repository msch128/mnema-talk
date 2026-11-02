import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import InviteLanding from './InviteLanding.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(() => () => true)
}))

const stubs = { LegalModal: true }

async function landing(path: string) {
  window.history.replaceState(null, '', path)
  const w = mount(InviteLanding, { global: { stubs } })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

afterEach(() => {
  window.history.replaceState(null, '', '/')
})

describe('InviteLanding', () => {
  it('is just the login dialog away from an invite link and asks the server nothing', async () => {
    const w = await landing('/')
    // Only the sign-in options are asked for, nothing about an invite.
    expect(apiMock.mock.calls.filter(([path]) => path !== '/api/auth/options')).toEqual([])
    expect(w.find('[data-testid="login-heading"]').text()).toBe('Mnema Talk')
    expect(w.find('#login-invite').exists()).toBe(false)
  })

  it('names the community, fills in the code and shows the expiry for a working invite', async () => {
    apiMock.mockResolvedValue({ valid: true, community_name: 'Test Home', expires_at: '2030-01-02T10:00:00Z' })
    const w = await landing('/invite/abc234xyz789')
    expect(apiMock).toHaveBeenCalledWith('/api/invites/abc234xyz789', { decode: expect.any(Function) })
    expect(w.find('[data-testid="login-heading"]').text()).toBe('Test Home')
    expect(w.find<HTMLInputElement>('#login-invite').element.value).toBe('abc234xyz789')
    expect(w.find('[data-testid="invite-expiry"]').exists()).toBe(true)
    expect(w.find('[data-testid="invite-invalid"]').exists()).toBe(false)
  })

  it('keeps the product name when the community has none, and shows no expiry for a code that never expires', async () => {
    apiMock.mockResolvedValue({ valid: true })
    const w = await landing('/invite/abc234xyz789')
    expect(w.find('[data-testid="login-heading"]').text()).toBe('Mnema Talk')
    expect(w.find('[data-testid="invite-expiry"]').exists()).toBe(false)
  })

  it('falls back to sign-in with a notice when the code is dead', async () => {
    apiMock.mockResolvedValue({ valid: false })
    const w = await landing('/invite/abc234xyz789')
    expect(w.find('[data-testid="invite-invalid"]').exists()).toBe(true)
    expect(w.find('#login-invite').exists()).toBe(false)
    expect(w.find('[data-testid="login-heading"]').text()).toBe('Mnema Talk')
  })

  it('still offers the form with the code when the server cannot be asked', async () => {
    apiMock.mockRejectedValue(new Error('offline'))
    const w = await landing('/invite/abc234xyz789')
    expect(w.find<HTMLInputElement>('#login-invite').element.value).toBe('abc234xyz789')
    expect(w.find('[data-testid="invite-invalid"]').exists()).toBe(false)
  })

  describe('vanity link', () => {
    it('opens the sign-up form with the link as the code when it takes sign-ups', async () => {
      apiMock.mockResolvedValue({ valid: true, community_name: 'Owls', accepts_signups: true })
      const w = await landing('/join/Owls')
      expect(apiMock).toHaveBeenCalledWith('/api/join/owls', { decode: expect.any(Function) })
      expect(w.find('[data-testid="login-heading"]').text()).toBe('Owls')
      expect(w.find('#login-invite').exists()).toBe(false)
      expect(w.find('[data-testid="invite-invalid"]').exists()).toBe(false)
      expect(w.find('button[type="submit"]').text()).toBe('Register')
    })

    it('greets the visitor on the sign-in page when the link takes no sign-ups', async () => {
      apiMock.mockResolvedValue({ valid: true, community_name: 'Owls' })
      const w = await landing('/join/owls')
      expect(w.find('[data-testid="login-heading"]').text()).toBe('Owls')
      expect(w.text()).toContain('You were invited to Owls')
      expect(w.find('#login-invite').exists()).toBe(false)
      expect(w.find('button[type="submit"]').text()).toBe('Sign in')
    })

    it('is the plain login dialog for an unknown link, and when the server cannot be asked', async () => {
      apiMock.mockResolvedValue({ valid: false })
      let w = await landing('/join/nobody')
      expect(w.find('[data-testid="login-heading"]').text()).toBe('Mnema Talk')
      expect(w.text()).not.toContain('nobody')
      apiMock.mockRejectedValue(new Error('offline'))
      w = await landing('/join/owls')
      expect(w.find('[data-testid="login-heading"]').exists()).toBe(true)
      expect(w.find('[data-testid="invite-invalid"]').exists()).toBe(false)
    })
  })
})
