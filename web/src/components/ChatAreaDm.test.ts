import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatArea from './ChatArea.vue'
import ChatHeader from './ChatHeader.vue'
import BlockedMessageRow from './BlockedMessageRow.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { i18nPlugin, setLocale } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import { searchScope } from '../lib/globalSearch'

const http = vi.hoisted(() => ({ handle: (_url: string, _opts?: ApiOptions): unknown => [] }))
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn((url: string, options?: ApiOptions) => {
    try { return Promise.resolve(http.handle(url, options)) } catch (error) { return Promise.reject(error) }
  })
}))
const openSearch = vi.hoisted(() => vi.fn())
vi.mock('../lib/globalSearch', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/globalSearch')>()),
  openSearch: (q?: string, scope?: string) => openSearch(q, scope)
}))

const ada = dmMemberFixture(2, { display_name: 'Ada' })
const pair = dmFixture({ id: fixtureId(100), members: [dmMemberFixture(1), ada] })
const group = dmFixture({ id: fixtureId(101), group: true, members: [dmMemberFixture(1), ada, dmMemberFixture(3, { display_name: 'Bo' })] })
const global = { plugins: [i18nPlugin], directives: { tooltip } }

async function open(dm = pair, features = ['dm', 'blocks']) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: fixtureId(1), username: 'member1' })
  const serverFeatures = useServerFeaturesStore()
  serverFeatures.list = features
  // The header asks the server for its features; the test decides them.
  vi.spyOn(serverFeatures, 'load').mockResolvedValue(undefined)
  chat.dms = [dm]
  chat.messages = []
  await chat.openDm(dm.id)
  return chat
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  http.handle = () => []
  openSearch.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})
afterEach(() => {
  useChatStore().resetCommunityState()
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('a conversation in the chat area', () => {
  it('writes to the other member, with the conversation’s own words', async () => {
    await open()
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.get('textarea').attributes('placeholder')).toBe('Message Ada')
    expect(w.text()).toContain('Your conversation with Ada')
    expect(w.get('[data-testid="chat-channel-name"]').text()).toBe('Ada')
  })

  it('opens an empty conversation with the conversation icon, not the channel hash', async () => {
    await open()
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.find('svg.lucide-message-circle').exists()).toBe(true)
    expect(w.find('svg.lucide-hash').exists()).toBe(false)
  })

  it('follows the server’s can_send, not the channel permissions', async () => {
    const chat = await open({ ...pair, can_send: false })
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.get('textarea').attributes('placeholder')).toBe('You can’t write in direct messages right now.')
    expect(w.get('textarea').attributes('disabled')).toBeDefined()
    chat.dms = [{ ...pair, can_send: true }]
    await flushPromises()
    expect(w.get('textarea').attributes('disabled')).toBeUndefined()
  })

  it('sends a message under the conversation’s id', async () => {
    http.handle = (url, o) => (o?.method === 'POST' && url.endsWith('/messages') ? messageFixture({ id: fixtureId(50), channel_id: pair.id, content: 'hello' }) : [])
    await open()
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    await w.get('textarea').setValue('hello')
    await w.get('textarea').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const { api } = await import('../lib/api')
    expect(vi.mocked(api).mock.calls.some(([u, o]) => u === `/api/channels/${pair.id}/messages` && o?.method === 'POST')).toBe(true)
  })

  it('shows one calm sentence when the server refuses the message, and keeps the text', async () => {
    http.handle = (url, o) => {
      if (o?.method === 'POST' && url.endsWith('/messages')) throw new ApiError(403, 'FORBIDDEN', 'You cannot send messages to this member')
      return []
    }
    await open()
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    await w.get('textarea').setValue('hello')
    await w.get('textarea').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['Can’t send a message to this member.'])
    expect((w.get('textarea').element as HTMLTextAreaElement).value).toBe('hello')
  })

  it('keeps a failure that is not a refusal in its own words, and words a group by its members', async () => {
    http.handle = (url, o) => {
      if (o?.method === 'POST' && url.endsWith('/messages')) throw new ApiError(400, 'INVALID_INPUT', 'The message is too long')
      return []
    }
    await open(group)
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain('Group conversation with Ada, Bo')
    await w.get('textarea').setValue('hello')
    await w.get('textarea').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toEqual(['The message is too long'])
  })

  it('folds the messages of a blocked member and shows one on request', async () => {
    const chat = await open()
    useBlocksStore().blocked = [{ user_id: ada.id, username: 'member2', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    chat.messages = [
      messageFixture({ id: fixtureId(60), channel_id: pair.id, user_id: ada.id, username: 'member2', display_name: 'Ada', content: 'secret words' }),
      messageFixture({ id: fixtureId(61), channel_id: pair.id, user_id: fixtureId(1), content: 'my words' })
    ]
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.text()).not.toContain('secret words')
    expect(w.text()).toContain('my words')
    expect(w.text()).toContain('This is the start of your conversation with Ada.')
    const folded = w.get('[data-testid="blocked-message"]')
    expect(folded.attributes('data-msg-id')).toBe(fixtureId(60))
    const thread = mount(BlockedMessageRow, { props: { msg: messageFixture({ id: fixtureId(63) }), compact: true }, global })
    expect(thread.get('[data-testid="blocked-message"]').classes()).toContain('px-2')
    await w.get('[data-testid="blocked-message-show"]').trigger('click')
    expect(w.text()).toContain('secret words')
    expect(w.find('[data-testid="blocked-message"]').exists()).toBe(false)
  })

  it('does not fold anything on a server without blocking', async () => {
    const chat = await open(pair, ['dm'])
    useBlocksStore().blocked = [{ user_id: ada.id, username: 'member2', display_name: 'Ada', blocked_at: '2026-01-01T00:00:00Z' }]
    chat.messages = [messageFixture({ id: fixtureId(62), channel_id: pair.id, user_id: ada.id, content: 'visible words' })]
    const w = mount(ChatArea, { global, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain('visible words')
  })
})

describe('the header of a conversation', () => {
  it('has no pins or notification menu, shows the conversation info, and searches the conversations', async () => {
    await open(group)
    const w = mount(ChatHeader, { global, attachTo: document.body })
    expect(w.find('[data-testid="chat-channel-name"]').text()).toBe('Ada, Bo')
    expect(w.find('[aria-haspopup="menu"]').exists()).toBe(false)
    expect(w.find('[data-testid="pinned-messages"]').exists()).toBe(false)
    await w.get('[data-testid="search-in-channel"]').trigger('click')
    expect(openSearch).toHaveBeenCalledWith('in:"Ada, Bo" ', 'dm')
    await w.get('[data-testid="dm-info-button"]').trigger('click')
    expect(document.querySelector('[data-testid="dm-info"]')).not.toBeNull()
    expect(searchScope.value).toBe('channels')
  })
})
