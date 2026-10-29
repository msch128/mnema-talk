import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountTwoFactor from './AccountTwoFactor.vue'
import QrCode from './QrCode.vue'
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

type Info = { email_verified: boolean; mail_available: boolean; two_factor_enabled: boolean; recovery_codes_remaining: number }
const off: Info = { email_verified: false, mail_available: false, two_factor_enabled: false, recovery_codes_remaining: 0 }
const on: Info = { ...off, two_factor_enabled: true, recovery_codes_remaining: 10 }
const CODES = Array.from({ length: 10 }, (_, i) => `abcd-efgh-jk${String(i).padStart(2, '0')}`.replace(/\d/g, 'm'))

function mountWith(info: Info) {
  const account = useAccountStore()
  account.info = info
  return mount(AccountTwoFactor)
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  useAuthStore().user = userFixture({ role: 'user' })
})
afterEach(() => vi.unstubAllGlobals())

describe('turning two-factor sign-in on', () => {
  it('walks through password, QR code, first code and recovery codes', async () => {
    const w = mountWith(off)
    expect(w.find('[data-testid="two-factor-recommend"]').exists()).toBe(false)
    await w.get('[data-testid="two-factor-start"]').trigger('click')

    apiMock.mockResolvedValueOnce({ secret: 'TESTKEY-NOT-REAL', otpauth_uri: 'otpauth://totp/Mnema%20Talk:max?secret=TESTKEY-NOT-REAL' })
    await w.get('[data-testid="two-factor-password"]').setValue('member-password-123')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/2fa/setup', expect.objectContaining({ method: 'POST', json: { current_password: 'member-password-123' } }))
    expect(w.get('[data-testid="two-factor-secret"]').text()).toBe('TESTKEY-NOT-REAL')
    expect(w.findComponent(QrCode).props('text')).toContain('otpauth://totp/')

    apiMock.mockImplementation(async path => (path === '/api/users/me/2fa/enable' ? { recovery_codes: CODES } : on))
    await w.get('[data-testid="two-factor-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/users/me/2fa/enable', expect.objectContaining({ json: { code: '123456' } }))
    expect(w.findAll('[data-testid="recovery-code-list"] li')).toHaveLength(10)

    // The codes can only be dismissed after confirming they were stored.
    expect(w.get('[data-testid="recovery-done"]').attributes('disabled')).toBeDefined()
    await w.get('[data-testid="recovery-saved"]').setValue(true)
    await w.get('[data-testid="recovery-done"]').trigger('click')
    expect(w.get('[data-testid="two-factor-state"]').text()).toContain('On')
    expect(w.get('[data-testid="two-factor-remaining"]').text()).toBe('10 recovery codes left')
    expect(w.find('[data-testid="recovery-code-list"]').exists()).toBe(false)
  })

  it('shows a wrong password or code and stays where it is', async () => {
    const w = mountWith(off)
    await w.get('[data-testid="two-factor-start"]').trigger('click')
    apiMock.mockRejectedValueOnce(new Error('The current password is wrong'))
    await w.get('[data-testid="two-factor-password"]').setValue('nope')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('The current password is wrong')

    apiMock.mockResolvedValueOnce({ secret: 'ABC', otpauth_uri: 'otpauth://totp/x?secret=ABC' })
    await w.get('form').trigger('submit')
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('That code did not work.'))
    await w.get('[data-testid="two-factor-code"]').setValue('111111')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('That code did not work.')
    expect(w.find('[data-testid="two-factor-secret"]').exists()).toBe(true)
    await w.get('button[type="button"]').trigger('click')
    expect(w.find('[data-testid="two-factor-start"]').exists()).toBe(true)
  })

  it('recommends it strongly to the Owner', () => {
    useAuthStore().user = userFixture({ role: 'admin', username: 'owner' })
    const w = mountWith(off)
    expect(w.get('[data-testid="two-factor-recommend"]').text()).toContain('strongly recommended')
    const withIt = mountWith(on)
    expect(withIt.find('[data-testid="two-factor-recommend"]').exists()).toBe(false)
  })
})

