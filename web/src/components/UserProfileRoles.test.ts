import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import UserProfileModal from './UserProfileModal.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import type { Role } from '../types/domain'

const mockApi = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => mockApi(...args)
}))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(40 + n), name: `Role ${n}`, color: 0xff0000, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const other = userFixture({ id: fixtureId(9), username: 'max', display_name: 'Max', role: 'user' })

let wrapper: VueWrapper
let chat: ReturnType<typeof useChatStore>
let permissions: ReturnType<typeof usePermissionsStore>

beforeEach(() => {
  setLocale('en')
  mockApi.mockReset()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: fixtureId(1), username: 'mod', role: 'user' })
  chat = useChatStore()
  permissions = usePermissionsStore()
  permissions.info = { owner: false, permissions: PERMISSION_BITS.manage_roles | PERMISSION_BITS.manage_members, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
  permissions.roles = [role(1), role(2, { color: 0 })]
  permissions.setMemberRoles([{ id: other.id, role_ids: [role(1).id, role(2).id] }])
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('UserProfileModal roles', () => {
  it('shows the roles and lets a higher-ranked member change them', async () => {
    chat.selectedUserProfile = { ...other }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    const box = wrapper.get('[data-testid="profile-roles"]')
    expect(box.text()).toContain('Role 1')
    expect(box.text()).toContain('Role 2')
    await wrapper.get('[data-testid="profile-edit-roles"]').trigger('click')
    expect(wrapper.findComponent({ name: 'MemberRolesDialog' }).exists()).toBe(true)
    wrapper.findComponent({ name: 'MemberRolesDialog' }).vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'MemberRolesDialog' }).exists()).toBe(false)
  })

  it('offers no role editing for the Owner, for oneself or without the permission', async () => {
    chat.selectedUserProfile = { ...other, role: 'admin' }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-edit-roles"]').exists()).toBe(false)
    wrapper.unmount()
    permissions.info = { ...permissions.info!, permissions: 0 }
    chat.selectedUserProfile = { ...other }
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-edit-roles"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="profile-status-edit"]').exists()).toBe(false)
  })

  it('copes with a profile stub that has no identity yet', () => {
    chat.selectedUserProfile = { status_text: '' } as never
    wrapper = mount(UserProfileModal, { attachTo: document.body })
    expect(wrapper.find('[data-testid="profile-edit-roles"]').exists()).toBe(false)
  })
})
