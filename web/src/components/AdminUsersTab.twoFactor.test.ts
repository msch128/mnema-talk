import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminUsersTab from './AdminUsersTab.vue'
import { useToastStore } from '../stores/toast'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { userFixture } from '../test-fixtures.fixture'
import { member } from './adminTestkit.fixture'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))

let wrapper: VueWrapper | null = null

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

async function openUsers() {
  apiMock.mockResolvedValue([member()])
  wrapper = mount(AdminUsersTab)
  await flushPromises()
  return wrapper
}

describe('resetting a member\'s two-factor sign-in', () => {
  async function openDialog() {
    const w = await openUsers()
    apiMock.mockReset().mockResolvedValue(undefined)
    await w.get('[data-testid="action-reset-two-factor"]').trigger('click')
    await flushPromises()
    return w
  }

  it('asks the Owner to confirm before it acts, and cancelling sends nothing', async () => {
    const w = await openDialog()
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringMatching(/two-factor\/reset$/), expect.anything())
    await w.get('button.btn-secondary').trigger('click')
    expect(w.find('[data-testid="privacy-password"]').exists()).toBe(false)
    expect(apiMock).not.toHaveBeenCalledWith(expect.stringMatching(/two-factor\/reset$/), expect.anything())
  })

  it('sends the confirmation with the reset', async () => {
    const w = await openDialog()
    await w.get('[data-testid="privacy-password"]').setValue('owner-password-123')
    await w.get('[data-testid="privacy-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(expect.stringMatching(/^\/api\/admin\/users\/.+\/two-factor\/reset$/), {
      method: 'POST', json: { current_password: 'owner-password-123', code: '123456' }
    })
    expect(useToastStore().toasts.at(-1)?.text).toBe('Two-factor sign-in reset.')
  })

  it('reports a failure in the dialog', async () => {
    const w = await openDialog()
    apiMock.mockRejectedValue(new Error('This action is not available for the Owner account'))
    await w.get('[data-testid="privacy-password"]').setValue('owner-password-123')
    await w.get('[data-testid="privacy-code"]').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="owner-confirm-error"]').text()).toContain('not available')
  })

  it('is not offered to anyone but the Owner', async () => {
    useAuthStore().user = userFixture({ role: 'user', username: 'moderator' })
    usePermissionsStore().$patch({})
    const w = await openUsers()
    expect(w.find('[data-testid="action-reset-two-factor"]').exists()).toBe(false)
  })
})
