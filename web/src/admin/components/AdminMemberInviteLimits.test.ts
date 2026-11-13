import type { ApiOptions } from '../../shared/lib/api'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminMemberInviteLimits from './AdminMemberInviteLimits.vue'
import { useToastStore } from '../../stores/toast'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const limits = { max_expires_in_hours: 168, max_uses: 25, max_active: 10, max_per_hour: 10 }

async function mountLimits() {
  apiMock.mockResolvedValueOnce(limits)
  const w = mount(AdminMemberInviteLimits)
  await flushPromises()
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminMemberInviteLimits', () => {
  it('shows the limits the server has and nothing to save yet', async () => {
    const w = await mountLimits()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/invites/limits', { decode: expect.any(Function) })
    expect(w.get<HTMLInputElement>('[data-testid="member-invite-hours"]').element.value).toBe('168')
    expect(w.get<HTMLInputElement>('[data-testid="member-invite-perHour"]').element.value).toBe('10')
    expect(w.get('[data-testid="member-invite-limits-save"]').attributes('disabled')).toBeDefined()
  })

  it('saves all four limits together', async () => {
    const w = await mountLimits()
    await w.get('[data-testid="member-invite-hours"]').setValue('48')
    await w.get('[data-testid="member-invite-uses"]').setValue('5')
    await w.get('[data-testid="member-invite-active"]').setValue('3')
    await w.get('[data-testid="member-invite-perHour"]').setValue('4')
    apiMock.mockResolvedValueOnce({ max_expires_in_hours: 48, max_uses: 5, max_active: 3, max_per_hour: 4 })
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/invites/limits', {
      method: 'PUT', json: { max_expires_in_hours: 48, max_uses: 5, max_active: 3, max_per_hour: 4 }, decode: expect.any(Function)
    })
    expect(useToastStore().toasts[0]?.text).toBe('Limits saved')
  })

  it('refuses numbers out of range before asking the server', async () => {
    const w = await mountLimits()
    await w.get('[data-testid="member-invite-uses"]').setValue('5000')
    expect(w.get('[data-testid="member-invite-limits-save"]').attributes('disabled')).toBeDefined()
    expect(w.get('[role="alert"]').text()).toContain('whole numbers')
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('says when the limits could not be loaded and tries again', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mount(AdminMemberInviteLimits)
    await flushPromises()
    expect(w.find('[data-testid="member-invite-limits-error"]').exists()).toBe(true)
    apiMock.mockResolvedValueOnce(limits)
    await w.get('[data-testid="member-invite-limits-error"] button').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="member-invite-hours"]').exists()).toBe(true)
  })
})
