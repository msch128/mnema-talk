import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import SidebarDmSection from './SidebarDmSection.vue'
import DmHeaderTitle from './DmHeaderTitle.vue'
import DmMuteButton from './DmMuteButton.vue'
import AccountDirectMessages from './AccountDirectMessages.vue'
import SearchConversationJump from './SearchConversationJump.vue'
import DmAvatar from './DmAvatar.vue'
import DmListRow from './DmListRow.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import type { ApiOptions } from '../lib/api'
import type { ChatMember } from '../types/rest'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))
import { api } from '../lib/api'

const me = fixtureId(1)
const ada = dmMemberFixture(2, { display_name: 'Ada' })
const bo = dmMemberFixture(3, { display_name: 'Bo' })
const cy = dmMemberFixture(4, { display_name: 'Cy' })
const withAda = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada], last_message_at: '2026-01-03T10:00:00Z' })
const withBo = dmFixture({ id: fixtureId(101), members: [dmMemberFixture(1), bo], last_message_at: '2026-01-02T10:00:00Z' })
const crew = dmFixture({ id: fixtureId(102), group: true, members: [dmMemberFixture(1), ada, bo, cy], last_message_at: '2026-01-01T10:00:00Z' })
const global = { plugins: [i18nPlugin], directives: { tooltip } }

function member(m: typeof ada): ChatMember {
  return { id: m.id, username: m.username, display_name: m.display_name, role: 'user', bio: '', status_text: '', created_at: '2026-01-01T00:00:00Z' } as ChatMember
}

function setup(features = ['dm', 'dm.v2', 'blocks']) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me })
  useServerFeaturesStore().list = features
  usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [], can_start_direct_messages: true }
  chat.members = [member(ada), member(bo), member(cy)]
  return chat
}

const puts = () => vi.mocked(api).mock.calls.filter(([, o]) => o?.method === 'PUT').map(([url, o]) => [url, o?.json])
const rows = (w: ReturnType<typeof mount>) => w.findAll('[data-dm]').map(r => r.attributes('data-dm'))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => undefined
  vi.mocked(api).mockClear()
  localStorage.clear()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('the rich conversation list', () => {
  it('words each row with the latest message, who wrote it, when, and the group size', () => {
    const chat = setup()
    chat.dms = [
      { ...withAda, last_message: { user_id: me, text: 'see you **soon**', has_attachment: false, has_poll: false } },
      { ...crew, last_message: { user_id: bo.id, text: '', has_attachment: true, has_poll: false } },
      { ...withBo, last_message: { user_id: bo.id, text: 'Lunch?', has_attachment: false, has_poll: true } }
    ]
    const w = mount(SidebarDmSection, { global })
    expect(w.findAll('[data-testid="dm-preview"]').map(p => p.text())).toEqual(['You: see you soon', 'Bo: Attachment', 'Poll'])
    expect(w.get(`[data-dm="${crew.id}"]`).find('[data-testid="dm-count"]').text()).toBe('4')
    expect(w.get(`[data-dm="${crew.id}"]`).find('[data-testid="dm-avatar-stack"]').exists()).toBe(true)
    expect(w.get(`[data-dm="${withAda.id}"]`).find('[data-testid="dm-time"]').exists()).toBe(true)
  })

  it('shows mentions and plain unread differently and quiets a muted conversation', () => {
    const chat = setup()
    chat.dms = [withAda, withBo, crew]
    chat.readStates = {
      [withAda.id]: { channel_id: withAda.id, unread_count: 3, mention_count: 1, notify_level: 'all', last_read_at: null },
      [withBo.id]: { channel_id: withBo.id, unread_count: 2, mention_count: 0, notify_level: 'all', last_read_at: null },
      [crew.id]: { channel_id: crew.id, unread_count: 5, mention_count: 2, notify_level: 'mute', last_read_at: null }
    }
    const w = mount(SidebarDmSection, { global })
    expect(w.get(`[data-dm="${withAda.id}"] [data-testid="dm-mentions"]`).text()).toBe('@1')
    expect(w.get(`[data-dm="${withBo.id}"] [data-testid="dm-unread"]`).text()).toBe('2')
    const quiet = w.get(`[data-dm="${crew.id}"]`)
    expect(quiet.find('[data-testid="dm-mentions"]').exists()).toBe(false)
    expect(quiet.get('[data-testid="dm-unread"]').text()).toBe('5')
    expect(quiet.find('[data-testid="dm-muted"]').exists()).toBe(true)
  })

  it('keeps favourites on top in their saved order and leaves closed conversations out', () => {
    const chat = setup()
    chat.dms = [withAda, { ...withBo, favourite_position: 1 }, { ...crew, favourite_position: 0 }, { ...dmFixture({ id: fixtureId(103), members: [dmMemberFixture(1), cy] }), hidden: true }]
    const w = mount(SidebarDmSection, { global })
    expect(rows(w)).toEqual([crew.id, withBo.id, withAda.id])
    expect(w.get('[data-testid="dm-favourites-label"]').text()).toBe('Favourites')
    expect(w.findAll('[data-testid="dm-favourites"] [data-dm]')).toHaveLength(2)
  })

  it('keeps the open conversation visible although it was closed', () => {
    const chat = setup()
    chat.dms = [{ ...withAda, hidden: true }]
    chat.activeChannel = chat.dmChannel(withAda.id)
    expect(rows(mount(SidebarDmSection, { global }))).toEqual([withAda.id])
  })

  it('offers nothing new on a server without dm.v2', () => {
    const chat = setup(['dm', 'blocks'])
    chat.dms = [withAda]
    const w = mount(SidebarDmSection, { global })
    expect(w.find('[data-testid="dm-find"]').exists()).toBe(false)
    expect(w.find('[data-testid="dm-star"]').exists()).toBe(false)
    expect(w.find('[data-testid="dm-close"]').exists()).toBe(false)
  })
})

