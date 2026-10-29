import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ChatArea from './ChatArea.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale, i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { userFixture, categoryFixture, channelFixture } from '../test-fixtures.fixture'

const CH = '00000000-0000-4000-8000-0000000000c1'
const CMD = '00000000-0000-4000-8000-0000000000d1'
const BOT = '00000000-0000-4000-8000-0000000000b1'
const ping = {
  id: CMD, bot_id: BOT, bot_name: 'helper', created_at: '2026-01-01T00:00:00Z', description: 'Replies', disabled: false, name: 'ping',
  options: [{ name: 'text', description: 'What to say', type: 3, required: true }]
}

let w!: ReturnType<typeof mount<typeof ChatArea>>
let requests: { url: string; method: string; body: unknown }[]

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  requests = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    const u = String(url)
    requests.push({ url: u, method: init?.method || 'GET', body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
    const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
    if (u.endsWith('/commands')) return json([ping])
    if (u.endsWith('/interactions')) return json({ interaction_id: BOT }, 202)
    return new Response(null, { status: 204 })
  }))
  useAuthStore().user = userFixture({ id: 'me', username: 'me', role: 'user' })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'c', channels: [channelFixture({ id: CH, name: 'general', type: 'text' })] })]
  chat.activeChannel = channelFixture({ id: CH, name: 'general', type: 'text' })
  chat.messages = []
  w = mount(ChatArea, { attachTo: document.body, global: { plugins: [i18nPlugin], directives: { tooltip } } })
})

afterEach(() => {
  w.unmount()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('slash commands in the composer', () => {
  it('lists the channel commands for "/", takes the options in a bar and runs the command', async () => {
    const ta = w.get('textarea')
    await ta.setValue('/pi')
    await flushPromises()
    expect(requests.map(r => r.url)).toContain(`/api/channels/${CH}/commands`)
    expect(w.find('[data-testid="slash-suggestions"]').exists()).toBe(true)

    await ta.trigger('keydown', { key: 'Enter' })
    expect(w.find('[data-testid="slash-suggestions"]').exists()).toBe(false)
    expect(w.find('[data-testid="slash-bar"]').exists()).toBe(true)
    expect((ta.element as HTMLTextAreaElement).value).toBe('')

    await w.get('[data-option="text"]').setValue('hello')
    await w.get('[data-testid="slash-run"]').trigger('click')
    await flushPromises()
    const run = requests.find(r => r.url.endsWith('/interactions'))
    expect(run).toMatchObject({ method: 'POST', body: { command_id: CMD, options: { text: 'hello' } } })
    expect(w.find('[data-testid="slash-bar"]').exists()).toBe(false)
  })

  it('points the combobox at the slash list and its highlighted command', async () => {
    const ta = w.get('textarea')
    expect(ta.attributes('aria-controls')).toBeUndefined()
    await ta.setValue('/pi')
    await flushPromises()
    expect(ta.attributes('aria-controls')).toBe('chat-slash')
    expect(ta.attributes('aria-activedescendant')).toBe('chat-slash-0')
    expect(w.find('#chat-slash-0').exists()).toBe(true)
  })

  it('can be dropped without running it, and an ordinary slash message still sends', async () => {
    const ta = w.get('textarea')
    await ta.setValue('/ping')
    await flushPromises()
    await ta.trigger('keydown', { key: 'Tab' })
    await w.get('[data-testid="slash-cancel"]').trigger('click')
    expect(w.find('[data-testid="slash-bar"]').exists()).toBe(false)
    expect(requests.some(r => r.url.endsWith('/interactions'))).toBe(false)

    await ta.setValue('/not a command')
    await flushPromises()
    expect(w.find('[data-testid="slash-suggestions"]').exists()).toBe(false)
  })

  it('shows a private reply below the messages and lets it go', async () => {
    useChatStore().handleWSEvent({
      type: 'interaction_response',
      payload: { channel_id: CH, bot_id: BOT, bot_name: 'helper', content: 'only for you', embeds: null, created_at: '2026-01-01T00:00:00Z' }
    })
    await flushPromises()
    expect(w.text()).toContain('only for you')
    await w.get('[data-testid="ephemeral-notes"] button').trigger('click')
    expect(w.text()).not.toContain('only for you')
  })
})
