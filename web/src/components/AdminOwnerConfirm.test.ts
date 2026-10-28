import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminResetPasswordDialog from './AdminResetPasswordDialog.vue'
import AdminResetTwoFactorDialog from './AdminResetTwoFactorDialog.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'
import type { AdminUser } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const alice = { id: '00000000-0000-4000-8000-0000000003fb', username: 'alice', display_name: 'Alice' } as AdminUser
const account = (twoFactor: boolean) => ({ two_factor_enabled: twoFactor })

function answer(twoFactor: boolean, post: () => Promise<unknown> = () => Promise.resolve(undefined)) {
  apiMock.mockImplementation((path) => (path === '/api/users/me/account' ? Promise.resolve(account(twoFactor)) : post()))
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('two-factor reset by the Owner', () => {
  it('asks for the password and the code and sends both', async () => {
    answer(true)
    const w = mount(AdminResetTwoFactorDialog, { props: { user: alice } })
    await flushPromises()
    await w.get('[data-testid="privacy-password"]').setValue('owner-password-123')
    await w.get('[data-testid="privacy-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/users/${alice.id}/two-factor/reset`, {
      method: 'POST', json: { current_password: 'owner-password-123', code: '123456' }
    })
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('needs only the password when the Owner has no second factor', async () => {
    answer(false)
    const w = mount(AdminResetTwoFactorDialog, { props: { user: alice } })
    await flushPromises()
    expect(w.find('[data-testid="privacy-code"]').exists()).toBe(false)
    await w.get('[data-testid="privacy-password"]').setValue('owner-password-123')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/users/${alice.id}/two-factor/reset`, {
      method: 'POST', json: { current_password: 'owner-password-123' }
    })
  })

  it('shows the refusal in the dialog and stays open', async () => {
    answer(false, () => Promise.reject(new Error('Wrong password.')))
    const w = mount(AdminResetTwoFactorDialog, { props: { user: alice } })
    await flushPromises()
    await w.get('[data-testid="privacy-password"]').setValue('nope')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="owner-confirm-error"]').text()).toContain('Wrong password.')
    expect(w.emitted('close')).toBeUndefined()
    expect(useToastStore().toasts.some(x => x.type === 'success')).toBe(false)
  })
})

describe('password set by the Owner', () => {
  it('sends the new password together with the Owner confirmation', async () => {
    answer(true)
    const w = mount(AdminResetPasswordDialog, { props: { user: alice } })
    await flushPromises()
    await w.get('[data-testid="new-password-input"]').setValue('a-brand-new-password')
    await w.get('[data-testid="privacy-password"]').setValue('owner-password-123')
    await w.get('[data-testid="privacy-code"]').setValue('654321')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/users/${alice.id}/password`, {
      method: 'POST', json: { password: 'a-brand-new-password', current_password: 'owner-password-123', code: '654321' }
    })
  })

  it('keeps the dialog open and says why when the confirmation is refused', async () => {
    answer(false, () => Promise.reject(new Error('Wrong password.')))
    const w = mount(AdminResetPasswordDialog, { props: { user: alice } })
    await flushPromises()
    await w.get('[data-testid="new-password-input"]').setValue('a-brand-new-password')
    await w.get('[data-testid="privacy-password"]').setValue('nope')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="owner-confirm-error"]').text()).toContain('Wrong password.')
    expect(w.emitted('close')).toBeUndefined()
  })
})
