import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import type { Invite } from '../types/domain'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import EditInviteDialog from './EditInviteDialog.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const ID = '00000000-0000-4000-8000-000000000411'
const NOW = new Date('2026-06-01T12:00:00Z')
const invite = (over: Partial<Invite> = {}): Invite => ({
  id: ID, code: 'edit-me-code', max_uses: 10, uses_count: 3, expires_at: '2026-06-10T12:00:00Z',
  created_at: '2026-05-01T00:00:00Z', weak: false, ...over
})

let wrapper: VueWrapper
const open = (inv: Invite) => {
  wrapper = mount(EditInviteDialog, { props: { invite: inv }, attachTo: document.body })
  return wrapper
}
const chip = (kind: 'expiry' | 'uses', key: string) => document.body.querySelector<HTMLButtonElement>(`[data-${kind}="${key}"]`)!
const save = () => document.body.querySelector<HTMLButtonElement>('[data-testid="unsaved-save"]')!
const submit = async () => {
  document.body.querySelector('form')!.dispatchEvent(new Event('submit', { cancelable: true }))
  await flushPromises()
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
})

afterEach(() => {
  wrapper?.unmount()
  vi.useRealTimers()
})

describe('EditInviteDialog', () => {
  it('sends nothing until something changed', () => {
    open(invite())
    expect(save().disabled).toBe(true)
    expect(chip('expiry', 'keep').getAttribute('aria-pressed')).toBe('true')
    expect(chip('uses', 'keep').getAttribute('aria-pressed')).toBe('true')
  })

  it('sets the expiry from a preset and keeps the limit', async () => {
    open(invite())
    chip('expiry', 'hour6').click()
    await flushPromises()
    expect(save().disabled).toBe(false)
    const updated = invite({ expires_at: '2026-06-01T18:00:00.000Z' })
    apiMock.mockResolvedValue(updated)
    await submit()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/invites/${ID}`, expect.objectContaining({ method: 'PATCH', json: { expires_at: '2026-06-01T18:00:00.000Z' } }))
    expect(wrapper.emitted('saved')![0]).toEqual([updated])
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Invite updated.'])
  })

  it('removes the expiry and the limit with null', async () => {
    open(invite())
    chip('expiry', 'never').click()
    chip('uses', 'unlimited').click()
    await flushPromises()
    apiMock.mockResolvedValue(invite({ expires_at: null, max_uses: null }))
    await submit()
    expect(apiMock.mock.calls[0]![1]).toMatchObject({ json: { expires_at: null, max_uses: null } })
  })

  it('adds a limit and an expiry to an invite that has neither', async () => {
    open(invite({ expires_at: null, max_uses: null, uses_count: 0 }))
    expect(document.body.querySelector('[data-expiry="keep"]')).toBeNull()
    expect(save().disabled).toBe(true)
    chip('uses', '25').click()
    chip('expiry', 'week').click()
    await flushPromises()
    apiMock.mockResolvedValue(invite())
    await submit()
    expect(apiMock.mock.calls[0]![1]).toMatchObject({ json: { expires_at: '2026-06-08T12:00:00.000Z', max_uses: 25 } })
  })

  it('takes a custom limit and refuses one below the uses already consumed', async () => {
    open(invite())
    chip('uses', 'custom').click()
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('[data-testid="edit-invite-uses"]')!
    input.value = '2'
    input.dispatchEvent(new Event('input'))
    await submit()
    expect(apiMock).not.toHaveBeenCalled()
    expect(document.body.querySelector('[role="alert"]')!.textContent).toContain('3 uses so far')
    input.value = '3'
    input.dispatchEvent(new Event('input'))
    apiMock.mockResolvedValue(invite({ max_uses: 3 }))
    await submit()
    expect(apiMock.mock.calls[0]![1]).toMatchObject({ json: { max_uses: 3 } })
  })

  it('refuses a custom date in the past', async () => {
    open(invite())
    chip('expiry', 'custom').click()
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('[data-testid="edit-invite-date"]')!
    input.value = '2026-05-01T10:00'
    input.dispatchEvent(new Event('input'))
    await submit()
    expect(apiMock).not.toHaveBeenCalled()
    expect(document.body.querySelector('[role="alert"]')!.textContent).toContain('future')
  })

  it('sends a valid custom date as a timestamp', async () => {
    open(invite())
    chip('expiry', 'custom').click()
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('[data-testid="edit-invite-date"]')!
    input.value = '2026-07-01T10:00'
    input.dispatchEvent(new Event('input'))
    apiMock.mockResolvedValue(invite())
    await submit()
    expect(apiMock.mock.calls[0]![1]).toMatchObject({ json: { expires_at: new Date('2026-07-01T10:00').toISOString() } })
  })

  it('shows the server error and stays open', async () => {
    open(invite())
    chip('uses', '50').click()
    await flushPromises()
    apiMock.mockRejectedValue(new Error('max_uses must be at least the 3 uses already consumed'))
    await submit()
    expect(document.body.querySelector('[role="alert"]')!.textContent).toContain('already consumed')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not count a choice that keeps things as they are as a change', async () => {
    open(invite({ expires_at: null, max_uses: 10 }))
    chip('uses', '10').click()
    await flushPromises()
    expect(save().disabled).toBe(true)
    chip('uses', '25').click()
    await flushPromises()
    expect(save().disabled).toBe(false)
    chip('uses', 'keep').click()
    await flushPromises()
    expect(save().disabled).toBe(true)
    expect(apiMock).not.toHaveBeenCalled()
  })

  it('keeps Save off for a custom limit below the uses consumed and says why inline', async () => {
    open(invite())
    chip('uses', 'custom').click()
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>('[data-testid="edit-invite-uses"]')!
    input.value = '2'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(save().disabled).toBe(true)
    expect(document.body.querySelector('[data-testid="edit-invite-uses-error"]')!.textContent).toContain('3 uses so far')
  })

  it('asks before closing with unsaved changes, and not without', async () => {
    open(invite())
    document.body.querySelector<HTMLButtonElement>('[data-dialog-close]')!.click()
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    chip('uses', '50').click()
    await flushPromises()
    document.body.querySelector<HTMLButtonElement>('[data-dialog-close]')!.click()
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(pendingConfirm.value?.title).toBe('Discard changes?')
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(2)
    expect(apiMock).not.toHaveBeenCalled()
  })
})
