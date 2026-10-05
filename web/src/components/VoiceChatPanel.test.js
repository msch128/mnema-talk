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

let w
let calls

const lounge = { id: 'v1', name: 'Lounge', type: 'voice' }

function msg(id, content, extra = {}) {
  return {
    id, channel_id: 'v1', user_id: 'a', username: 'alice', display_name: 'Alice',
    content, created_at: '2026-01-01T10:00:00Z', reactions: [], attachments: [], ...extra
  }
}

function seed({ messages = [], active = lounge } = {}) {
  useAuthStore().user = { id: 'me', username: 'me', display_name: 'Me', role: 'user' }
  const chat = useChatStore()
  chat.categories = [{ id: 'cat', name: 'Talks', channels: [lounge, { id: 'v2', name: 'Other', type: 'voice' }] }]
  chat.activeChannel = active
  chat.messages = messages
  return chat
}

function mountPanel(props = {}) {
  w = mount(VoiceChatPanel, { props: { channelId: 'v1', ...props }, attachTo: document.body })
  return w
}

beforeEach(() => {
  localStorage.clear()
  setLocale('de')
  setActivePinia(createPinia())
  calls = []
  vi.stubGlobal('fetch', vi.fn(async (url, init) => {
    calls.push({ url, method: init?.method || 'GET', body: init?.body })
    if (init?.method === 'POST' && /\/messages$/.test(url)) {
      const sent = JSON.parse(init.body)
      return new Response(JSON.stringify(msg('new', sent.content, { user_id: 'me', username: 'me', display_name: 'Me', created_at: new Date().toISOString() })), {
        status: 201, headers: { 'Content-Type': 'application/json' }
      })
    }
    if (/\/messages(\?|$)/.test(url)) {
      return new Response('[]', { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    return new Response(null, { status: 204 })
  }))
})

afterEach(() => {
  w?.unmount()
  w = null
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VoiceChatPanel', () => {
  it('shows the empty state like Discord: welcome and beginning of the channel', async () => {
    seed()
    mountPanel()
    await nextTick()
    const empty = w.get('[data-testid="voice-chat-empty"]')
    expect(empty.text()).toContain('Willkommen in Lounge!')
    expect(empty.text()).toContain('Das ist der Anfang des Kanals Lounge.')
    expect(w.get('[data-testid="chat-channel-name"]').text()).toBe('Lounge')
    expect(w.get('textarea').attributes('placeholder')).toBe('Nachricht an Lounge')
  })

  it('has the same English texts', async () => {
    setLocale('en')
    seed()
    mountPanel()
    await nextTick()
    const empty = w.get('[data-testid="voice-chat-empty"]')
    expect(empty.text()).toContain('Welcome to Lounge!')
    expect(empty.text()).toContain('This is the start of the Lounge channel.')
    expect(w.get('textarea').attributes('placeholder')).toBe('Message Lounge')
  })

  it('the X in its header closes it', async () => {
    seed()
    mountPanel()
    await w.get('[data-testid="voice-chat-close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    // The member list toggle stays in the Talk header, not in the chat.
    expect(w.find('[aria-pressed]').exists()).toBe(false)
  })

  it('sends a message to the voice channel', async () => {
    const chat = seed()
    mountPanel()
    const box = w.get('textarea')
    await box.setValue('hallo Talk')
    await box.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const post = calls.find(c => c.method === 'POST' && c.url === '/api/channels/v1/messages')
    expect(post).toBeTruthy()
    expect(JSON.parse(post.body)).toEqual({ content: 'hallo Talk' })
    expect(box.element.value).toBe('')
    expect(chat.messages.map(m => m.content)).toEqual(['hallo Talk'])
    await nextTick()
    expect(w.text()).toContain('hallo Talk')
    expect(w.find('[data-testid="voice-chat-empty"]').exists()).toBe(false)
  })

  it('renders messages with the shared rows: author button, inline videos, reactions', async () => {
    const chat = seed({
      messages: [msg('m1', 'schau mal', {
        reactions: [{ emoji: '👍', count: 2, me: false }],
        attachments: [{ id: 'att', url: '/m/v', mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 10 }]
      })]
    })
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(() => {})
    mountPanel()
    await nextTick()
    const name = w.get('[data-testid="author-name"]')
    expect(name.element.tagName).toBe('BUTTON')
    await name.trigger('click')
    expect(open).toHaveBeenCalled()
    expect(w.find('[data-attachment="video"] video').exists()).toBe(true)
    expect(w.find('[data-msg-id="m1"]').text()).toContain('👍')
  })

  it('selects the Talk\'s channel when another one is active', async () => {
    const chat = seed({ active: { id: 'x', name: 'other', type: 'text' } })
    const select = vi.spyOn(chat, 'selectChannel').mockImplementation(async ch => { chat.activeChannel = ch })
    mountPanel()
    expect(select).toHaveBeenCalledWith(expect.objectContaining({ id: 'v1' }))
    await nextTick()
    expect(w.find('textarea').exists()).toBe(true)
  })

  it('counts as reading while open: unreads are marked read, not collected', async () => {
    const chat = seed()
    chat.readStates = { v1: { channel_id: 'v1', unread_count: 2, mention_count: 1 } }
    mountPanel()
    expect(chat.voiceChatReading).toBe(true)
    await flushPromises()
    await new Promise(r => setTimeout(r, 0))
    expect(calls.some(c => c.method === 'POST' && c.url === '/api/channels/v1/read')).toBe(true)

    w.unmount()
    w = null
    expect(chat.voiceChatReading).toBe(false)
  })

  it('closes a thread of its channel when it closes', async () => {
    const chat = seed()
    mountPanel()
    chat.activeThread = { id: 'm1', channel_id: 'v1' }
    w.unmount()
    w = null
    expect(chat.activeThread).toBe(null)
  })

  it('its height is dragged and keyed on a horizontal handle and remembered', async () => {
    seed()
    window.innerHeight = 1000
    const height = useResizable([VOICE_CHAT_HEIGHT], { axis: 'y', centerMin: 288, storage: localStorage }).voiceChat
    mountPanel({ panel: height })
    const root = w.get('[data-testid="voice-chat-panel"]')
    expect(root.attributes('style')).toContain('height: 300px')

    const handle = w.get('[role="separator"]')
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
    expect(window.location.pathname).toBe('/v/v1/chat/m/root1')
    chat.goToMessage({ id: 't1', channel_id: 'textch' })
    expect(window.location.pathname).toBe('/c/textch/m/t1')
  })

  it('a message in the Talk\'s channel is unread while the chat is closed', () => {
    const chat = seed()
    useVoiceStore().activeView = 'voice'
    chat.handleWSEvent({ type: 'message_create', payload: msg('m1', 'hi') })
    expect(chat.readStates.v1.unread_count).toBe(1)

    // Opening the chat reads it; what arrives while open is read right away.
    chat.setVoiceChatReading(true)
    expect(chat.readStates.v1.unread_count).toBe(0)
    chat.handleWSEvent({ type: 'message_create', payload: msg('m2', 'noch da?') })
    expect(chat.readStates.v1.unread_count).toBe(0)

    chat.setVoiceChatReading(false)
    chat.handleWSEvent({ type: 'message_create', payload: msg('m3', 'hallo?') })
    expect(chat.readStates.v1.unread_count).toBe(1)
  })
})
