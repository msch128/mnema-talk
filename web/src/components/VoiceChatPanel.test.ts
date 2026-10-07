import type { Message } from '../types/domain'
import { fixtureId, requireValue } from '../test-fixtures.fixture'
import { channelFixture, messageFixture, userFixture, categoryFixture, readStateFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useResizable, storageKey } from '../composables/useResizable'
import { VOICE_CHAT_HEIGHT } from '../lib/voiceChatPanel'
import VoiceChatPanel from './VoiceChatPanel.vue'

let w!: ReturnType<typeof mount<typeof VoiceChatPanel>>
let calls: {url: string; method: string; body: BodyInit | null | undefined}[]

const lounge = channelFixture({ id: fixtureId(10), name: 'Lounge', type: 'voice' })

function msg(id: string, content: string, extra: Partial<Message> = {}) {
  return messageFixture({
    id, channel_id: fixtureId(10), user_id: fixtureId(12), username: 'alice', display_name: 'Alice',
    content, created_at: '2026-01-01T10:00:00Z', reactions: [], attachments: [], ...extra
  })
}

function seed({ messages = [], active = lounge }: { messages?: Message[]; active?: typeof lounge } = {}) {
  useAuthStore().user = userFixture({ id: fixtureId(11), username: fixtureId(11), display_name: 'Me', role: 'user' })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'cat', name: 'Talks', channels: [lounge, channelFixture({ id: 'v2', name: 'Other', type: 'voice' })] })]
  chat.activeChannel = active
  chat.messages = messages
  return chat
}

function mountPanel(props = {}) {
  w = mount(VoiceChatPanel, { props: { channelId: fixtureId(10), ...props }, attachTo: document.body })
  return w
}

beforeEach(() => {
  localStorage.clear()
  setLocale('de')
  setActivePinia(createPinia())
  calls = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    calls.push({ url: String(url), method: init?.method || 'GET', body: init?.body })
    if (init?.method === 'POST' && /\/messages$/.test(String(url))) {
      const sent = JSON.parse(typeof init.body === 'string' ? init.body : '{}')
      return new Response(JSON.stringify(msg(fixtureId(13), sent.content, { user_id: fixtureId(11), username: fixtureId(11), display_name: 'Me', created_at: new Date().toISOString() })), {
        status: 201, headers: { 'Content-Type': 'application/json' }
      })
    }
    if (/\/messages(\?|$)/.test(String(url))) {
      return new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    return new Response(null, { status: 204 })
  }))
})

