import type { ApiOptions } from '../lib/api'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AccountEmail from './AccountEmail.vue'
import { useAccountStore } from '../stores/account'
import { useToastStore } from '../stores/toast'
import { t, setLocale } from '../i18n'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

let wrapper: VueWrapper

function mountWith(twoFactor: boolean) {
  setActivePinia(createPinia())
  useAccountStore().info = { email_verified: false, mail_available: true, recovery_codes_remaining: 0, two_factor_enabled: twoFactor }
  wrapper = mount(AccountEmail, { attachTo: document.body })
}

beforeEach(() => {
  setLocale('en')
  mockApi.mockReset()
})

afterEach(() => {
  wrapper.unmount()
  document.body.innerHTML = ''
})

async function startEdit() {
  await wrapper.get('[data-testid="account-email-edit"]').trigger('click')
  await wrapper.get('[data-testid="account-email-input"]').setValue('ada@example.org')
  await wrapper.get('[data-testid="account-email-password"]').setValue('previous-password')
}

describe('the e-mail address', () => {
  it('sends the second factor code with a change while two-factor sign-in is on', async () => {
    mountWith(true)
    await startEdit()
    await wrapper.get('[data-testid="account-email-code"]').setValue(' 123456 ')
    mockApi.mockResolvedValue({ email: 'ada@example.org', email_verified: false, mail_available: true, recovery_codes_remaining: 0, two_factor_enabled: true })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith('/api/users/me/email', expect.objectContaining({
      json: { email: 'ada@example.org', current_password: 'previous-password', code: '123456' }
    }))
  })

  it('does not submit without a code while two-factor sign-in is on', async () => {
    mountWith(true)
    await startEdit()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toBe(t('security.email.needCode'))
    expect(mockApi).not.toHaveBeenCalled()
  })

  it('shows no code field otherwise', async () => {
    mountWith(false)
    await startEdit()
    expect(wrapper.find('[data-testid="account-email-code"]').exists()).toBe(false)
  })

  it('says so when the address is saved but the confirmation mail was refused', async () => {
    mountWith(false)
    await startEdit()
    mockApi.mockResolvedValue({ email: 'ada@example.org', email_verified: false, mail_available: true, recovery_codes_remaining: 0, two_factor_enabled: false, confirmation_sent: false })
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.map(x => x.text)).toContain(t('security.email.savedNoMail'))
    expect(wrapper.get('[data-testid="account-email-address"]').text()).toBe('ada@example.org')
  })
})
