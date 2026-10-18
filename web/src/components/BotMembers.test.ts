import type { ApiOptions } from '../lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import BotBadge from './BotBadge.vue'
import MemberList from './MemberList.vue'
import MemberRolesDialog from './MemberRolesDialog.vue'
import MentionSuggestions from './MentionSuggestions.vue'
import ReplyPreview from './ReplyPreview.vue'
import UserProfileModal from './UserProfileModal.vue'
import AdminRolesTab from './AdminRolesTab.vue'
import AdminAuditTab from './AdminAuditTab.vue'
import BotRolesPanel from './BotRolesPanel.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Member, Role } from '../types/domain'
import type { BotBot } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))

const role = (n: number, overrides: Partial<Role> = {}): Role => ({
  id: fixtureId(40 + n), name: `Role ${n}`, color: 0, position: n, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null, ...overrides
})
const botRole = role(1, { name: 'Bot', hoist: true, managed: true })
const everyone = role(0, { name: '@everyone', is_everyone: true })
const owner = userFixture({ id: fixtureId(1), username: 'herzog', display_name: 'Herzog', role: 'admin' })
const helper: Member = { ...userFixture({ id: fixtureId(2), username: 'bot-a1b2c3', display_name: 'Helper' }), is_bot: true, added_by: owner.id }
const sleeper: Member = { ...userFixture({ id: fixtureId(3), username: 'bot-d4e5f6', display_name: 'Sleeper' }), is_bot: true }

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})
afterEach(() => { document.body.innerHTML = '' })

describe('BotBadge', () => {
  it('labels a bot "Bot" and a webhook "Webhook", styled alike', () => {
    const bot = mount(BotBadge)
    expect(bot.get('[data-testid="bot-badge"]').text()).toBe('Bot')
    const hook = mount(BotBadge, { props: { webhook: true } })
    expect(hook.get('[data-testid="webhook-badge"]').text()).toBe('Webhook')
    expect(hook.classes()).toEqual(bot.classes())
  })
})

describe('bots in the member list', () => {
  function setup() {
    const chat = useChatStore()
    chat.members = [owner, helper, sleeper]
    chat.presenceById = { [owner.id]: 'online', [helper.id]: 'online' }
    const permissions = usePermissionsStore()
    permissions.roles = [botRole, everyone]
    permissions.setMemberRoles([{ id: helper.id, role_ids: [botRole.id] }, { id: sleeper.id, role_ids: [botRole.id] }])
    return mount(MemberList)
  }

  it('groups an online bot under the hoisted Bot role and lists an offline one with the BOT tag', () => {
    const w = setup()
    const group = w.get('[data-role-group="Bot"]')
    expect(group.text()).toContain('Helper')
    expect(group.find('[data-testid="bot-badge"]').exists()).toBe(true)
    const offline = w.get('[data-member="bot-d4e5f6"]')
    expect(offline.find('[data-testid="bot-badge"]').exists()).toBe(true)
    expect(w.get('[data-member="herzog"]').find('[data-testid="bot-badge"]').exists()).toBe(false)
  })

  it('lists bots among the others when the Bot role is not hoisted', async () => {
    const w = setup()
    usePermissionsStore().roles = [{ ...botRole, hoist: false }, everyone]
    await w.vm.$nextTick()
    expect(w.find('[data-role-group="Bot"]').exists()).toBe(false)
    expect(w.get('[data-member="bot-a1b2c3"]').text()).toContain('Helper')
  })
})

describe('the BOT tag elsewhere', () => {
  it('shows in mention suggestions, reply previews and the audit log', async () => {
    const mention = mount(MentionSuggestions, { props: { items: [{ username: helper.username, display_name: 'Helper', user: helper }, { username: 'herzog', display_name: 'Herzog', user: owner }] } })
    expect(mention.findAll('[data-testid="bot-badge"]')).toHaveLength(1)

    const reply = (extra: Record<string, unknown>) => mount(ReplyPreview, { props: { reply: { id: fixtureId(9), deleted: false, user_id: helper.id, username: helper.username, display_name: 'Helper', content: 'hi', ...extra } } })
    expect(reply({ is_bot: true }).find('[data-testid="bot-badge"]').exists()).toBe(true)
    expect(reply({ is_bot: true, is_webhook: true }).find('[data-testid="webhook-badge"]').exists()).toBe(true)
    expect(reply({}).find('[data-testid="bot-badge"]').exists()).toBe(false)

    useChatStore().members = [owner, helper]
    apiMock.mockResolvedValue({
      entries: [
        { id: 2, created_at: '2026-01-01T10:00:00Z', actor_id: helper.id, actor_name: 'Helper', action: 'message.delete', target_type: 'user', target_id: owner.id, target_name: 'Herzog', details: {}, request_id: 'r' },
        { id: 1, created_at: '2026-01-01T09:00:00Z', actor_id: owner.id, actor_name: 'Herzog', action: 'role.update', target_type: 'role', target_id: 'x', target_name: 'Mods', details: {}, request_id: 'r' }
      ],
      next_before: null
    })
    const audit = mount(AdminAuditTab)
    await flushPromises()
    expect(audit.findAll('[data-testid="bot-badge"]')).toHaveLength(1)
  })
})

