import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SettingsDialog from './SettingsDialog.vue'
import { setLocale } from '../i18n'
import { useAccountStore } from '../stores/account'
import { settingsTab } from '../lib/settingsDialog'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

type Info = { email?: string; email_verified: boolean; mail_available: boolean }
const none: Info = { email_verified: false, mail_available: true }
const unconfirmed: Info = { email: 'me@example.org', email_verified: false, mail_available: true }

function respond(info: Info) {
  apiMock.mockImplementation(async (path) => (path === '/api/auth/sessions' ? { sessions: [] } : info))
}

async function open(info: Info) {
  respond(info)
  const w = mount(SettingsDialog, { attachTo: document.body })
  await flushPromises()
  return w
}

beforeEach(() => {
  localStorage.clear()
  settingsTab.value = 'signin'
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  document.body.replaceChildren()
})

describe('SettingsDialog e-mail section', () => {
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

  it('says in the app which field is missing instead of leaving it to the browser', async () => {
    const w = await open(none)
    await w.get('[data-testid="account-email-edit"]').trigger('click')
    expect(w.get('form').attributes('novalidate')).toBeDefined()
    await w.get('[data-testid="account-email-input"]').setValue('not-an-address')
    await w.get('[data-testid="account-email-password"]').setValue('my-password-123')
    await w.get('form').trigger('submit')
    expect(w.get('[role="alert"]').text()).toBe('That does not look like an e-mail address.')
    await w.get('[data-testid="account-email-input"]').setValue('me@example.org')
    await w.get('[data-testid="account-email-password"]').setValue('')
    await w.get('form').trigger('submit')
    expect(w.get('[role="alert"]').text()).toBe('Enter your current password to confirm.')
    expect(apiMock).not.toHaveBeenCalledWith('/api/users/me/email', expect.anything())
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
    const w = mount(SettingsDialog, { attachTo: document.body })
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toBe('Cannot reach the server')
    w.unmount()
  })
})

describe('SettingsDialog password', () => {
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

describe('SettingsDialog sections', () => {
  const stubs = { AudioSettingsPanel: true, AppSettingsPanel: true, AccountPrivacy: true, AccountData: true, AccountSessions: true }

  async function openAt(tab: typeof settingsTab.value) {
    settingsTab.value = tab
    respond(none)
    const w = mount(SettingsDialog, { attachTo: document.body, global: { stubs } })
    await flushPromises()
    return w
  }

  it('shows one section at a time instead of one long scroll', async () => {
    const w = await openAt('signin')
    expect(w.find('[data-testid="account-password"]').exists()).toBe(true)
    expect(w.findComponent({ name: 'AccountSessions' }).exists()).toBe(false)
    await w.get('[data-testid="settings-tab-sessions"]').trigger('click')
    expect(w.findComponent({ name: 'AccountSessions' }).exists()).toBe(true)
    expect(w.find('[data-testid="account-password"]').exists()).toBe(false)
    expect(settingsTab.value).toBe('sessions')
    w.unmount()
  })

  it('opens on the section it was asked for and marks it selected', async () => {
    const w = await openAt('data')
    expect(w.get('[data-testid="settings-tab-data"]').attributes('aria-selected')).toBe('true')
    expect(w.get('[data-testid="settings-tab-signin"]').attributes('aria-selected')).toBe('false')
    expect(w.get('[role="tabpanel"]').attributes('aria-labelledby')).toBe('settings-tab-data')
    expect(w.findComponent({ name: 'AccountData' }).exists()).toBe(true)
    w.unmount()
  })

  it('moves between sections with the arrow keys, wrapping at both ends', async () => {
    const w = await openAt('signin')
    const list = w.get('[role="tablist"]')
    await list.trigger('keydown', { key: 'ArrowDown' })
    expect(settingsTab.value).toBe('sessions')
    await list.trigger('keydown', { key: 'End' })
    expect(settingsTab.value).toBe('data')
    await list.trigger('keydown', { key: 'ArrowRight' })
    expect(settingsTab.value).toBe('audio')
    await list.trigger('keydown', { key: 'ArrowUp' })
    expect(settingsTab.value).toBe('data')
    await list.trigger('keydown', { key: 'Home' })
    expect(settingsTab.value).toBe('audio')
    await list.trigger('keydown', { key: 'x' })
    expect(settingsTab.value).toBe('audio')
    w.unmount()
  })

  it('waits for the account only where it is needed', async () => {
    apiMock.mockImplementation(() => new Promise(() => undefined))
    settingsTab.value = 'sessions'
    const w = mount(SettingsDialog, { attachTo: document.body, global: { stubs } })
    await flushPromises()
    expect(w.findComponent({ name: 'AccountSessions' }).exists()).toBe(true)
    await w.get('[data-testid="settings-tab-signin"]').trigger('click')
    expect(w.get('[role="status"]').text()).toBe('Loading…')
    w.unmount()
  })
})

describe('SettingsDialog jump targets', () => {
  it('puts the cursor on the field a notice asked for, in the section it asked for', async () => {
    apiMock.mockImplementation(async () => ({ sessions: [] }))
    const { openSettings, settingsField } = await import('../lib/settingsDialog')
    openSettings('audio', { field: 'input-device' })
    const w = mount(SettingsDialog, { attachTo: document.body, global: { directives: { tooltip: {} } } })
    await flushPromises()
    await vi.waitFor(() => expect(settingsField.value).toBeNull())
    expect(w.get('[data-testid="settings-tab-audio"]').attributes('aria-selected')).toBe('true')
    const field = document.querySelector('[data-settings-field="input-device"]')
    expect(field).not.toBeNull()
    expect(document.activeElement).toBe(field)
    w.unmount()
  })

  it('forgets the requested field when the person picks a section themselves', async () => {
    apiMock.mockImplementation(async () => ({ sessions: [] }))
    const { openSettings, settingsField } = await import('../lib/settingsDialog')
    const w = await open(none)
    openSettings('signin', { field: 'noise' })
    await w.get('[data-testid="settings-tab-sessions"]').trigger('click')
    expect(settingsField.value).toBeNull()
    w.unmount()
  })
})

describe('SettingsDialog Roles section', () => {
  const closedRole = { id: '00000000-0000-4000-8000-000000000041', name: 'Mods', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }

  it('appears only where an admin opened a role to everyone, and shows the choices', async () => {
    usePermissionsStore().roles = [closedRole]
    let w = await open(none)
    expect(w.find('[data-testid="settings-tab-roles"]').exists()).toBe(false)
    w.unmount()
    document.body.replaceChildren()

    usePermissionsStore().roles = [{ ...closedRole, self_assignable: true }]
    apiMock.mockImplementation(async (path) => (path === '/api/roles/self-assignable' ? [{ ...closedRole, self_assignable: true }] : none))
    w = mount(SettingsDialog, { attachTo: document.body })
    await flushPromises()
    await w.get('[data-testid="settings-tab-roles"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="self-roles"]').text()).toContain('Mods')
    w.unmount()
  })
})
