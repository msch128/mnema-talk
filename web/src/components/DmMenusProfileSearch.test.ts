import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import UserProfileModal from './UserProfileModal.vue'
import SearchModal from './SearchModal.vue'
import { buildMemberItems } from '../composables/useNavMenus'
import { messageSafetyItems, memberRelationItems } from '../composables/dmMenuItems'
import { useDmActions } from '../composables/useDmActions'
import { searchOpen, searchScope, searchText } from '../lib/globalSearch'
import { pendingReport, closeReport } from '../lib/reportDialog'
import { dmErrorText, dmTitle, sortDms } from '../lib/dm'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
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

const me = fixtureId(1)
const ada = dmMemberFixture(2, { display_name: 'Ada' })
const global = { plugins: [i18nPlugin], directives: { tooltip } }
const blockedAda = { user_id: ada.id, username: ada.username, display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }

function member(m: typeof ada, extra: Partial<ChatMember> = {}): ChatMember {
  return { id: m.id, username: m.username, display_name: m.display_name, role: 'user', bio: '', status_text: '', created_at: '2026-01-01T00:00:00Z', ...extra } as ChatMember
}

function setup({ features = ['dm', 'blocks', 'reports'], canStart = true } = {}) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me })
  useServerFeaturesStore().list = features
  usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [], can_start_direct_messages: canStart }
  chat.members = [member(ada), member(dmMemberFixture(5, { display_name: 'Robo' }), { is_bot: true })]
  return chat
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => []
  closeReport()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('the profile card', () => {
  function profile(user = member(ada)) {
    const chat = setup()
    chat.selectedUserProfile = { id: user.id, username: user.username, display_name: user.display_name, role: 'user' }
    return mount(UserProfileModal, { global, attachTo: document.body })
  }

  it('starts a conversation with the member and closes', async () => {
    http.handle = (url, o) => (url === '/api/dms' && o?.method === 'POST' ? dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] }) : [])
    const w = profile()
    await w.get('[data-testid="profile-message"]').trigger('click')
    await flushPromises()
    expect(useChatStore().activeChannel).toMatchObject({ id: fixtureId(100), name: 'Ada' })
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('shows the calm refusal and stays open when the conversation cannot be started', async () => {
    http.handle = (url, o) => { if (url === '/api/dms' && o?.method === 'POST') throw new ApiError(403, 'FORBIDDEN', 'x'); return [] }
    const w = profile()
    await w.get('[data-testid="profile-message"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Can’t send a message to this member.'])
    expect(w.emitted('close')).toBeUndefined()
  })

  it('blocks and unblocks the member from the card', async () => {
    let blockedNow = false
    http.handle = (url, o) => (url === '/api/me/blocks' && !o?.method ? (blockedNow ? [blockedAda] : []) : undefined)
    const w = profile()
    expect(w.get('[data-testid="profile-block"]').text()).toBe('Block')
    blockedNow = true
    await w.get('[data-testid="profile-block"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="profile-block"]').text()).toBe('Unblock')
    expect(useToastStore().toasts.map(t => t.text)).toContain('Blocked Ada. Their messages are folded away.')
    blockedNow = false
    await w.get('[data-testid="profile-block"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="profile-block"]').text()).toBe('Block')
  })

  it('offers neither on one’s own card, for a bot, or without the features', async () => {
    const own = profile(member(dmMemberFixture(1)))
    expect(own.find('[data-testid="profile-message"]').exists()).toBe(false)
    expect(own.find('[data-testid="profile-block"]').exists()).toBe(false)
    const bot = profile(member(dmMemberFixture(5), { is_bot: true }))
    expect(bot.find('[data-testid="profile-message"]').exists()).toBe(false)
    expect(bot.find('[data-testid="profile-block"]').exists()).toBe(false)
    document.body.innerHTML = ''
    setActivePinia(createPinia())
    const chat = setup({ features: [] })
    chat.selectedUserProfile = { id: ada.id, username: ada.username, display_name: 'Ada', role: 'user' }
    const old = mount(UserProfileModal, { global, attachTo: document.body })
    expect(old.find('[data-testid="profile-message"]').exists()).toBe(false)
    expect(old.find('[data-testid="profile-block"]').exists()).toBe(false)
  })

  it('hides "Message" for a member the caller blocked and without the permission', () => {
    setup({ canStart: false })
    expect(useDmActions().canMessage(ada.id)).toBe(false)
    setActivePinia(createPinia())
    setup()
    useBlocksStore().blocked = [blockedAda]
    expect(useDmActions().canMessage(ada.id)).toBe(false)
    expect(useDmActions().canBlock(ada.id)).toBe(true)
  })
})

