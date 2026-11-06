import type { ApiOptions } from '../lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminUserRow from './AdminUserRow.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'
import { member } from './adminTestkit.fixture'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const until = () => new Date(Date.now() + 3_600_000).toISOString()

function allow(bits: number) {
  usePermissionsStore().info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
}

function mountRow(timeout?: string) {
  const user = { ...member(), ...(timeout ? { timeout_until: timeout } : {}) }
  return { user, w: mount(AdminUserRow, { props: { user }, global: { stubs: { UserAvatar: true } } }) }
}

enableAutoUnmount(afterEach)
afterEach(() => { vi.useRealTimers() })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  mockApi.mockReset().mockResolvedValue(undefined)
})

describe('admin user row remove timeout', () => {
  it('lifts a running timeout through the API', async () => {
    allow(PERMISSION_BITS.timeout_members)
    const { user, w } = mountRow(until())
    await w.get('[data-testid="action-remove-timeout"]').trigger('click')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith(`/api/admin/users/${user.id}/timeout`, { method: 'DELETE' })
  })

  it('is not offered without a timeout or without the permission', () => {
    allow(PERMISSION_BITS.timeout_members)
    expect(mountRow().w.find('[data-testid="action-remove-timeout"]').exists()).toBe(false)
    allow(0)
    expect(mountRow(until()).w.find('[data-testid="action-remove-timeout"]').exists()).toBe(false)
  })

  it('shows the member as active again when the live timeout is lifted', async () => {
    allow(PERMISSION_BITS.timeout_members)
    const { user, w } = mountRow(until())
    const chat = useChatStore()
    chat.members = [{ ...user, timeout_until: until() } as never]
    await w.vm.$nextTick()
    chat.handleWSEvent({ type: 'member_timeout', payload: { id: user.id, timeout_until: null } })
    await w.vm.$nextTick()
    expect(w.get('[data-testid="user-status"]').text()).toBe('Active')
    expect(w.find('[data-testid="action-remove-timeout"]').exists()).toBe(false)
  })

  it('takes the End timeout button away when the timeout runs out', async () => {
    vi.useFakeTimers()
    allow(PERMISSION_BITS.timeout_members)
    const { w } = mountRow(new Date(Date.now() + 5000).toISOString())
    expect(w.find('[data-testid="action-remove-timeout"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(w.find('[data-testid="action-remove-timeout"]').exists()).toBe(false)
    w.unmount()
  })
})
