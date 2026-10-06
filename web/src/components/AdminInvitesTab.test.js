import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminInvitesTab from './AdminInvitesTab.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn()
vi.mock('../lib/api', () => ({
  api: (...args) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))
const confirmMock = vi.fn()
vi.mock('../lib/confirm', () => ({ confirm: (...args) => confirmMock(...args) }))

const future = new Date(Date.now() + 3600e3).toISOString()
const past = new Date(Date.now() - 3600e3).toISOString()
const invites = () => [
  { id: 'i1', code: 'open-code', max_uses: null, uses_count: 4, expires_at: null },
  { id: 'i2', code: 'gone-code', max_uses: 5, uses_count: 0, expires_at: past },
  { id: 'i3', code: 'full-code', max_uses: 2, uses_count: 2, expires_at: future },
  { id: 'i4', code: 'soon-code', max_uses: 3, uses_count: 1, expires_at: future }
]

const rows = w => w.findAll('.divide-y > div').filter(r => r.find('.font-mono').exists())
const row = (w, code) => rows(w).find(r => r.find('.font-mono').text() === code)
const toastTexts = () => useToastStore().toasts.map(t => t.text)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('AdminInvitesTab', () => {
  it('lists invites with their uses and status', async () => {
    apiMock.mockResolvedValue(invites())
    const w = mount(AdminInvitesTab)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/invites')
    expect(rows(w)).toHaveLength(4)
    expect(row(w, 'open-code').text()).toContain('Uses: 4 / ∞')
    expect(row(w, 'open-code').text()).toContain('no expiry')
    expect(row(w, 'gone-code').text()).toContain('expired')
    expect(row(w, 'full-code').text()).toContain('used up')
    expect(row(w, 'soon-code').text()).toContain('valid until')
  })

  it('shows the empty state and keeps going when loading fails', async () => {
    apiMock.mockRejectedValue(new Error('boom'))
    const w = mount(AdminInvitesTab)
    await flushPromises()
    expect(w.text()).toContain('No invites yet.')
    expect(toastTexts()).toContain('boom')
  })

  it('creates an invite with the typed limits and reloads', async () => {
    apiMock.mockResolvedValueOnce([])
    const w = mount(AdminInvitesTab)
    await flushPromises()
    const [uses, hours] = w.findAll('input')
    await uses.setValue('10')
    await hours.setValue('48')
    apiMock.mockResolvedValueOnce({}).mockResolvedValueOnce(invites())
    await w.find('button').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/invites', { method: 'POST', json: { max_uses: 10, expires_in_hours: 48 } })
    expect(uses.element.value).toBe('')
    expect(hours.element.value).toBe('')
    expect(rows(w)).toHaveLength(4)
    expect(toastTexts()).toContain('Invite created')
  })

  it('sends no limits when the fields are empty and keeps them on failure', async () => {
    apiMock.mockResolvedValueOnce([])
    const w = mount(AdminInvitesTab)
    await flushPromises()
    apiMock.mockResolvedValueOnce({}).mockResolvedValueOnce([])
    await w.find('button').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenNthCalledWith(2, '/api/admin/invites', { method: 'POST', json: {} })

    await w.findAll('input')[0].setValue('2000')
    apiMock.mockRejectedValueOnce(new Error('max_uses must be between 1 and 1000'))
    const calls = apiMock.mock.calls.length
    await w.find('button').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.length).toBe(calls + 1) // no reload after a failure
    expect(w.findAll('input')[0].element.value).toBe('2000')
    expect(toastTexts()).toContain('max_uses must be between 1 and 1000')
  })

  it('deletes an invite only after confirmation', async () => {
    apiMock.mockResolvedValueOnce(invites())
    const w = mount(AdminInvitesTab)
    await flushPromises()

    confirmMock.mockResolvedValueOnce(false)
    await row(w, 'gone-code').find('button[aria-label="Delete invite"]').trigger('click')
    await flushPromises()
    expect(confirmMock.mock.calls[0][0]).toMatchObject({ danger: true })
    expect(confirmMock.mock.calls[0][0].title).toContain('gone-code')
    expect(apiMock).toHaveBeenCalledTimes(1)

    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(null)
    await row(w, 'gone-code').find('button[aria-label="Delete invite"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/invites/i2', { method: 'DELETE' })
    expect(row(w, 'gone-code')).toBeUndefined()
    expect(rows(w)).toHaveLength(3)
    expect(toastTexts()).toContain('Invite deleted')
  })

  it('keeps the invite listed when deleting fails', async () => {
    apiMock.mockResolvedValueOnce(invites())
    const w = mount(AdminInvitesTab)
    await flushPromises()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('invite not found'))
    await row(w, 'open-code').find('button[aria-label="Delete invite"]').trigger('click')
    await flushPromises()
    expect(rows(w)).toHaveLength(4)
    expect(toastTexts()).toContain('invite not found')
  })

  it('copies the invite link and resets the copied state', async () => {
    vi.useFakeTimers()
    const writeText = vi.fn().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    apiMock.mockResolvedValueOnce([{ id: 'i1', code: 'a b&c', max_uses: null, uses_count: 0, expires_at: null }])
    const w = mount(AdminInvitesTab)
    await flushPromises()
    const copy = rows(w)[0].findAll('button')[0]
    await copy.trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/?invite=a%20b%26c`)
    expect(copy.text()).toBe('Copied')
    expect(toastTexts()).toContain('Link copied')
    vi.advanceTimersByTime(2000)
    await flushPromises()
    expect(copy.text()).toBe('Copy link')
    vi.unstubAllGlobals()
  })

  it('shows the link when the clipboard is unavailable', async () => {
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    apiMock.mockResolvedValueOnce([{ id: 'i1', code: 'abc123', max_uses: null, uses_count: 0, expires_at: null }])
    const w = mount(AdminInvitesTab)
    await flushPromises()
    await rows(w)[0].findAll('button')[0].trigger('click')
    await flushPromises()
    const toast = useToastStore().toasts.at(-1)
    expect(toast.type).toBe('error')
    expect(toast.detail).toBe(`${window.location.origin}/?invite=abc123`)
    expect(rows(w)[0].text()).toContain('Copy link')
    vi.unstubAllGlobals()
  })
})