describe('closing and favouriting', () => {
  it('keeps the action buttons outside the button that opens the conversation', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.activeChannel = null
    const w = mount(SidebarDmSection, { global })
    const row = w.get(`[data-dm="${withAda.id}"]`)
    expect(row.attributes('role')).toBeUndefined()
    const opener = row.get('[data-testid="dm-open"]')
    expect(opener.find('button').exists()).toBe(false)
    expect(opener.element.contains(row.get('[data-testid="dm-star"]').element)).toBe(false)
  })

  it('names muted state and mentions in the row label', () => {
    const chat = setup()
    chat.dms = [withAda]
    const w = mount(DmListRow, { global, props: { dm: withAda, unread: 2, mentions: 1, muted: true } })
    const label = w.get('[data-testid="dm-open"]').attributes('aria-label')
    expect(label).toContain('1 unread mentions')
    expect(label).toContain('muted')
  })

  it('closes a conversation at once, saves it and brings it back with the next message', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    const w = mount(SidebarDmSection, { global })
    await w.get(`[data-dm="${withAda.id}"] [data-testid="dm-close"]`).trigger('click')
    expect(rows(w)).toEqual([withBo.id])
    expect(puts()).toEqual([[`/api/dms/${withAda.id}/hidden`, { hidden: true }]])
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(9), channel_id: withAda.id, user_id: ada.id, created_at: '2026-02-01T00:00:00Z', content: 'back again' }) })
    await flushPromises()
    expect(rows(w)).toEqual([withAda.id, withBo.id])
    expect(w.get(`[data-dm="${withAda.id}"] [data-testid="dm-preview"]`).text()).toBe('back again')
  })

  it('puts a closed conversation back and says so when the server refuses', async () => {
    const chat = setup()
    chat.dms = [withAda]
    http.handle = () => { throw new Error('offline') }
    const w = mount(SidebarDmSection, { global })
    await w.get('[data-testid="dm-close"]').trigger('click')
    await flushPromises()
    expect(rows(w)).toEqual([withAda.id])
    expect(useToastStore().toasts.map(t => t.text)).toContain('Couldn’t change the list. Try again.')
  })

  it('does not reopen a closed conversation for a blocked member’s message', async () => {
    const chat = setup()
    chat.dms = [{ ...withAda, hidden: true }]
    const { useBlocksStore } = await import('../stores/blocks')
    useBlocksStore().blocked = [{ user_id: ada.id, username: ada.username, display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(9), channel_id: withAda.id, user_id: ada.id }) })
    expect(chat.dmById(withAda.id)?.hidden).toBe(true)
  })

  it('reopens a closed conversation when it is opened', async () => {
    const chat = setup()
    chat.dms = [{ ...withAda, hidden: true }]
    await chat.openDm(withAda.id)
    await flushPromises()
    expect(puts()).toEqual([[`/api/dms/${withAda.id}/hidden`, { hidden: false }]])
    expect(chat.dmById(withAda.id)?.hidden).toBe(false)
  })

  it('favourites with the star and unfavourites again, in the order saved', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    const w = mount(SidebarDmSection, { global })
    await w.get(`[data-dm="${withBo.id}"] [data-testid="dm-star"]`).trigger('click')
    await w.get(`[data-dm="${withAda.id}"] [data-testid="dm-star"]`).trigger('click')
    expect(puts()).toEqual([['/api/dms/favourites', { ids: [withBo.id] }], ['/api/dms/favourites', { ids: [withBo.id, withAda.id] }]])
    expect(rows(w)).toEqual([withBo.id, withAda.id])
    expect(w.find(`[data-dm="${withBo.id}"] [data-testid="dm-close"]`).exists()).toBe(false)
    await w.get(`[data-dm="${withBo.id}"] [data-testid="dm-star"]`).trigger('click')
    expect(puts()[2]).toEqual(['/api/dms/favourites', { ids: [withAda.id] }])
  })

  it('reorders favourites by keyboard and by dragging', async () => {
    const chat = setup()
    chat.dms = [{ ...withAda, favourite_position: 0 }, { ...withBo, favourite_position: 1 }, { ...crew, favourite_position: 2 }]
    const w = mount(SidebarDmSection, { global })
    await w.get(`[data-dm="${withAda.id}"] [data-testid="dm-open"]`).trigger('keydown', { key: 'ArrowDown', altKey: true })
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [withBo.id, withAda.id, crew.id] }])
    await w.get(`[data-dm="${crew.id}"]`).trigger('dragstart')
    await w.get(`[data-dm="${withBo.id}"]`).trigger('dragover', { clientY: 0 })
    await w.get('[data-testid="dm-favourites"]').trigger('drop')
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [crew.id, withBo.id, withAda.id] }])
  })

  it('reads the real favourites back when the server refuses a change', async () => {
    const chat = setup()
    chat.dms = [withAda]
    http.handle = (url, o) => { if (o?.method === 'PUT') throw new Error('nope'); return url === '/api/dms' ? [withAda] : { mute_all: false, list_collapsed: false } }
    const w = mount(SidebarDmSection, { global })
    await w.get('[data-testid="dm-star"]').trigger('click')
    await flushPromises()
    expect(chat.dmById(withAda.id)?.favourite_position).toBeNull()
    expect(useToastStore().toasts).toHaveLength(1)
  })
})