afterEach(() => {
  w?.unmount()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VoiceChatPanel', () => {
  it('shows the empty state like Discord: welcome and beginning of the channel', async () => {
    seed()
    mountPanel()
    await nextTick()
    const empty = w.get<HTMLElement>('[data-testid="voice-chat-empty"]')
    expect(empty.text()).toContain('Willkommen in Lounge!')
    expect(empty.text()).toContain('Das ist der Anfang des Kanals Lounge.')
    expect(w.get<HTMLElement>('[data-testid="chat-channel-name"]').text()).toBe('Lounge')
    expect(w.get<HTMLTextAreaElement>('textarea').attributes('placeholder')).toBe('Nachricht an Lounge')
  })

  it('has the same English texts', async () => {
    setLocale('en')
    seed()
    mountPanel()
    await nextTick()
    const empty = w.get<HTMLElement>('[data-testid="voice-chat-empty"]')
    expect(empty.text()).toContain('Welcome to Lounge!')
    expect(empty.text()).toContain('This is the start of the Lounge channel.')
    expect(w.get<HTMLTextAreaElement>('textarea').attributes('placeholder')).toBe('Message Lounge')
  })

  it('the X in its header closes it', async () => {
    seed()
    mountPanel()
    await w.get<HTMLElement>('[data-testid="voice-chat-close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    // The member list toggle stays in the Talk header, not in the chat.
    expect(w.find<HTMLElement>('[aria-pressed]').exists()).toBe(false)
  })

  it('sends a message to the voice channel', async () => {
    const chat = seed()
    mountPanel()
    const box = w.get<HTMLTextAreaElement>('textarea')
    await box.setValue('hallo Talk')
    await box.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const post = calls.find(c => c.method === 'POST' && c.url === `/api/channels/${fixtureId(10)}/messages`)
    expect(post).toBeTruthy()
    expect(JSON.parse(String(requireValue(post).body))).toEqual({ content: 'hallo Talk' })
    expect(box.element.value).toBe('')
    expect(chat.messages.map(m => m.content)).toEqual(['hallo Talk'])
    await nextTick()
    expect(w.text()).toContain('hallo Talk')
    expect(w.find<HTMLElement>('[data-testid="voice-chat-empty"]').exists()).toBe(false)
  })

  it('renders messages with the shared rows: author button, inline videos, reactions', async () => {
    const chat = seed({
      messages: [msg('m1', 'schau mal', {
        reactions: [{ emoji: '👍', count: 2, users: [fixtureId(12), 'b'] }],
        attachments: [{ id: 'att', url: '/m/v', mime_type: 'video/mp4', is_deleted: false, original_filename: 'clip.mp4', size_bytes: 10 }]
      })]
    })
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(async () => {})
    mountPanel()
    await nextTick()
    const name = w.get<HTMLElement>('[data-testid="author-name"]')
    expect(name.element.tagName).toBe('BUTTON')
    await name.trigger('click')
    expect(open).toHaveBeenCalled()
    expect(w.find<HTMLVideoElement>('[data-attachment="video"] video').exists()).toBe(true)
    expect(w.find<HTMLElement>('[data-msg-id="m1"]').text()).toContain('👍')
  })

  it('selects the Talk\'s channel when another one is active', async () => {
    const chat = seed({ active: channelFixture({ id: 'x', name: 'other', type: 'text' }) })
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async ch => { chat.activeChannel = ch })
    mountPanel()
    expect(select).toHaveBeenCalledWith(expect.objectContaining({ id: fixtureId(10) }))
    await nextTick()
    expect(w.find<HTMLTextAreaElement>('textarea').exists()).toBe(true)
  })

  it('counts as reading while open: unreads are marked read, not collected', async () => {
    const chat = seed()
    chat.readStates = { [fixtureId(10)]: readStateFixture({ channel_id: fixtureId(10), unread_count: 2, mention_count: 1 }) }
    mountPanel()
    expect(chat.voiceChatReading).toBe(true)
    await flushPromises()
    await new Promise(r => setTimeout(r, 0))
    expect(calls.some(c => c.method === 'POST' && c.url === `/api/channels/${fixtureId(10)}/read`)).toBe(true)

    w.unmount()
      expect(chat.voiceChatReading).toBe(false)
  })

  it('closes a thread of its channel when it closes', async () => {
    const chat = seed()
    mountPanel()
    chat.activeThread = { id: 'm1', channel_id: fixtureId(10) }
    w.unmount()
      expect(chat.activeThread).toBe(null)
  })

  it('its height is dragged and keyed on a horizontal handle and remembered', async () => {
    seed()
    window.innerHeight = 1000
    const height = useResizable([VOICE_CHAT_HEIGHT], { axis: 'y', centerMin: 288, storage: localStorage }).voiceChat
    mountPanel({ panel: height })
    const root = w.get<HTMLElement>('[data-testid="voice-chat-panel"]')
    expect(root.attributes('style')).toContain('height: 300px')

    const handle = w.get<HTMLElement>('[role="separator"]')
    expect(handle.attributes('aria-orientation')).toBe('horizontal')
    expect(handle.attributes('aria-label')).toBe('Höhe des Chats anpassen')
    await handle.trigger('keydown', { key: 'ArrowUp' })
    expect(root.attributes('style')).toContain('height: 308px')
    expect(localStorage.getItem(storageKey('voiceChat', 'height'))).toBe('308')
    await handle.trigger('keydown', { key: 'ArrowDown', shiftKey: true })
    expect(root.attributes('style')).toContain('height: 276px')
    await handle.trigger('keydown', { key: 'End' })
    // The stage keeps 288px of the 1000px window.
    expect(root.attributes('style')).toContain('height: 712px')
    await handle.trigger('keydown', { key: 'Home' })
    expect(root.attributes('style')).toContain('height: 160px')
  })
})

describe('voice chat reading in the store', () => {
  it('a search hit or notification in a voice channel opens its Talk\'s chat', () => {
    const chat = seed()
    chat.goToMessage(msg('r1', 'reply', { parent_id: 'root1' }))
    expect(window.location.pathname).toBe(`/v/${fixtureId(10)}/chat/m/root1`)
    chat.goToMessage(messageFixture({ id: 't1', channel_id: 'textch' }))
    expect(window.location.pathname).toBe('/c/textch/m/t1')
  })

  it('a message in the Talk\'s channel is unread while the chat is closed', () => {
    const chat = seed()
    useVoiceStore().activeView = 'voice'
    chat.handleWSEvent({ type: 'message_create', payload: msg('m1', 'hi') })
    expect(requireValue(chat.readStates[fixtureId(10)]).unread_count).toBe(1)

    // Opening the chat reads it; what arrives while open is read right away.
    chat.setVoiceChatReading(true)
    expect(requireValue(chat.readStates[fixtureId(10)]).unread_count).toBe(0)
    chat.handleWSEvent({ type: 'message_create', payload: msg('m2', 'noch da?') })
    expect(requireValue(chat.readStates[fixtureId(10)]).unread_count).toBe(0)

    chat.setVoiceChatReading(false)
    chat.handleWSEvent({ type: 'message_create', payload: msg('m3', 'hallo?') })
    expect(requireValue(chat.readStates[fixtureId(10)]).unread_count).toBe(1)
  })
})

describe('VoiceChatPanel unavailable room', () => {
  it('shows loading for unknown channel, accepts empty channel, and keeps an unrelated thread on close', async () => {
    const chat = seed(); mountPanel({ channelId: 'missing' })
    expect(w.find('[role="status"]').exists()).toBe(true); expect(w.find('textarea').exists()).toBe(false)
    await w.setProps({ channelId: '' }); expect(w.find('[role="status"]').exists()).toBe(true)
    chat.activeThread = { id: 'unrelated-thread', channel_id: 'other-channel' }; w.unmount()
    expect(chat.activeThread?.id).toBe('unrelated-thread')
  })
})
