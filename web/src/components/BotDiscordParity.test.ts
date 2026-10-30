import type { ApiOptions } from '../lib/api'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MemberList from './MemberList.vue'
import UserProfileModal from './UserProfileModal.vue'
import AdminRolesTab from './AdminRolesTab.vue'
import BotRolesPanel from './BotRolesPanel.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { buildMemberItems } from '../composables/useNavMenus'
import { PERMISSION_BITS } from '../lib/permissions'
import { activityOf, activityText } from '../lib/presence'
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
const sharedBotRole = role(1, { name: 'Bot', hoist: true, managed: true })
const everyone = role(0, { name: '@everyone', is_everyone: true })
const owner = userFixture({ id: fixtureId(1), username: 'cool_guy', display_name: 'Mnema', role: 'admin' })
const helper: Member = { ...userFixture({ id: fixtureId(2), username: 'bot-a1b2c3', display_name: 'Helper' }), is_bot: true, added_by: owner.id }
const sleeper: Member = { ...userFixture({ id: fixtureId(3), username: 'bot-d4e5f6', display_name: 'Sleeper' }), is_bot: true }
const helperRole = role(2, { name: 'Helper', managed: true, bot_user_id: helper.id })
const sleeperRole = role(3, { name: 'Sleeper', managed: true, bot_user_id: sleeper.id })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})
afterEach(() => { document.body.innerHTML = '' })

describe('bot activities', () => {
  it('reads an activity defensively and words it per type', () => {
    expect(activityOf({ type: 0, name: ' chess ' })).toEqual({ type: 0, name: 'chess' })
    for (const bad of [null, 'x', {}, { type: 0 }, { type: '0', name: 'x' }, { type: 0.5, name: 'x' }, { type: 0, name: '  ' }]) expect(activityOf(bad)).toBeNull()
    expect(activityText({ type: 0, name: 'chess' })).toBe('Playing chess')
    expect(activityText({ type: 2, name: 'jazz' })).toBe('Listening to jazz')
    expect(activityText({ type: 3, name: 'you' })).toBe('Watching you')
    expect(activityText({ type: 5, name: 'a cup' })).toBe('Competing in a cup')
    expect(activityText({ type: 4, name: 'out to lunch' })).toBe('out to lunch')
    setLocale('de')
    expect(activityText({ type: 0, name: 'Schach' })).toBe('Spielt Schach')
  })

  it('follows presence_update events and ends with the bot being offline', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: helper.id, status: 'dnd', activity: { type: 0, name: 'chess' } } })
    expect(chat.presenceOf(helper.id)).toBe('dnd')
    expect(chat.activityOfUser(helper.id)).toEqual({ type: 0, name: 'chess' })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: helper.id, status: 'away' } })
    expect(chat.activityOfUser(helper.id)).toBeNull()
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: helper.id, status: 'online', activity: { type: 3, name: 'logs' } } })
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: helper.id, status: 'offline', activity: { type: 3, name: 'logs' } } })
    expect(chat.presenceOf(helper.id)).toBe('offline')
    expect(chat.activityOfUser(helper.id)).toBeNull()
    // A new connection starts from the snapshot, which only carries statuses.
    chat.handleWSEvent({ type: 'presence_update', payload: { user_id: helper.id, status: 'online', activity: { type: 0, name: 'x' } } })
    chat.handleWSEvent({ type: 'presence_snapshot', payload: { [helper.id]: 'online' } })
    expect(chat.activityOfUser(helper.id)).toBeNull()
  })

  function members() {
    const chat = useChatStore()
    chat.members = [owner, helper, sleeper]
    chat.presenceById = { [owner.id]: 'online', [helper.id]: 'dnd' }
    chat.activityById = { [helper.id]: { type: 2, name: 'jazz' }, [owner.id]: { type: 0, name: 'ignored' } }
    return chat
  }

  it('shows the activity of an online bot in the member list, and only of bots', () => {
    members()
    const w = mount(MemberList)
    expect(w.get('[data-member="bot-a1b2c3"] [data-testid="member-activity"]').text()).toBe('Listening to jazz')
    expect(w.get('[data-member="bot-a1b2c3"]').attributes('data-status')).toBe('dnd')
    expect(w.find('[data-member="cool_guy"] [data-testid="member-activity"]').exists()).toBe(false)
    expect(w.find('[data-member="bot-d4e5f6"] [data-testid="member-activity"]').exists()).toBe(false)
  })

  it('shows it in the profile popover of the bot', () => {
    const chat = members()
    chat.selectedUserProfile = { ...helper }
    useAuthStore().user = userFixture({ id: fixtureId(8), username: 'mod' })
    const w = mount(UserProfileModal, { attachTo: document.body })
    expect(w.get('[data-testid="profile-activity"]').text()).toBe('Listening to jazz')
    expect(w.get('[data-testid="profile-presence"]').text()).toBe('Do not disturb')
    w.unmount()
    chat.selectedUserProfile = { ...sleeper }
    const off = mount(UserProfileModal, { attachTo: document.body })
    expect(off.find('[data-testid="profile-activity"]').exists()).toBe(false)
    off.unmount()
  })
})