describe('the context menu', () => {
  it('offers favourite, mute and close, and acts on the choice', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.readStates = { [withAda.id]: { channel_id: withAda.id, unread_count: 1, mention_count: 0, notify_level: 'all', last_read_at: null } }
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    await w.get(`[data-dm="${withAda.id}"]`).trigger('contextmenu', { clientX: 10, clientY: 10 })
    await flushPromises()
    const labels = [...document.querySelectorAll('[role="menuitem"]')].map(i => i.textContent?.trim())
    expect(labels).toEqual(['Mark as read', 'Add to favourites', 'Mute conversation', 'Close conversation'])
    ;(document.querySelector('[role="menuitem"][data-id="close"], [role="menuitem"]:last-of-type') as HTMLElement).click()
    await flushPromises()
    expect(puts().at(-1)).toEqual([`/api/dms/${withAda.id}/hidden`, { hidden: true }])
  })

  it('does not offer to close a favourite but offers to move it', async () => {
    const chat = setup()
    chat.dms = [{ ...withAda, favourite_position: 0 }, { ...withBo, favourite_position: 1 }]
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    await w.get(`[data-dm="${withAda.id}"]`).trigger('contextmenu', { clientX: 10, clientY: 10 })
    await flushPromises()
    const labels = [...document.querySelectorAll('[role="menuitem"]')].map(i => i.textContent?.trim())
    expect(labels).toEqual(['Remove from favourites', 'Move down', 'Mute conversation'])
  })
})

