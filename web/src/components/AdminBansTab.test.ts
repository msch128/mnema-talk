import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminBansTab from './AdminBansTab.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args: unknown[]) => confirmMock(...args) }))

const bans = () => [
  { user_id: '00000000-0000-4000-8000-000000000001', username: 'troll', display_name: 'The Troll', reason: 'spam', banned_at: '2026-01-01T00:00:00Z', banned_until: null, banned_by: 'moddy' },
  { user_id: '00000000-0000-4000-8000-000000000002', username: 'quiet', display_name: '', reason: '', banned_at: '2026-01-01T00:00:00Z', banned_until: '2030-01-01T00:00:00Z', banned_by: '' }
]
const toastTexts = () => useToastStore().toasts.map(t => t.text)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
})

describe('AdminBansTab', () => {
  it('lists bans with reason, moderator and end', async () => {
    apiMock.mockResolvedValue(bans())
    const w = mount(AdminBansTab)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/bans', { decode: expect.any(Function) })
    const first = w.get('[data-testid="ban-00000000-0000-4000-8000-000000000001"]').text()
    expect(first).toContain('The Troll')
    expect(first).toContain('Banned for good')
    expect(first).toContain('by moddy')
    expect(first).toContain('spam')
    const second = w.get('[data-testid="ban-00000000-0000-4000-8000-000000000002"]').text()
    expect(second).toContain('quiet')
    expect(second).toContain('Banned until')
    expect(second).not.toContain('by ')
  })

  it('says when nobody is banned and reports a failed load', async () => {
    apiMock.mockResolvedValueOnce([])
    const w = mount(AdminBansTab)
    await flushPromises()
    expect(w.text()).toContain('Nobody is banned.')
    apiMock.mockRejectedValueOnce(new Error('boom'))
    const failed = mount(AdminBansTab)
    await flushPromises()
    expect(toastTexts()).toContain('boom')
    expect(failed.text()).not.toContain('Nobody is banned.')
    expect(failed.get('[data-testid="bans-error"]').text()).toContain('Couldn’t load the bans.')
    apiMock.mockResolvedValueOnce(bans())
    await failed.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(failed.find('[data-testid="bans-error"]').exists()).toBe(false)
    expect(failed.text()).toContain('The Troll')
  })

  it('unbans after confirmation and drops the row', async () => {
    apiMock.mockResolvedValueOnce(bans())
    const w = mount(AdminBansTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="unban"]').trigger('click')
    expect(apiMock).toHaveBeenCalledTimes(1)

    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await w.get('[data-testid="unban"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/users/00000000-0000-4000-8000-000000000001/ban', { method: 'DELETE' })
    expect(w.find('[data-testid="ban-00000000-0000-4000-8000-000000000001"]').exists()).toBe(false)
    expect(toastTexts()).toContain('The Troll is unbanned.')
  })

  it('names a member without a display name by their user name', async () => {
    apiMock.mockResolvedValueOnce(bans())
    const w = mount(AdminBansTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await w.findAll('[data-testid="unban"]')[1]!.trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledWith(expect.objectContaining({ title: 'Unban quiet?' }))
  })

  it('keeps the row when unbanning fails', async () => {
    apiMock.mockResolvedValueOnce(bans())
    const w = mount(AdminBansTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('denied'))
    await w.get('[data-testid="unban"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('denied')
    expect(w.findAll('[data-testid^="ban-"]')).toHaveLength(2)
  })
})
