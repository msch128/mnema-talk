import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountData from './AccountData.vue'
import { setLocale } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { userFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const none = { state: 'none', size_bytes: 0 }
const info = { email_verified: false, mail_available: false, two_factor_enabled: false, recovery_codes_remaining: 0 }

function mountWith(status: unknown, opts: { owner?: boolean; twoFactor?: boolean } = {}) {
  useAuthStore().user = userFixture({ role: opts.owner ? 'admin' : 'user', username: 'max' })
  useAccountStore().info = { ...info, two_factor_enabled: !!opts.twoFactor }
  apiMock.mockResolvedValue(status)
  return mount(AccountData)
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('data export', () => {
  it('asks for the password, starts the export and follows it to the download', async () => {
    const w = mountWith(none)
    await flushPromises()
    await w.get('[data-testid="export-start"]').trigger('click')
    expect(w.find('[data-testid="privacy-code"]').exists()).toBe(false)
    await w.get('[data-testid="privacy-password"]').setValue('member-password-123')
    apiMock.mockResolvedValue({ state: 'pending', size_bytes: 0, requested_at: '2026-10-22T10:00:00Z' })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/users/me/export', expect.objectContaining({ method: 'POST', json: { current_password: 'member-password-123' } }))
    expect(w.find('[data-testid="export-pending"]').exists()).toBe(true)
    expect(useToastStore().toasts.at(-1)?.text).toBe('Your export is being prepared.')

    apiMock.mockResolvedValue({ state: 'ready', size_bytes: 1234, expires_at: '2026-10-23T10:00:00Z', next_request_at: '2099-01-01T00:00:00Z' })
    await vi.advanceTimersByTimeAsync(2100)
    await flushPromises()
    const link = w.get('[data-testid="export-download"]')
    expect(link.attributes('href')).toBe('/api/users/me/export/download')

    // After the download the file is gone; the next one waits for the day to pass.
    apiMock.mockResolvedValue({ state: 'downloaded', size_bytes: 1234, next_request_at: '2099-01-01T00:00:00Z' })
    await link.trigger('click')
    await vi.advanceTimersByTimeAsync(1600)
    await flushPromises()
    expect(w.get('[data-testid="export-done"]').text()).toContain('downloaded')
    expect(w.find('[data-testid="export-start"]').exists()).toBe(false)
    expect(w.get('[data-testid="export-wait"]').text()).toContain('next export')
  })

  it('asks for a code when two-factor sign-in is on and shows a refusal', async () => {
    const w = mountWith(none, { twoFactor: true })
    await flushPromises()
    await w.get('[data-testid="export-start"]').trigger('click')
    await w.get('[data-testid="privacy-password"]').setValue('pw')
    await w.get('[data-testid="privacy-code"]').setValue('123456')
    apiMock.mockRejectedValueOnce(new Error('The code is incorrect'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/export', expect.objectContaining({ json: { current_password: 'pw', code: '123456' } }))
    expect(w.get('[role="alert"]').text()).toBe('The code is incorrect')
    await w.findAll('button[type="button"]').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.find('[data-testid="export-start"]').exists()).toBe(true)
  })

  it('reports a failed export and an expired one', async () => {
    let w = mountWith({ state: 'failed', size_bytes: 0 })
    await flushPromises()
    expect(w.find('[data-testid="export-failed"]').exists()).toBe(true)
    w = mountWith({ state: 'expired', size_bytes: 0 })
    await flushPromises()
    expect(w.get('[data-testid="export-done"]').text()).toContain('expired')
  })

  it('survives a status that cannot be read', async () => {
    useAuthStore().user = userFixture({ role: 'user', username: 'max' })
    useAccountStore().info = info
    apiMock.mockRejectedValue(new Error('offline'))
    const w = mount(AccountData)
    await flushPromises()
    expect(w.find('[data-testid="delete-account"]').exists()).toBe(true)
  })
})

describe('account deletion', () => {
  it('needs the typed username and the password, then signs out and reloads', async () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    const w = mountWith(none)
    await flushPromises()
    await w.get('[data-testid="delete-start"]').trigger('click')
    expect(w.get('[data-testid="delete-submit"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="delete-name"]').setValue('Max')
    await w.get('[data-testid="privacy-password"]').setValue('member-password-123')
    expect(w.get('[data-testid="delete-submit"]').attributes('disabled')).toBeUndefined()

    apiMock.mockResolvedValue(undefined)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/delete', { method: 'POST', json: { current_password: 'member-password-123' } })
    expect(useAuthStore().user).toBeNull()
    expect(assign).toHaveBeenCalledWith('/')
  })

  it('keeps everything when the password is wrong', async () => {
    const w = mountWith(none, { twoFactor: true })
    await flushPromises()
    await w.get('[data-testid="delete-start"]').trigger('click')
    await w.get('[data-testid="delete-name"]').setValue('max')
    await w.get('[data-testid="privacy-password"]').setValue('nope')
    await w.get('[data-testid="privacy-code"]').setValue('000000')
    apiMock.mockRejectedValueOnce(new Error('The current password is wrong'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('The current password is wrong')
    expect(useAuthStore().user).not.toBeNull()
  })

  it('offers the Owner no delete button and says why', async () => {
    const w = mountWith(none, { owner: true })
    await flushPromises()
    expect(w.find('[data-testid="delete-start"]').exists()).toBe(false)
    expect(w.get('[data-testid="delete-owner"]').text()).toContain('cannot be deleted')
  })
})