describe('finding and starting a conversation', () => {
  it('opens the first matching conversation on Enter', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SidebarDmSection, { global })
    const input = w.get('[data-testid="dm-find-input"]')
    await input.setValue('bo')
    expect(w.findAll('[data-find]').map(r => r.attributes('data-find'))).toEqual(['dm'])
    await input.trigger('keydown', { key: 'Enter' })
    expect(open).toHaveBeenCalledWith(withBo.id)
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('does not pick a result when Enter commits an IME composition', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SidebarDmSection, { global })
    const input = w.get('[data-testid="dm-find-input"]')
    await input.setValue('bo')
    await input.trigger('keydown', { key: 'Enter', isComposing: true })
    await input.trigger('keydown', { key: 'Enter', keyCode: 229 })
    expect(open).not.toHaveBeenCalled()
    await input.trigger('keydown', { key: 'Enter' })
    expect(open).toHaveBeenCalledWith(withBo.id)
  })

  it('starts a conversation with a member who has none yet and moves with the arrow keys', async () => {
    const chat = setup()
    chat.dms = [withAda]
    http.handle = (url, o) => (url === '/api/dms' && o?.method === 'POST' ? dmFixture({ id: fixtureId(110), members: [dmMemberFixture(1), cy] }) : undefined)
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SidebarDmSection, { global })
    const input = w.get('[data-testid="dm-find-input"]')
    await input.setValue('c')
    expect(w.findAll('[data-find]').map(r => r.attributes('data-find'))).toEqual(['member'])
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(vi.mocked(api).mock.calls.find(([url]) => url === '/api/dms')?.[1]?.json).toEqual({ user_ids: [cy.id] })
    expect(open).toHaveBeenCalledWith(fixtureId(110))
  })

  it('says when nothing matches and clears on Escape', async () => {
    const chat = setup()
    chat.dms = [withAda]
    const w = mount(SidebarDmSection, { global })
    const input = w.get('[data-testid="dm-find-input"]')
    await input.setValue('zzz')
    expect(w.get('[data-testid="dm-find-empty"]').text()).toBe('No match')
    await input.trigger('keydown', { key: 'Escape' })
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('also offers the matching conversations in the Ctrl/Cmd+K search', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SearchConversationJump, { global, props: { text: 'ad' } })
    expect(w.findAll('[data-jump]').map(b => b.attributes('data-jump'))).toEqual([withAda.id])
    await w.get('[data-jump]').trigger('click')
    await flushPromises()
    expect(open).toHaveBeenCalledWith(withAda.id)
    expect(w.emitted('jumped')).toHaveLength(1)
    expect(mount(SearchConversationJump, { global, props: { text: '' } }).find('[data-testid="search-jump"]').exists()).toBe(false)
  })
})