describe('with two-factor sign-in on', () => {
  it('turns it off with the password and a code', async () => {
    const w = mountWith(on)
    await w.get('[data-testid="two-factor-off"]').trigger('click')
    apiMock.mockResolvedValue(off)
    await w.get('[data-testid="two-factor-password"]').setValue('member-password-123')
    await w.get('[data-testid="two-factor-code"]').setValue('abcd-efgh-jkmm')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/users/me/2fa/disable', expect.objectContaining({ json: { current_password: 'member-password-123', code: 'abcd-efgh-jkmm' } }))
    expect(useToastStore().toasts.at(-1)?.text).toBe('Two-factor sign-in is off.')
    expect(w.find('[data-testid="two-factor-start"]').exists()).toBe(true)
  })

  it('makes new recovery codes and warns when few are left', async () => {
    const w = mountWith({ ...on, recovery_codes_remaining: 2 })
    expect(w.find('[data-testid="two-factor-low"]').exists()).toBe(true)
    await w.get('[data-testid="two-factor-renew"]').trigger('click')
    apiMock.mockImplementation(async path => (path === '/api/users/me/2fa/recovery-codes' ? { recovery_codes: CODES } : on))
    await w.get('[data-testid="two-factor-password"]').setValue('member-password-123')
    await w.get('[data-testid="two-factor-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.findAll('[data-testid="recovery-code-list"] li')).toHaveLength(10)
    await w.get('[data-testid="recovery-saved"]').setValue(true)
    await w.get('[data-testid="recovery-done"]').trigger('click')
    expect(w.find('[data-testid="two-factor-low"]').exists()).toBe(false)
  })

  it('shows the recovery codes even when refreshing the account fails', async () => {
    const w = mountWith(on)
    await w.get('[data-testid="two-factor-renew"]').trigger('click')
    apiMock.mockImplementation(async path => {
      if (path === '/api/users/me/2fa/recovery-codes') return { recovery_codes: CODES }
      throw new Error('offline')
    })
    await w.get('[data-testid="two-factor-password"]').setValue('member-password-123')
    await w.get('[data-testid="two-factor-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.findAll('[data-testid="recovery-code-list"] li')).toHaveLength(10)
  })

  it('shows a wrong code', async () => {
    const w = mountWith(on)
    await w.get('[data-testid="two-factor-off"]').trigger('click')
    apiMock.mockRejectedValueOnce(new Error('The code is incorrect'))
    await w.get('[data-testid="two-factor-password"]').setValue('member-password-123')
    await w.get('[data-testid="two-factor-code"]').setValue('000000')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('The code is incorrect')
  })
})

describe('recovery codes panel', () => {
  it('copies and downloads the codes', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const created: Blob[] = []
    vi.stubGlobal('URL', { createObjectURL: (b: Blob) => { created.push(b); return 'blob:x' }, revokeObjectURL: vi.fn() })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)

    const w = mountWith(off)
    await w.get('[data-testid="two-factor-start"]').trigger('click')
    apiMock.mockResolvedValueOnce({ secret: 'ABC', otpauth_uri: 'otpauth://totp/x?secret=ABC' })
    await w.get('[data-testid="two-factor-password"]').setValue('p')
    await w.get('form').trigger('submit')
    await flushPromises()
    apiMock.mockImplementation(async path => (path === '/api/users/me/2fa/enable' ? { recovery_codes: CODES } : on))
    await w.get('[data-testid="two-factor-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()

    await w.get('[data-testid="recovery-copy"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(CODES.join('\n'))
    await w.get('[data-testid="recovery-download"]').trigger('click')
    expect(click).toHaveBeenCalled()
    expect(await created[0]?.text()).toContain(CODES[0])

    writeText.mockRejectedValueOnce(new Error('denied'))
    await w.get('[data-testid="recovery-copy"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    click.mockRestore()
  })
})

describe('QrCode', () => {
  it('draws the text as an accessible SVG', () => {
    const w = mount(QrCode, { props: { text: 'otpauth://totp/x?secret=ABC', label: 'QR' } })
    expect(w.get('svg').attributes('aria-label')).toBe('QR')
    expect(w.get('path').attributes('d')?.length).toBeGreaterThan(100)
  })
})
