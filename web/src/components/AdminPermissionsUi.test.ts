import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminDashboard from './AdminDashboard.vue'
import AdminUsersTab from './AdminUsersTab.vue'
import MemberRolesDialog from './MemberRolesDialog.vue'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { AdminUser, Role } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(40 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const account = (n: number, overrides: Partial<AdminUser> = {}): AdminUser => ({
  ...userFixture({ id: fixtureId(n), username: `user${n}`, display_name: `User ${n}` }), disabled: false, banned: false, last_seen_at: null, ...overrides
})
const roles = [role(5, { name: 'High', color: 0xff0000 }), role(2, { name: 'Mid' }), role(0, { name: '@everyone', is_everyone: true })]
const accounts = () => [
  account(1, { role: 'admin' }),
  account(2, { role_ids: [fixtureId(42)] }), // Mid: below the moderator
  account(3, { role_ids: [fixtureId(45)] }), // High: above
  account(4),
  account(9) // the moderator
]

function asModerator(bits: number) {
  const auth = useAuthStore()
  auth.user = userFixture({ id: fixtureId(9) })
  const permissions = usePermissionsStore()
  permissions.info = { owner: false, permissions: bits, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [], channels: [] }
  permissions.roles = roles
}

function mountUsers() {
  apiMock.mockImplementation(async (path) => (path === '/api/admin/users' ? accounts() : path === '/api/roles' ? roles : undefined))
  return mount(AdminUsersTab)
}
const rows = (w: ReturnType<typeof mount>) => Object.fromEntries(w.findAll('[data-testid="user-row"]').map(r => [r.find('[data-testid="user-username"]').text().slice(1), r]))
const has = (row: { find: (selector: string) => { exists: () => boolean } }, testid: string) => row.find(`[data-testid="${testid}"]`).exists()

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminUsersTab roles and rank', () => {
  it('offers only what the moderator may do to each account', async () => {
    const voice = useVoiceStore()
    asModerator(PERMISSION_BITS.manage_members | PERMISSION_BITS.manage_roles | PERMISSION_BITS.disconnect_members)
    voice.channelUsers = { [fixtureId(70)]: { [fixtureId(2)]: voiceUserFixture({ id: fixtureId(2) }) } }
    const w = mountUsers()
    await flushPromises()
    const r = rows(w)
    expect(r['user2']!.find('[data-testid="user-role"]').text()).toBe('Mid')
    expect(r['user3']!.find('[data-testid="user-role"]').text()).toBe('High')
    expect(r['user4']!.find('[data-testid="user-role"]').text()).toBe('Member')
    expect(r['user1']!.find('[data-testid="user-role"]').text()).toBe('Owner')
    // below the moderator: roles, disable, sessions, kick (in a Talk); no password reset
    for (const id of ['action-roles', 'action-disable', 'action-revoke-sessions', 'action-kick']) expect(has(r['user2']!, id), id).toBe(true)
    expect(has(r['user2']!, 'action-reset-password')).toBe(false)
    // above, the Owner and themselves: nothing
    for (const name of ['user1', 'user3', 'user9']) {
      for (const id of ['action-roles', 'action-disable', 'action-revoke-sessions']) expect(has(r[name]!, id), `${name} ${id}`).toBe(false)
    }
  })

  it('hides what the permissions do not cover', async () => {
    asModerator(PERMISSION_BITS.manage_roles)
    useVoiceStore().channelUsers = { [fixtureId(70)]: { [fixtureId(2)]: voiceUserFixture({ id: fixtureId(2) }) } }
    const w = mountUsers()
    await flushPromises()
    const mid = rows(w)['user2']!
    expect(has(mid, 'action-roles')).toBe(true)
    expect(has(mid, 'action-disable')).toBe(false)
    expect(has(mid, 'action-kick')).toBe(false)
  })

  it('lets the Owner reset passwords and opens the roles dialog, which refreshes the list when saved', async () => {
    useAuthStore().user = userFixture({ id: fixtureId(1), role: 'admin' })
    const w = mountUsers()
    await flushPromises()
    expect(has(rows(w)['user2']!, 'action-reset-password')).toBe(true)
    await rows(w)['user2']!.find('[data-testid="action-roles"]').trigger('click')
    const dialog = w.findComponent(MemberRolesDialog)
    expect(dialog.exists()).toBe(true)
    apiMock.mockClear()
    dialog.vm.$emit('saved')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/users', expect.anything())
    dialog.vm.$emit('close')
    await flushPromises()
    expect(w.findComponent(MemberRolesDialog).exists()).toBe(false)
  })
})

describe('AdminDashboard tabs', () => {
  const tabs = (w: ReturnType<typeof mount>) => w.findAll('[data-testid^="tab-"]').map(t => t.attributes('data-testid')!.slice(4))
  const stubs = { AdminUsersTab: true, AdminRolesTab: true, AdminLayoutTab: true, AdminInvitesTab: true, AdminBansTab: true, AdminBotsTab: true, AdminMediaTab: true, AdminAuditTab: true, AdminSystemTab: true }

  it('shows a tab only to those it is for, and the storage and system tabs to the Owner alone', () => {
    useAuthStore().user = userFixture({ role: 'admin' })
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toEqual(['users', 'roles', 'channels', 'invites', 'onboarding', 'access', 'emoji', 'raid', 'bans', 'audit', 'bots', 'media', 'system'])
    setActivePinia(createPinia())
    asModerator(PERMISSION_BITS.manage_bots)
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toEqual(['bots'])
    setActivePinia(createPinia())
    asModerator(PERMISSION_BITS.view_audit_log)
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toEqual(['audit'])
    setActivePinia(createPinia())
    asModerator(PERMISSION_BITS.manage_roles | PERMISSION_BITS.create_invites)
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toEqual(['users', 'roles', 'invites'])
    setActivePinia(createPinia())
    asModerator(PERMISSION_BITS.manage_channels)
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toEqual(['channels'])
    // Welcome & rules belong to Administrators (and the Owner), not to invite managers.
    setActivePinia(createPinia())
    asModerator(PERMISSION_BITS.administrator)
    expect(tabs(mount(AdminDashboard, { global: { stubs } }))).toContain('onboarding')
  })

  it('opens the first tab the member may use when the asked one is not theirs', async () => {
    asModerator(PERMISSION_BITS.create_invites)
    const w = mount(AdminDashboard, { props: { initialTab: 'system' }, global: { stubs } })
    expect(w.find('[data-testid="tab-invites"]').attributes('aria-pressed')).toBe('true')
    await w.setProps({ initialTab: 'users' })
    expect(w.find('[data-testid="tab-invites"]').attributes('aria-pressed')).toBe('true')
  })

  it('falls back to users when no tab is allowed at all', () => {
    asModerator(0)
    const w = mount(AdminDashboard, { global: { stubs } })
    expect(tabs(w)).toEqual([])
  })
})
