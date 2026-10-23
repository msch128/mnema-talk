import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarDmSection from './SidebarDmSection.vue'
import DmCreateDialog from './DmCreateDialog.vue'
import DmInfoDialog from './DmInfoDialog.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import type { ChatMember } from '../types/rest'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))
const confirmMock = vi.hoisted(() => vi.fn())
vi.mock('../lib/confirm', () => ({ confirm: confirmMock }))

const me = fixtureId(1)
const ada = dmMemberFixture(2, { display_name: 'Ada' })
const bo = dmMemberFixture(3, { display_name: 'Bo' })
const cy = dmMemberFixture(4, { display_name: 'Cy' })
const pair = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada], unread_count: 2 })
const group = dmFixture({ id: fixtureId(101), group: true, members: [dmMemberFixture(1), ada, bo] })
const global = { plugins: [i18nPlugin], directives: { tooltip } }

function member(m: typeof ada, extra: Partial<ChatMember> = {}): ChatMember {
  return { id: m.id, username: m.username, display_name: m.display_name, role: 'user', bio: '', status_text: '', created_at: '2026-01-01T00:00:00Z', ...extra } as ChatMember
}

function setup({ features = ['dm', 'blocks'], canStart = true } = {}) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me })
  useServerFeaturesStore().list = features
  usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [], can_start_direct_messages: canStart }
  chat.members = [member(ada), member(bo), member(cy), member(dmMemberFixture(5, { display_name: 'Bot' }), { is_bot: true })]
  return chat
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => []
  confirmMock.mockReset()
  localStorage.clear()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('SidebarDmSection', () => {
  it('shows nothing on a server without direct messages', () => {
    const chat = setup({ features: [] })
    chat.dms = [pair]
    expect(mount(SidebarDmSection, { global }).find('[data-testid="dm-section"]').exists()).toBe(false)
  })

  it('lists conversations with the other member’s name and an unread badge', async () => {
    const chat = setup()
    chat.dms = [pair, group]
    chat.readStates = { [pair.id]: { channel_id: pair.id, unread_count: 2, mention_count: 2, notify_level: 'all', last_read_at: null } }
    const w = mount(SidebarDmSection, { global })
    expect(w.findAll('[data-testid="dm-title"]').map(t => t.text())).toEqual(['Ada', 'Ada, Bo'])
    expect(w.get('[data-testid="dm-unread"]').text()).toBe('2')
  })

  it('opens a conversation on click and on Enter', async () => {
    const chat = setup()
    chat.dms = [pair, group]
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SidebarDmSection, { global })
    await w.get(`[data-dm="${pair.id}"]`).trigger('click')
    await w.get(`[data-dm="${group.id}"]`).trigger('keydown', { key: 'Enter' })
    expect(open.mock.calls.map(c => c[0])).toEqual([pair.id, group.id])
  })

  it('offers the new-conversation button only to a member who may start one', async () => {
    setup({ canStart: false })
    useChatStore().dms = [pair]
    expect(mount(SidebarDmSection, { global }).find('[data-testid="dm-new"]').exists()).toBe(false)
    setActivePinia(createPinia())
    setup()
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    expect(w.get('[data-testid="dm-empty"]').text()).toBe('No conversations yet')
    await w.get('[data-testid="dm-new"]').trigger('click')
    expect(document.querySelector('[data-testid="dm-create"]')).not.toBeNull()
  })

  it('collapses to the open conversation and the ones with news, remembering the choice', async () => {
    const chat = setup()
    chat.dms = [pair, group]
    chat.readStates = { [pair.id]: { channel_id: pair.id, unread_count: 2, mention_count: 2, notify_level: 'all', last_read_at: null } }
    const w = mount(SidebarDmSection, { global })
    await w.get('[data-testid="dm-toggle"]').trigger('click')
    expect(w.findAll('[data-dm]').map(r => r.attributes('data-dm'))).toEqual([pair.id])
    expect(localStorage.getItem('mnema_dm_collapsed')).toBe('1')
    expect(mount(SidebarDmSection, { global }).findAll('[data-dm]')).toHaveLength(1)
  })

  it('shows the newest eight and a button for the rest', async () => {
    const chat = setup()
    chat.dms = Array.from({ length: 11 }, (_, i) => dmFixture({ id: fixtureId(200 + i), members: [dmMemberFixture(1), dmMemberFixture(20 + i)] }))
    const w = mount(SidebarDmSection, { global })
    expect(w.findAll('[data-dm]')).toHaveLength(8)
    await w.get('[data-testid="dm-more"]').trigger('click')
    expect(w.findAll('[data-dm]')).toHaveLength(11)
  })
})

