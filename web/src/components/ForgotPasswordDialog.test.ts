import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ForgotPasswordDialog from './ForgotPasswordDialog.vue'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

beforeEach(() => {
  setLocale('en')
  apiMock.mockReset()
  document.body.replaceChildren()
})

describe('ForgotPasswordDialog', () => {
  it('asks for a reset link and answers the same way for any address', async () => {
    apiMock.mockResolvedValue(undefined)
    const w = mount(ForgotPasswordDialog, { attachTo: document.body })
    await w.get('#forgot-email').setValue(' someone@example.org ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/auth/password-reset/request', { method: 'POST', json: { email: 'someone@example.org' } })
    expect(w.get('[data-testid="forgot-sent"]').text()).toContain('If an account has this confirmed address')
    w.unmount()
  })

  it('shows a refusal such as a rate limit without claiming success', async () => {
    apiMock.mockRejectedValue(new Error('Too many attempts – please wait a moment'))
    const w = mount(ForgotPasswordDialog, { attachTo: document.body })
    await w.get('#forgot-email').setValue('someone@example.org')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('Too many attempts')
    expect(w.find('[data-testid="forgot-sent"]').exists()).toBe(false)
    w.unmount()
  })

  it('closes on request', async () => {
    const w = mount(ForgotPasswordDialog, { attachTo: document.body })
    await w.get('button[type="button"]:not([data-dialog-close])').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
})