describe('the profile of a bot', () => {
  function open(user: Member) {
    useAuthStore().user = userFixture({ id: fixtureId(8), username: 'mod' })
    const chat = useChatStore()
    chat.members = [owner, helper, user]
    chat.selectedUserProfile = { ...user }
    const permissions = usePermissionsStore()
    permissions.info = { owner: false, permissions: PERMISSION_BITS.manage_roles | PERMISSION_BITS.manage_members, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
    permissions.roles = [botRole, everyone]
    permissions.setMemberRoles([{ id: helper.id, role_ids: [botRole.id] }])
    return mount(UserProfileModal, { attachTo: document.body })
  }

  it('shows the BOT tag, the locked Bot role and who added it, and offers neither role nor moderation edits', () => {
    const w = open(helper)
    expect(w.find('[data-testid="bot-badge"]').exists()).toBe(true)
    expect(w.get('[data-testid="profile-roles"]').text()).toContain('Bot')
    expect(w.get('[data-testid="profile-roles"]').find('[role="img"]').exists()).toBe(true)
    expect(w.get('[data-testid="profile-added-by"]').text()).toBe('Added by Herzog')
    expect(w.find('[data-testid="profile-edit-roles"]').exists()).toBe(false)
    expect(w.find('[data-testid="profile-moderation"]').exists()).toBe(false)
    w.unmount()
  })

  it('does not say who added a bot whose creator is unknown, and a person has no tag', () => {
    const w = open(sleeper)
    expect(w.find('[data-testid="profile-added-by"]').exists()).toBe(false)
    w.unmount()
    const person = { ...userFixture({ id: fixtureId(4), username: 'ann', display_name: 'Ann' }), is_bot: false } as Member
    const p = open(person)
    expect(p.find('[data-testid="bot-badge"]').exists()).toBe(false)
    p.unmount()
  })
})

describe('the managed Bot role in role management', () => {
  const custom = role(2, { name: 'Mods' })

  async function editor() {
    const auth = useAuthStore()
    auth.user = userFixture({ role: 'admin' })
    const permissions = usePermissionsStore()
    permissions.info = { owner: true, permissions: 65535, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [], channels: [] }
    apiMock.mockImplementation(async (path) => (path === '/api/roles' ? [custom, botRole, everyone] : undefined))
    const w = mount(AdminRolesTab)
    await flushPromises()
    return w
  }

  it('marks the role as managed: locked name, no delete, no upload limit, an explanation', async () => {
    const w = await editor()
    expect(w.get('[data-role="Bot"]').find('[data-testid="role-managed-icon"]').exists()).toBe(true)
    expect(w.get('[data-role="Mods"]').find('[data-testid="role-managed-icon"]').exists()).toBe(false)
    await w.get('[data-role="Bot"] button[aria-pressed]').trigger('click')
    expect(w.get('[data-testid="role-managed-note"]').text()).toContain('managed')
    expect((w.get('input[maxlength="32"]').element as HTMLInputElement).disabled).toBe(true)
    expect(w.find('input[type="number"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Delete role')
    await w.get('[data-role="Mods"] button[aria-pressed]').trigger('click')
    expect(w.find('[data-testid="role-managed-note"]').exists()).toBe(false)
    expect((w.get('input[maxlength="32"]').element as HTMLInputElement).disabled).toBe(false)
  })

  it('lists the Bot role in a bot\'s roles as held and fixed, and never offers it for people', async () => {
    usePermissionsStore().roles = [role(2, { name: 'Mods' }), botRole, everyone]
    const bot: BotBot = { id: fixtureId(5), user_id: helper.id, name: 'Helper', allowed_intents: 0, disabled: false, role_ids: [], created_at: '2026-01-02T03:04:05Z' }
    const panel = mount(BotRolesPanel, { props: { bot } })
    const box = panel.get('[data-role="Bot"] input').element as HTMLInputElement
    expect(box.checked).toBe(true)
    expect(box.disabled).toBe(true)
    expect(panel.find('[data-testid="bot-role-managed"]').exists()).toBe(true)
    expect(panel.get('[data-role="Mods"] input').element).toHaveProperty('checked', false)

    const auth = useAuthStore()
    auth.user = userFixture({ id: fixtureId(8), role: 'admin' })
    usePermissionsStore().info = { owner: true, permissions: 65535, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [], channels: [] }
    const dialog = mount(MemberRolesDialog, { props: { user: { id: fixtureId(7), username: 'max', display_name: 'Max', role: 'user' } }, attachTo: document.body })
    expect(document.body.textContent).toContain('Mods')
    expect(document.body.textContent).not.toContain('Bot')
    dialog.unmount()
  })
})