describe('DmCreateDialog', () => {
  it('offers members except oneself, bots and blocked members, narrowed by the search', async () => {
    const chat = setup()
    useBlocksStore().blocked = [{ user_id: bo.id, username: bo.username, display_name: 'Bo', blocked_at: '2026-01-01T00:00:00Z' }]
    chat.members = [member({ ...dmMemberFixture(1), display_name: 'Me' }), ...chat.members]
    const w = mount(DmCreateDialog, { global, attachTo: document.body })
    expect(w.findAll('[data-dm-candidate]').map(b => b.attributes('data-dm-candidate'))).toEqual([ada.username, cy.username])
    await w.get('[data-dm-search]').setValue('cy')
    expect(w.findAll('[data-dm-candidate]')).toHaveLength(1)
  })

  it('starts a conversation of two with one member and opens it', async () => {
    const chat = setup()
    http.handle = (_u, o) => (o?.method === 'POST' ? pair : [])
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(DmCreateDialog, { global, attachTo: document.body })
    expect(w.get('[data-testid="dm-submit"]').attributes('disabled')).toBeDefined()
    await w.get(`[data-dm-candidate="${ada.username}"]`).trigger('click')
    expect(w.get('[data-testid="dm-submit"]').text()).toBe('Start conversation')
    await w.get('[data-testid="dm-create"]').trigger('submit')
    await flushPromises()
    expect(open).toHaveBeenCalledWith(pair.id)
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('starts a group with several members, up to nine', async () => {
    setup()
    let sent: unknown
    http.handle = (u, o) => { if (u === '/api/dms') sent = o?.json; return o?.method === 'POST' ? group : [] }
    const w = mount(DmCreateDialog, { global, attachTo: document.body })
    await w.get(`[data-dm-candidate="${ada.username}"]`).trigger('click')
    await w.get(`[data-dm-candidate="${bo.username}"]`).trigger('click')
    expect(w.get('[data-testid="dm-submit"]').text()).toBe('Start group conversation')
    expect(w.get('[data-testid="dm-count"]').text()).toBe('2 of 9 chosen')
    await w.get('[data-testid="dm-create"]').trigger('submit')
    await flushPromises()
    expect(sent).toEqual({ user_ids: [ada.id, bo.id] })
  })

  it('answers every refusal with the same calm sentence', async () => {
    setup()
    http.handle = () => { throw new ApiError(403, 'FORBIDDEN', 'cannot') }
    const w = mount(DmCreateDialog, { global, attachTo: document.body })
    await w.get(`[data-dm-candidate="${ada.username}"]`).trigger('click')
    await w.get('[data-testid="dm-create"]').trigger('submit')
    await flushPromises()
    expect(w.get('[data-testid="dm-error"]').text()).toBe('Can’t send a message to this member.')
    expect(w.emitted('close')).toBeUndefined()
  })

  it('adds one member to a group and says when the group is full', async () => {
    const chat = setup()
    chat.dms = [group]
    http.handle = (_u, o) => (o?.method === 'POST' ? { ...group, members: [...group.members, cy] } : [])
    const w = mount(DmCreateDialog, { props: { group }, global, attachTo: document.body })
    expect(w.findAll('[data-dm-candidate]').map(b => b.attributes('data-dm-candidate'))).toEqual([cy.username])
    await w.get(`[data-dm-candidate="${cy.username}"]`).trigger('click')
    await w.get('[data-testid="dm-create"]').trigger('submit')
    await flushPromises()
    expect(chat.dmById(group.id)?.members).toHaveLength(4)
    const full = { ...group, members: Array.from({ length: 10 }, (_, i) => dmMemberFixture(i + 1)) }
    const crowded = mount(DmCreateDialog, { props: { group: full }, global, attachTo: document.body })
    expect(crowded.text()).toContain('This group is full')
  })
})

describe('DmCreateDialog limits', () => {
  it('takes no more than nine members besides the caller', async () => {
    const chat = setup()
    chat.members = Array.from({ length: 12 }, (_, i) => member(dmMemberFixture(10 + i)))
    const w = mount(DmCreateDialog, { global, attachTo: document.body })
    for (const button of w.findAll('[data-dm-candidate]')) await button.trigger('click')
    expect(w.get('[data-testid="dm-count"]').text()).toBe('9 of 9 chosen')
  })
})

describe('DmInfoDialog', () => {
  it('lists the members and adds or leaves in a group', async () => {
    const chat = setup()
    chat.dms = [group]
    const leave = vi.spyOn(chat, 'leaveDm').mockResolvedValue(undefined)
    confirmMock.mockResolvedValue(true)
    const w = mount(DmInfoDialog, { props: { dm: group }, global, attachTo: document.body })
    expect(w.text()).toContain('Members (3)')
    expect(w.text()).toContain('You')
    await w.get('[data-testid="dm-add-member"]').trigger('click')
    expect(document.querySelector('[data-testid="dm-create"]')).not.toBeNull()
    await w.get('[data-testid="dm-leave"]').trigger('click')
    await flushPromises()
    expect(leave).toHaveBeenCalledWith(group.id)
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('does not leave when the member backs out, and reports a failure', async () => {
    const chat = setup()
    chat.dms = [group]
    const leave = vi.spyOn(chat, 'leaveDm').mockRejectedValue(new Error('offline'))
    const w = mount(DmInfoDialog, { props: { dm: group }, global, attachTo: document.body })
    confirmMock.mockResolvedValue(false)
    await w.get('[data-testid="dm-leave"]').trigger('click')
    await flushPromises()
    expect(leave).not.toHaveBeenCalled()
    confirmMock.mockResolvedValue(true)
    await w.get('[data-testid="dm-leave"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('offline')
  })

  it('cannot leave a conversation of two and offers to block the other member instead', async () => {
    const chat = setup()
    chat.dms = [pair]
    http.handle = (url, o) => (url === '/api/me/blocks' ? [{ user_id: ada.id, username: 'member2', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }] : o?.method ? undefined : [])
    const w = mount(DmInfoDialog, { props: { dm: pair }, global, attachTo: document.body })
    expect(w.find('[data-testid="dm-leave"]').exists()).toBe(false)
    expect(w.find('[data-testid="dm-add-member"]').exists()).toBe(false)
    expect(w.get('[data-testid="dm-pair-hint"]').text()).toContain('can’t be left')
    await w.get('[data-testid="dm-block"]').trigger('click')
    await flushPromises()
    expect(useBlocksStore().isBlocked(ada.id)).toBe(true)
    expect(w.find('[data-testid="dm-unblock"]').exists()).toBe(true)
  })

  it('lifts a block from the info of a conversation of two, and closes the add dialog', async () => {
    const chat = setup()
    chat.dms = [pair]
    useBlocksStore().blocked = [{ user_id: ada.id, username: 'member2', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    http.handle = url => (url === '/api/me/blocks' ? [] : undefined)
    const w = mount(DmInfoDialog, { props: { dm: pair }, global, attachTo: document.body })
    await w.get('[data-testid="dm-unblock"]').trigger('click')
    await flushPromises()
    expect(useBlocksStore().isBlocked(ada.id)).toBe(false)
    const crowd = mount(DmInfoDialog, { props: { dm: group }, global, attachTo: document.body })
    await crowd.get('[data-testid="dm-add-member"]').trigger('click')
    ;[...document.querySelectorAll<HTMLElement>('[data-testid="dm-create"] button')].find(b => b.textContent?.trim() === 'Cancel')!.click()
    await flushPromises()
    expect(document.querySelector('[data-testid="dm-create"]')).toBeNull()
  })

  it('opens the profile of a member', async () => {
    const chat = setup()
    const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(undefined)
    const w = mount(DmInfoDialog, { props: { dm: group }, global, attachTo: document.body })
    await w.findAll('ul button')[1]!.trigger('click')
    expect(profile).toHaveBeenCalledWith(expect.objectContaining({ id: ada.id }))
    expect(w.emitted('close')).toHaveLength(1)
  })
})