describe('folding the section', () => {
  it('saves the folded state for the member and follows what the server says', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.dmSettingsLoaded = true
    http.handle = (_url, o) => o?.json ?? { mute_all: false, list_collapsed: true }
    const w = mount(SidebarDmSection, { global })
    await w.get('[data-testid="dm-toggle"]').trigger('click')
    await flushPromises()
    expect(puts()).toEqual([['/api/users/me/dm-settings', { mute_all: false, list_collapsed: true }]])
    expect(rows(w)).toEqual([])
    chat.dmSettings = { mute_all: false, list_collapsed: false }
    await flushPromises()
    expect(rows(w)).toEqual([withAda.id])
  })
})

describe('events from another device', () => {
  it('reads one conversation again, or the whole list', async () => {
    const chat = setup()
    chat.dms = [withAda]
    http.handle = url => (url === `/api/dms/${withAda.id}` ? { ...withAda, hidden: true } : url === '/api/dms' ? [{ ...withAda, favourite_position: 0 }] : { mute_all: true, list_collapsed: true })
    chat.handleWSEvent({ type: 'dm_prefs_updated', payload: { dm_id: withAda.id } })
    await flushPromises()
    expect(chat.dmById(withAda.id)?.hidden).toBe(true)
    chat.handleWSEvent({ type: 'dm_prefs_updated', payload: { dm_id: null } })
    await flushPromises()
    expect(chat.dmById(withAda.id)?.favourite_position).toBe(0)
    expect(chat.dmSettings).toEqual({ mute_all: true, list_collapsed: true })
  })

  it('reads the preview of a group the member was added to', async () => {
    const chat = setup()
    http.handle = () => ({ ...crew, last_message: { user_id: bo.id, text: 'welcome', has_attachment: false, has_poll: false } })
    chat.handleWSEvent({ type: 'dm_created', payload: crew })
    await flushPromises()
    expect(chat.dmById(crew.id)?.last_message?.text).toBe('welcome')
  })
})

describe('the preview after an edit or a delete', () => {
  const preview = (text: string) => ({ ...withAda, last_message: { user_id: ada.id, text, has_attachment: false, has_poll: false } })

  it('shows the edited text and drops a deleted message for the one before', async () => {
    const chat = setup()
    chat.dms = [preview('secret')]
    http.handle = () => preview('fixed')
    chat.handleWSEvent({ type: 'message_update', payload: messageFixture({ id: fixtureId(9), channel_id: withAda.id, content: 'fixed' }) })
    await flushPromises()
    expect(chat.dmById(withAda.id)?.last_message?.text).toBe('fixed')
    http.handle = () => ({ ...withAda, last_message: undefined })
    chat.handleWSEvent({ type: 'message_delete', payload: { id: fixtureId(9), channel_id: withAda.id, parent_id: null } })
    await flushPromises()
    expect(chat.dmById(withAda.id)?.last_message).toBeUndefined()
    expect(vi.mocked(api).mock.calls.filter(([url]) => url === `/api/dms/${withAda.id}`)).toHaveLength(2)
  })

  it('ignores changes in channels', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.handleWSEvent({ type: 'message_delete', payload: { id: fixtureId(9), channel_id: fixtureId(50), parent_id: null } })
    await flushPromises()
    expect(vi.mocked(api).mock.calls.filter(([url]) => String(url).startsWith('/api/dms/'))).toHaveLength(0)
  })
})

