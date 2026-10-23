import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountSecurityDialog from './AccountSecurityDialog.vue'
import { setLocale } from '../i18n'
import { useAccountStore } from '../stores/account'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

type Info = { email?: string; email_verified: boolean; mail_available: boolean }
const none: Info = { email_verified: false, mail_available: true }
const unconfirmed: Info = { email: 'me@example.org', email_verified: false, mail_available: true }

function respond(info: Info) {
  apiMock.mockImplementation(async (path) => (path === '/api/users/me/account' ? info : info))
}

async function open(info: Info) {
  respond(info)
  const w = mount(AccountSecurityDialog, { attachTo: document.body })
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  document.body.replaceChildren()
})

describe('AccountSecurityDialog e-mail section', () => {
  it('offers to add an address when there is none', async () => {
    const w = await open(none)
    expect(w.find('[data-testid="account-email-none"]').exists()).toBe(true)
    expect(w.find('[data-testid="account-email-remove"]').exists()).toBe(false)
    expect(w.get('[data-testid="account-email-edit"]').text()).toBe('Add address')
    w.unmount()
  })

  it('saves an address with the current password and says to check the inbox', async () => {
    const w = await open(none)
    await w.get('[data-testid="account-email-edit"]').trigger('click')
    await w.get('[data-testid="account-email-input"]').setValue(' me@example.org ')
    await w.get('[data-testid="account-email-password"]').setValue('my-password-123')
    apiMock.mockResolvedValueOnce(unconfirmed)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/email', expect.objectContaining({
      method: 'PUT', json: { email: 'me@example.org', current_password: 'my-password-123' }
    }))
    expect(w.get('[data-testid="account-email-address"]').text()).toBe('me@example.org')
    expect(w.get('[data-testid="account-email-state"]').text()).toBe('Not confirmed yet')
    expect(useToastStore().toasts.at(-1)?.text).toContain('Check your inbox')
    w.unmount()
  })

  it('keeps the form and shows the error when the password is wrong', async () => {
    const w = await open(unconfirmed)
    await w.get('[data-testid="account-email-edit"]').trigger('click')
    apiMock.mockRejectedValueOnce(new Error('The current password is wrong'))
    await w.get('[data-testid="account-email-password"]').setValue('nope')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('The current password is wrong')
    expect(w.find('form').exists()).toBe(true)
    w.unmount()
  })

  it('removes the address after asking for the password', async () => {
    const w = await open({ ...unconfirmed, email_verified: true })
    expect(w.get('[data-testid="account-email-state"]').text()).toBe('Confirmed')
    await w.get('[data-testid="account-email-remove"]').trigger('click')
    expect(w.find('[data-testid="account-email-input"]').exists()).toBe(false)
    await w.get('[data-testid="account-email-password"]').setValue('my-password-123')
    apiMock.mockResolvedValueOnce(none)
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/email/remove', expect.objectContaining({ method: 'POST', json: { current_password: 'my-password-123' } }))
    expect(w.find('[data-testid="account-email-none"]').exists()).toBe(true)
    w.unmount()
  })

  it('can ask for the confirmation mail again while the address is unconfirmed', async () => {
    const w = await open(unconfirmed)
    apiMock.mockResolvedValueOnce(undefined)
    await w.get('[data-testid="account-email-resend"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/users/me/email/resend', { method: 'POST' })
    expect(useToastStore().toasts.at(-1)?.text).toBe('Confirmation e-mail sent.')
    apiMock.mockRejectedValueOnce(new Error('Too many attempts'))
    await w.get('[data-testid="account-email-resend"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
    w.unmount()
  })

  it('explains that an address stays unused when the server cannot send mail', async () => {
    const w = await open({ ...unconfirmed, mail_available: false })
    expect(w.get('[data-testid="account-email-state"]').text()).toBe('Not used')
    expect(w.find('[data-testid="account-email-resend"]').exists()).toBe(false)
    w.unmount()
  })

  it('forgets the loaded state when closed', async () => {
    const w = await open(unconfirmed)
    const account = useAccountStore()
    account.open()
    expect(account.dialogOpen).toBe(true)
    account.close()
    expect(account.dialogOpen).toBe(false)
    expect(account.info).toBeNull()
    w.unmount()
  })

  it('closes from its close button and forgets what it loaded', async () => {
    const w = await open(unconfirmed)
    const account = useAccountStore()
    account.open()
    await w.get('[data-dialog-close]').trigger('click')
    expect(account.dialogOpen).toBe(false)
    expect(account.info).toBeNull()
    w.unmount()
  })

  it('shows why the state could not be loaded', async () => {
    apiMock.mockRejectedValue(new Error('Cannot reach the server'))
    const w = mount(AccountSecurityDialog, { attachTo: document.body })
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Cannot reach the server')
    w.unmount()
  })
})

describe('AccountSecurityDialog password', () => {
  it('keeps the password next to two-factor sign-in and the e-mail address', async () => {
    const w = await open(none)
    const text = w.text()
    expect(text).toContain('Password')
    expect(text.indexOf('Password')).toBeLessThan(text.indexOf('E-mail address'))
    await w.findAll('button').find(b => b.text() === 'Change')!.trigger('click')
    expect(w.find('input[autocomplete="current-password"]').exists()).toBe(true)
    w.unmount()
  })
})
