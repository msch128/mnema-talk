import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountSessions from './AccountSessions.vue'
import { setLocale } from '../i18n'
import { pendingConfirm } from '../lib/confirm'
import { useAccountStore } from '../stores/account'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const NOW = Date.now()
const ago = (ms: number) => new Date(NOW - ms).toISOString()
const laptop = { id: '00000000-0000-4000-8000-000000000001', device: 'Firefox · Linux', created_at: ago(9 * 86_400_000), last_active_at: ago(60_000), current: true, push: false }
const phone = { id: '00000000-0000-4000-8000-000000000002', device: 'Safari · iOS', created_at: ago(3 * 86_400_000), last_active_at: ago(2 * 86_400_000), current: false, push: true }
const unknown = { id: '00000000-0000-4000-8000-000000000003', device: '', created_at: ago(86_400_000), last_active_at: ago(3_600_000), current: false, push: false }

let sessions: unknown[]

async function open() {
  const w = mount(AccountSessions)
  await flushPromises()
  return w
}

// The confirm dialog is hosted elsewhere; answer whatever is pending.
async function answer(yes: boolean) {
  await flushPromises()
  pendingConfirm.value?.resolve(yes)
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  pendingConfirm.value = null
  sessions = [laptop, phone]
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/auth/sessions') return { sessions }
    if (path === '/api/auth/sessions/revoke-others') return { revoked: 2 }
    void options
    return undefined
  })
})

describe('AccountSessions', () => {
  it('lists the devices with this one marked and push shown', async () => {
    const w = await open()
    const rows = w.findAll('li[data-session]')
    expect(rows.map(r => r.attributes('data-session'))).toEqual(['Firefox · Linux', 'Safari · iOS'])
    expect(rows[0]!.find('[data-testid="session-current"]').text()).toBe('This device')
    expect(rows[0]!.find('[data-testid="session-push"]').exists()).toBe(false)
    expect(rows[1]!.find('[data-testid="session-current"]').exists()).toBe(false)
    expect(rows[1]!.find('[data-testid="session-push"]').text()).toBe('Notifications on')
    expect(rows[0]!.text()).toContain('just now')
    expect(rows[1]!.text()).toContain('2 days ago')
  })

  it('names a device the server could not recognise', async () => {
    sessions = [laptop, unknown]
    const w = await open()
    expect(w.findAll('li[data-session]')[1]!.attributes('data-session')).toBe('Unknown device')
  })

  it('signs out one other device after confirming and reloads the list', async () => {
    const w = await open()
    sessions = [laptop]
    await w.findAll('[data-testid="session-sign-out"]')[1]!.trigger('click')
    await flushPromises()
    expect(pendingConfirm.value?.title).toBe('Sign out Safari · iOS?')
    await answer(true)
    expect(apiMock).toHaveBeenCalledWith(`/api/auth/sessions/${phone.id}`, { method: 'DELETE' })
    expect(w.findAll('li[data-session]')).toHaveLength(1)
    expect(useToastStore().toasts[0]?.text).toBe('Safari · iOS signed out.')
  })

  it('does nothing when the confirmation is declined', async () => {
    const w = await open()
    await w.findAll('[data-testid="session-sign-out"]')[1]!.trigger('click')
    await answer(false)
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringContaining(phone.id), expect.anything())
    expect(w.findAll('li[data-session]')).toHaveLength(2)
  })

  it('signs out all other sessions at once', async () => {
    const w = await open()
    sessions = [laptop]
    await w.get('[data-testid="sessions-sign-out-others"]').trigger('click')
    await answer(true)
    expect(apiMock).toHaveBeenCalledWith('/api/auth/sessions/revoke-others', expect.objectContaining({ method: 'POST' }))
    expect(w.find('[data-testid="sessions-sign-out-others"]').exists()).toBe(false)
    expect(w.find('[data-testid="sessions-only-this"]').exists()).toBe(true)
  })

  it('leaves the page when the current session is signed out', async () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { ...window.location, assign })
    const account = useAccountStore()
    account.dialogOpen = true
    const w = await open()
    await w.findAll('[data-testid="session-sign-out"]')[0]!.trigger('click')
    await answer(true)
    expect(apiMock).toHaveBeenCalledWith(`/api/auth/sessions/${laptop.id}`, { method: 'DELETE' })
    expect(assign).toHaveBeenCalledWith('/')
    expect(account.dialogOpen).toBe(false)
    vi.unstubAllGlobals()
  })

  it('shows a load failure instead of an empty list', async () => {
    apiMock.mockRejectedValue(new Error('down'))
    const w = await open()
    expect(w.find('[data-testid="sessions-error"]').exists()).toBe(true)
    expect(w.find('li[data-session]').exists()).toBe(false)
  })

  it('keeps the list when a later refresh fails and retries from the error', async () => {
    const w = await open()
    apiMock.mockImplementation(async path => {
      if (path === '/api/auth/sessions') throw new Error('down')
      return { revoked: 1 }
    })
    await w.get('[data-testid="sessions-sign-out-others"]').trigger('click')
    await answer(true)
    expect(w.find('[data-testid="sessions-error"]').exists()).toBe(true)
    expect(w.findAll('li[data-session]').length).toBeGreaterThan(0)
    apiMock.mockImplementation(async () => ({ sessions }))
    await w.get('[data-testid="sessions-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="sessions-error"]').exists()).toBe(false)
    expect(w.findAll('li[data-session]')).toHaveLength(2)
  })
})