describe('the header of a conversation', () => {
  it('shows the face and name of the other member and opens their profile', async () => {
    const chat = setup()
    chat.dms = [withAda]
    const profile = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(undefined)
    const w = mount(DmHeaderTitle, { global, props: { dm: withAda } })
    expect(w.get('[data-testid="chat-channel-name"]').text()).toBe('Ada')
    expect(w.find('[data-testid="dm-header-members"]').exists()).toBe(false)
    await w.get('[data-testid="dm-header-title"]').trigger('click')
    expect(profile).toHaveBeenCalledWith(expect.objectContaining({ id: ada.id }))
  })

  it('counts a group’s members and opens its member list', async () => {
    setup()
    const w = mount(DmHeaderTitle, { global, props: { dm: crew } })
    expect(w.get('[data-testid="dm-header-members"]').text()).toBe('4 members')
    await w.get('[data-testid="dm-header-title"]').trigger('click')
    expect(w.emitted('members')).toHaveLength(1)
  })

  it('mutes and unmutes the conversation', async () => {
    const chat = setup()
    chat.dms = [withAda]
    http.handle = () => ({ channels: [], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: false } })
    const w = mount(DmMuteButton, { global, props: { dm: withAda } })
    await w.get('[data-testid="dm-mute"]').trigger('click')
    await flushPromises()
    expect(puts()[0]).toEqual([`/api/channels/${withAda.id}/notifications`, { level: 'mute', muted_until: null }])
    expect(w.get('[data-testid="dm-mute"]').attributes('aria-pressed')).toBe('true')
    await w.get('[data-testid="dm-mute"]').trigger('click')
    await flushPromises()
    expect(puts()[1]).toEqual([`/api/channels/${withAda.id}/notifications`, { level: 'inherit', muted_until: null }])
  })

  it('unmutes to "all" when every conversation is muted, so the conversation wins over the general setting', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.dmSettings = { mute_all: true, list_collapsed: false }
    chat.readStates = { [withAda.id]: { channel_id: withAda.id, unread_count: 0, mention_count: 0, notify_level: 'mute', last_read_at: null } }
    http.handle = () => ({ channels: [], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: false } })
    const w = mount(DmMuteButton, { global, props: { dm: withAda } })
    await w.get('[data-testid="dm-mute"]').trigger('click')
    await flushPromises()
    expect(puts()[0]).toEqual([`/api/channels/${withAda.id}/notifications`, { level: 'all', muted_until: null }])
  })
})

describe('Direct messages in the privacy settings', () => {
  it('explains who may write and mutes all conversations, going back when saving fails', async () => {
    const chat = setup()
    http.handle = (url, o) => {
      if (url === '/api/users/me/privacy') return { dm_policy: 'everyone' }
      if (url === '/api/users/me/dm-settings') return o?.method === 'PUT' ? o.json : { mute_all: true, list_collapsed: false }
      return []
    }
    const w = mount(AccountDirectMessages, { global })
    await flushPromises()
    expect(w.text()).toContain('Who can send me direct messages')
    const toggle = w.get('[data-testid="dm-mute-all"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    await flushPromises()
    expect(puts()).toEqual([['/api/users/me/dm-settings', { mute_all: true, list_collapsed: false }]])
    expect(chat.dmSettings.mute_all).toBe(true)
    expect(toggle.attributes('aria-checked')).toBe('true')

    http.handle = (url, o) => { if (o?.method === 'PUT') throw new Error('offline'); return url === '/api/users/me/privacy' ? { dm_policy: 'everyone' } : [] }
    await toggle.trigger('click')
    await flushPromises()
    expect(chat.dmSettings.mute_all).toBe(true)
    expect(useToastStore().toasts).toHaveLength(1)
  })

  it('hides the mute switch on a server without dm.v2', async () => {
    setup(['dm'])
    http.handle = () => ({ dm_policy: 'everyone' })
    const w = mount(AccountDirectMessages, { global })
    await flushPromises()
    expect(w.find('[data-testid="dm-mute-all"]').exists()).toBe(false)
  })
})

describe('every entry of the context menu', () => {
  async function menuFor(w: ReturnType<typeof mount>, id: string) {
    await w.get(`[data-dm="${id}"]`).trigger('contextmenu', { clientX: 10, clientY: 10 })
    await flushPromises()
    return [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
  }
  const click = async (items: HTMLElement[], label: string) => {
    items.find(i => i.textContent?.trim() === label)!.click()
    await flushPromises()
  }

  it('marks read, favourites, mutes and unmutes', async () => {
    const chat = setup()
    chat.dms = [withAda]
    chat.readStates = { [withAda.id]: { channel_id: withAda.id, unread_count: 4, mention_count: 0, notify_level: 'all', last_read_at: null } }
    http.handle = () => ({ channels: [], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: false } })
    const read = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(true)
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    await click(await menuFor(w, withAda.id), 'Mark as read')
    expect(read).toHaveBeenCalledWith(withAda.id)
    await click(await menuFor(w, withAda.id), 'Add to favourites')
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [withAda.id] }])
    await click(await menuFor(w, withAda.id), 'Mute conversation')
    expect(puts().at(-1)).toEqual([`/api/channels/${withAda.id}/notifications`, { level: 'mute', muted_until: null }])
    await click(await menuFor(w, withAda.id), 'Unmute conversation')
    expect(puts().at(-1)).toEqual([`/api/channels/${withAda.id}/notifications`, { level: 'inherit', muted_until: null }])
    await click(await menuFor(w, withAda.id), 'Remove from favourites')
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [] }])
  })

  it('moves a favourite up and down', async () => {
    const chat = setup()
    chat.dms = [{ ...withAda, favourite_position: 0 }, { ...withBo, favourite_position: 1 }, { ...crew, favourite_position: 2 }]
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    await click(await menuFor(w, withBo.id), 'Move up')
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [withBo.id, withAda.id, crew.id] }])
    await click(await menuFor(w, withBo.id), 'Move down')
    expect(puts().at(-1)).toEqual(['/api/dms/favourites', { ids: [withAda.id, withBo.id, crew.id] }])
  })

  it('opens from the keyboard and from the more button', async () => {
    const chat = setup()
    chat.dms = [withAda]
    const w = mount(SidebarDmSection, { global, attachTo: document.body })
    await w.get(`[data-dm="${withAda.id}"] [data-testid="dm-open"]`).trigger('keydown', { key: 'F10', shiftKey: true })
    await flushPromises()
    expect(document.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(0)
    await w.get('[data-testid="dm-more-actions"]').trigger('click')
    await flushPromises()
    expect(document.querySelectorAll('[role="menuitem"]').length).toBeGreaterThan(0)
  })
})

