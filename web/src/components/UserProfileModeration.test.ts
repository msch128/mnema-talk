import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UserProfileModal from './UserProfileModal.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { pendingModeration } from '../lib/moderation'
import { setLocale } from '../i18n'
import { fixtureId, userFixture } from '../test-fixtures.fixture'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const other = userFixture({ id: fixtureId(9), username: 'max', display_name: 'Max', role: 'user' })
let wrapper: VueWrapper
let chat: ReturnType<typeof useChatStore>
let permissions: ReturnType<typeof usePermissionsStore>

function allow(bits: number) {
  permissions.info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
}

beforeEach(() => {
  setLocale('en')
  mockApi.mockReset()
  pendingModeration.value = null
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: fixtureId(1), username: 'mod', role: 'user' })
  chat = useChatStore()
  permissions = usePermissionsStore()
  chat.selectedUserProfile = { ...other }
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('UserProfileModal moderation', () => {
  it('offers nothing without the permissions', () => {
    allow(0)
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-moderation"]').exists()).toBe(false)
  })

  it('opens the timeout and ban dialogs', async () => {
    allow(PERMISSION_BITS.timeout_members | PERMISSION_BITS.ban_members)
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-untimeout"]').exists()).toBe(false)
    await wrapper.get('[data-testid="profile-timeout"]').trigger('click')
    expect(pendingModeration.value).toMatchObject({ kind: 'timeout', target: { id: other.id, name: 'Max' } })
    await wrapper.get('[data-testid="profile-ban"]').trigger('click')
    expect(pendingModeration.value).toMatchObject({ kind: 'ban', target: { id: other.id } })
  })

  it('lifts a running timeout', async () => {
    allow(PERMISSION_BITS.timeout_members)
    chat.members = [{ ...other, timeout_until: new Date(Date.now() + 60_000).toISOString() } as never]
    mockApi.mockResolvedValue(undefined)
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-ban"]').exists()).toBe(false)
    await wrapper.get('[data-testid="profile-untimeout"]').trigger('click')
    await flushPromises()
    expect(mockApi).toHaveBeenCalledWith(`/api/admin/users/${other.id}/timeout`, { method: 'DELETE' })
  })
})