describe('menus', () => {
  it('adds "Send message" and "Block" to a member’s menu, and swaps the latter for "Unblock"', () => {
    setup()
    expect(memberRelationItems(member(ada)).map(i => i.label)).toEqual(['Message', 'Block'])
    expect(buildMemberItems(member(ada)).map(i => i.label)).toEqual(expect.arrayContaining(['Message', 'Block']))
    useBlocksStore().blocked = [blockedAda]
    expect(memberRelationItems(member(ada)).map(i => i.label)).toEqual(['Unblock'])
  })

  it('offers nothing about oneself or on an old server', () => {
    setup()
    expect(memberRelationItems(member(dmMemberFixture(1)))).toEqual([])
    setActivePinia(createPinia())
    setup({ features: [] })
    expect(memberRelationItems(member(ada))).toEqual([])
  })

  it('runs the entries: opens the conversation, blocks and unblocks', async () => {
    const chat = setup()
    http.handle = (url, o) => {
      if (url === '/api/dms' && o?.method === 'POST') return dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] })
      return url === '/api/me/blocks' && !o?.method ? [blockedAda] : undefined
    }
    const [message, block] = memberRelationItems(member(ada))
    message!.action?.()
    await flushPromises()
    expect(chat.activeChannel?.id).toBe(fixtureId(100))
    block!.action?.()
    await flushPromises()
    expect(useBlocksStore().isBlocked(ada.id)).toBe(true)
    http.handle = (url, o) => (url === '/api/me/blocks' && !o?.method ? [] : undefined)
    memberRelationItems(member(ada))[0]!.action?.()
    await flushPromises()
    expect(useBlocksStore().isBlocked(ada.id)).toBe(false)
  })

  it('adds "Report message" and "Block" to a message of somebody else only', () => {
    setup()
    const theirs = messageFixture({ user_id: ada.id, username: ada.username, display_name: 'Ada' })
    const items = messageSafetyItems(theirs)
    expect(items.map(i => i.label)).toEqual(['Report message', 'Block'])
    expect(messageSafetyItems(messageFixture({ user_id: me }))).toEqual([])
    expect(messageSafetyItems(messageFixture({ user_id: fixtureId(5), is_bot: true })).map(i => i.label)).toEqual(['Report message'])
    items[0]!.action?.()
    expect(pendingReport.value).toEqual({ messageId: theirs.id, author: 'Ada' })
  })

  it('leaves out the report entry when the server has no reports', () => {
    setup({ features: ['blocks'] })
    expect(messageSafetyItems(messageFixture({ user_id: ada.id })).map(i => i.label)).toEqual(['Block'])
  })
})

describe('search in direct messages', () => {
  const opened: { unmount: () => void }[] = []
  afterEach(() => { for (const w of opened.splice(0)) w.unmount() })

  async function open(scope: 'channels' | 'dm', text = 'hello') {
    const chat = setup()
    chat.dms = [dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] })]
    searchText.value = text
    searchScope.value = scope
    const w = mount(SearchModal, { global, attachTo: document.body, props: { modelValue: false } })
    opened.push(w)
    await w.setProps({ modelValue: true })
    await nextTick()
    await flushPromises()
    return w
  }

  afterEach(() => { searchOpen.value = false; searchText.value = ''; searchScope.value = 'channels' })

  it('asks for the member’s direct messages with dm=true and names the conversation in the results', async () => {
    const urls: string[] = []
    http.handle = url => { urls.push(url); return { messages: [messageFixture({ channel_id: fixtureId(100), content: 'hello there' })], has_more: false } }
    await open('dm', 'hello in:Ada')
    await vi.waitFor(() => expect(urls.some(u => u.startsWith('/api/search'))).toBe(true))
    const query = new URLSearchParams(urls.find(u => u.startsWith('/api/search'))!.split('?')[1])
    expect(query.get('dm')).toBe('true')
    expect(query.get('channel_id')).toBe(fixtureId(100))
    expect(query.get('q')).toBe('hello')
    await flushPromises()
    expect(document.body.textContent).toContain('Ada')
  })

  it('does not send dm for channel searches, and switches scope with the buttons', async () => {
    const urls: string[] = []
    http.handle = url => { urls.push(url); return { messages: [], has_more: false } }
    const w = await open('channels')
    await vi.waitFor(() => expect(urls).toHaveLength(1))
    expect(urls[0]).not.toContain('dm=')
    document.querySelector<HTMLElement>('[data-scope="dm"]')!.click()
    await vi.waitFor(() => expect(urls).toHaveLength(2))
    expect(urls[1]).toContain('dm=true')
    expect(document.querySelector('[data-scope="dm"]')!.getAttribute('aria-pressed')).toBe('true')
    expect(w.exists()).toBe(true)
  })

  it('shows no scope switch on a server without direct messages', async () => {
    const chat = setup({ features: [] })
    chat.dms = []
    const w = mount(SearchModal, { global, attachTo: document.body, props: { modelValue: true } })
    await nextTick()
    expect(document.querySelector('[data-testid="search-scope"]')).toBeNull()
    w.unmount()
  })
})

describe('lib/dm', () => {
  it('names a conversation after the others, sorts by activity and words each refusal', () => {
    const pair = dmFixture({ members: [dmMemberFixture(1), ada] })
    expect(dmTitle(pair, me)).toBe('Ada')
    expect(dmTitle({ members: [dmMemberFixture(1)] }, me)).toBe('Only you')
    const quiet = dmFixture({ id: fixtureId(1), last_message_at: '2026-01-02T00:00:00Z' })
    const fresh = dmFixture({ id: fixtureId(2), last_message_at: '2026-01-05T00:00:00Z' })
    const none = dmFixture({ id: fixtureId(3), last_message_at: null, created_at: '2026-01-03T00:00:00Z' })
    expect(sortDms([quiet, none, fresh]).map(d => d.id)).toEqual([fresh.id, none.id, quiet.id])
    expect(dmErrorText(new ApiError(403, 'FORBIDDEN', 'x'), 'start')).toBe('Can’t send a message to this member.')
    expect(dmErrorText(new ApiError(403, 'RULES_NOT_ACCEPTED', 'x'), 'send')).toBe('Couldn’t send the message. Try again.')
    expect(dmErrorText(new ApiError(409, 'CONFLICT', 'x'), 'add')).toBe('This group is full. A group takes up to ten people.')
    expect(dmErrorText(new ApiError(409, 'CONFLICT', 'x'), 'start')).toBe('You are in too many group conversations. Leave one first.')
    expect(dmErrorText(new ApiError(429, 'RATE_LIMITED', 'x'), 'start')).toBe('Too many tries. Wait a moment, then try again.')
    expect(dmErrorText(new Error('x'), 'add')).toBe('Couldn’t add the member. Try again.')
  })
})
