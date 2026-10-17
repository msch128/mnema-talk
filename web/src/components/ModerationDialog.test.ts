import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import ModerationDialog from './ModerationDialog.vue'
import { setLocale } from '../i18n'
import { moderate, pendingModeration } from '../lib/moderation'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

const target = { id: 'u7', name: 'Troll' }
const submit = (w: ReturnType<typeof mount>) => w.get('[data-testid="moderation-submit"]')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  pendingModeration.value = null
})

async function open(kind: 'timeout' | 'ban') {
  const w = mount(ModerationDialog)
  const done = moderate(kind, target)
  await nextTick()
  return { w, done }
}

describe('ModerationDialog', () => {
  it('renders nothing until asked', () => {
    expect(mount(ModerationDialog).find('form').exists()).toBe(false)
  })

  it('times a member out for the chosen time with a reason', async () => {
    apiMock.mockResolvedValue({})
    const { w, done } = await open('timeout')
    expect(w.text()).toContain('Time out Troll')
    expect(w.find('[data-testid="duration-forever"]').exists()).toBe(false)
    expect(w.find('[data-testid="revoke-invite"]').exists()).toBe(false)
    await w.get('[data-testid="duration-300"]').trigger('click')
    await w.get('[data-testid="reason"]').setValue('  spam  ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/u7/timeout', { method: 'POST', json: { duration_seconds: 300, reason: 'spam' } })
    expect(await done).toBe(true)
    expect(useToastStore().toasts.map(t => t.text)).toContain('Troll is timed out.')
    expect(w.find('form').exists()).toBe(false)
  })

  it('takes a custom duration in the chosen unit and validates its range', async () => {
    apiMock.mockResolvedValue({})
    const { w } = await open('timeout')
    await w.get('[data-testid="duration-custom"]').trigger('click')
    expect(submit(w).attributes('disabled')).toBeDefined()
    await w.get('[data-testid="custom-amount"]').setValue('2')
    await w.get('[data-testid="custom-unit"]').setValue('hours')
    expect(submit(w).attributes('disabled')).toBeUndefined()
    await w.get('[data-testid="custom-amount"]').setValue('1')
    await w.get('[data-testid="custom-unit"]').setValue('seconds')
    expect(submit(w).attributes('disabled')).toBeDefined()
    expect(w.text()).toContain('Between 5 s and 4 w')
    await w.get('[data-testid="custom-amount"]').setValue('9999')
    await w.get('[data-testid="custom-unit"]').setValue('days')
    expect(submit(w).attributes('disabled')).toBeDefined()
    await w.get('[data-testid="custom-amount"]').setValue('10')
    await w.get('[data-testid="custom-unit"]').setValue('seconds')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users/u7/timeout', { method: 'POST', json: { duration_seconds: 10, reason: '' } })
  })

  it('bans for good by default, and can ban for a time without revoking the invite', async () => {
    apiMock.mockResolvedValue({})
    const { w, done } = await open('ban')
    expect(w.text()).toContain('Ban Troll')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/users/u7/ban', { method: 'POST', json: { duration_seconds: null, reason: '', revoke_invite: true } })
    expect(await done).toBe(true)

    const second = await open('ban')
    await second.w.get('[data-testid="duration-86400"]').trigger('click')
    await second.w.get('[data-testid="duration-forever"]').trigger('click')
    expect(second.w.get('[data-testid="duration-forever"]').attributes('aria-pressed')).toBe('true')
    await second.w.get('[data-testid="duration-86400"]').trigger('click')
    await second.w.get('[data-testid="revoke-invite"]').setValue(false)
    await second.w.get('[data-testid="reason"]').setValue('cooling')
    await second.w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/users/u7/ban', { method: 'POST', json: { duration_seconds: 86400, reason: 'cooling', revoke_invite: false } })
    expect(useToastStore().toasts.map(t => t.text)).toContain('Troll is banned.')
  })

  it('shows a failure and stays open; cancel and re-opening start fresh', async () => {
    apiMock.mockRejectedValue(new Error('nope'))
    const { w, done } = await open('timeout')
    await w.get('[data-testid="reason"]').setValue('x')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('nope')
    expect(pendingModeration.value).not.toBeNull()
    await w.get('form').trigger('submit') // a second try while idle is fine
    await flushPromises()
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(await done).toBe(false)

    const again = moderate('ban', target)
    await nextTick()
    expect((w.get('[data-testid="reason"]').element as HTMLInputElement).value).toBe('')
    expect(w.find('[role="alert"]').exists()).toBe(false)
    pendingModeration.value?.resolve(false)
    await again
  })

  it('does nothing when submitted while invalid or without a request', async () => {
    const { w } = await open('timeout')
    await w.get('[data-testid="duration-custom"]').trigger('click')
    await w.get('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalled()
    pendingModeration.value?.resolve(false)
    await nextTick()
    w.unmount()
  })

  it('cancels a pending request when it is unmounted', async () => {
    const { w, done } = await open('timeout')
    w.unmount()
    expect(await done).toBe(false)
  })
})
