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
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

let wrapper: VueWrapper | null = null

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset().mockResolvedValue(true)
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
  it('is offered to the Owner and asks before it acts', async () => {
    const w = await openUsers()
    apiMock.mockClear()
    confirmMock.mockResolvedValue(false)
    await w.get('[data-testid="action-reset-two-factor"]').trigger('click')
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()

    confirmMock.mockResolvedValue(true)
    apiMock.mockResolvedValue(undefined)
    await w.get('[data-testid="action-reset-two-factor"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(expect.stringMatching(/^\/api\/admin\/users\/.+\/two-factor\/reset$/), { method: 'POST' })
    expect(useToastStore().toasts.at(-1)?.text).toBe('Two-factor sign-in reset.')
  })

  it('reports a failure', async () => {
    const w = await openUsers()
    apiMock.mockRejectedValue(new Error('This action is not available for the Owner account'))
    await w.get('[data-testid="action-reset-two-factor"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.at(-1)?.type).toBe('error')
  })

  it('is not offered to anyone but the Owner', async () => {
    useAuthStore().user = userFixture({ role: 'user', username: 'moderator' })
    usePermissionsStore().$patch({})
    const w = await openUsers()
    expect(w.find('[data-testid="action-reset-two-factor"]').exists()).toBe(false)
  })
})