describe('DmAvatar', () => {
  it('shows a person, two faces of a group, or an icon for a conversation with nobody', () => {
    setup()
    expect(mount(DmAvatar, { global, props: { dm: withAda } }).find('[data-testid="dm-avatar-stack"]').exists()).toBe(false)
    expect(mount(DmAvatar, { global, props: { dm: crew } }).find('[data-testid="dm-avatar-stack"]').exists()).toBe(true)
    const alone = dmFixture({ id: fixtureId(120), group: true, members: [dmMemberFixture(1)] })
    const w = mount(DmAvatar, { global, props: { dm: alone } })
    expect(w.find('[data-testid="dm-avatar-stack"]').exists()).toBe(false)
    expect(w.find('svg').exists()).toBe(true)
  })
})

describe('the find box edge cases', () => {
  it('wraps around with the arrow keys, ignores Enter without a match and keeps the text when starting fails', async () => {
    const chat = setup()
    chat.dms = [withAda, withBo]
    http.handle = () => { throw new Error('refused') }
    const open = vi.spyOn(chat, 'openDm').mockResolvedValue(undefined)
    const w = mount(SidebarDmSection, { global })
    const input = w.get('[data-testid="dm-find-input"]')
    await input.trigger('keydown', { key: 'Escape' })
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.setValue('zzz')
    await input.trigger('keydown', { key: 'Enter' })
    expect(open).not.toHaveBeenCalled()
    await input.setValue('c')
    await input.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect((input.element as HTMLInputElement).value).toBe('c')
    await input.setValue('e')
    expect(w.findAll('[data-find]').length).toBeGreaterThan(1)
    await input.trigger('keydown', { key: 'ArrowUp' })
    expect(w.findAll('[role="option"]').at(-1)!.attributes('aria-selected')).toBe('true')
    await w.findAll('[role="option"]')[0]!.trigger('click')
    expect(open).toHaveBeenCalled()
  })
})