describe('the own role of a bot', () => {
  async function editor() {
    useAuthStore().user = userFixture({ role: 'admin' })
    usePermissionsStore().info = { owner: true, permissions: 65535, max_upload_mb: 50, role_ids: [], top_position: 3, catalog: [], channels: [] }
    apiMock.mockImplementation(async (path) => (path === '/api/roles' ? [role(5, { name: 'Mods' }), sharedBotRole, helperRole, sleeperRole, everyone] : undefined))
    const w = mount(AdminRolesTab)
    await flushPromises()
    return w
  }

  it('lists bot roles apart from the hierarchy, locked, with an explanation of what they are', async () => {
    const w = await editor()
    expect(w.get('[data-testid="bot-roles-heading"]').text()).toBe('Bot roles')
    const row = w.get('[data-bot-role][data-role="Helper"]')
    expect(row.find('[data-testid="role-managed-icon"]').exists()).toBe(true)
    // Nothing to drag or move: they are not part of the order people arrange.
    expect(row.find('svg[aria-label^="Drag"]').exists()).toBe(false)
    expect(row.findAll('button')).toHaveLength(1)
    expect(w.findAll('li[draggable]').map(li => li.attributes('data-role'))).toEqual(['Mods', 'Bot'])

    await row.get('button[aria-pressed]').trigger('click')
    expect(w.get('[data-testid="role-managed-note"]').text()).toContain('“Helper” is a bot’s own role')
    expect((w.get('input[maxlength="32"]').element as HTMLInputElement).disabled).toBe(true)
    expect(w.find('input[type="number"]').exists()).toBe(false)
    expect(w.text()).not.toContain('Delete role')
    // Colour and permissions stay editable.
    expect(w.find('[data-testid="unsaved-save"]').exists()).toBe(true)
  })

  it('sends only the roles of the hierarchy when they are reordered', async () => {
    const w = await editor()
    await w.get('button[aria-label="Move Bot up"]').trigger('click')
    await flushPromises()
    const call = apiMock.mock.calls.find(c => c[0] === '/api/admin/roles/order')
    expect(call?.[1]?.json).toEqual({ role_ids: [sharedBotRole.id, role(5).id] })
  })

  it('is shown ticked for its own bot and not offered for another one', () => {
    usePermissionsStore().roles = [role(5, { name: 'Mods' }), sharedBotRole, helperRole, sleeperRole, everyone]
    const bot: BotBot = { id: fixtureId(5), user_id: helper.id, name: 'Helper', allowed_intents: 0, disabled: false, role_ids: [helperRole.id], created_at: '2026-01-02T03:04:05Z' }
    const panel = mount(BotRolesPanel, { props: { bot, roles: [...bot.role_ids] } })
    const own = panel.get('[data-role="Helper"] input').element as HTMLInputElement
    expect(own.checked).toBe(true)
    expect(own.disabled).toBe(true)
    expect(panel.get('[data-role="Helper"]').text()).toContain('own role')
    expect(panel.find('[data-role="Sleeper"]').exists()).toBe(false)
    expect(panel.find('[data-role="Bot"]').exists()).toBe(true)
  })

  it('appears with a lock in the profile of the bot', () => {
    const chat = useChatStore()
    chat.members = [owner, helper]
    chat.selectedUserProfile = { ...helper }
    useAuthStore().user = userFixture({ id: fixtureId(8), username: 'mod' })
    const permissions = usePermissionsStore()
    permissions.roles = [sharedBotRole, helperRole, everyone]
    permissions.setMemberRoles([{ id: helper.id, role_ids: [sharedBotRole.id, helperRole.id] }])
    const w = mount(UserProfileModal, { attachTo: document.body })
    const roles = w.get('[data-testid="profile-roles"]')
    expect(roles.text()).toContain('Helper')
    expect(roles.findAll('[role="img"]')).toHaveLength(2)
    expect(roles.findAll('[role="img"]').map(i => i.attributes('aria-label'))).toContain('Managed role of one bot: it cannot be removed.')
    w.unmount()
  })
})

describe('moderation of bots', () => {
  it('offers timeout and ban in the member context menu, like for any member below your rank', () => {
    useAuthStore().user = userFixture({ id: fixtureId(8), username: 'mod' })
    const permissions = usePermissionsStore()
    permissions.info = { owner: false, permissions: PERMISSION_BITS.timeout_members | PERMISSION_BITS.ban_members, max_upload_mb: 50, role_ids: [], top_position: 5, catalog: [], channels: [] }
    permissions.roles = [sharedBotRole, helperRole, everyone]
    permissions.setMemberRoles([{ id: helper.id, role_ids: [sharedBotRole.id, helperRole.id] }])
    const ids = buildMemberItems(helper).map(i => i.id)
    expect(ids).toContain('timeout')
    expect(ids).toContain('ban')
    // A bot's roles sit at the bottom: rank decides, as for everyone.
    permissions.info = { ...permissions.info, top_position: 1 }
    expect(buildMemberItems(helper).map(i => i.id)).not.toContain('ban')
  })
})
