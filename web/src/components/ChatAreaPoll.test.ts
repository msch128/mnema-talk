import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ChatArea from './ChatArea.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale, i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'
import { userFixture, categoryFixture, channelFixture, messageFixture } from '../test-fixtures.fixture'
import type { Poll } from '../types/domain'

const CH = '00000000-0000-4000-8000-0000000000c1'
const POLL_ID = '00000000-0000-4000-8000-0000000000d1'
const BOT = '00000000-0000-0000-0000-000000000000'
const builtinPoll = {
  id: POLL_ID, bot_id: BOT, bot_name: '', builtin: true, created_at: '2026-01-01T00:00:00Z', description: 'Ask the channel a question', disabled: false, name: 'poll',
  options: [{ name: 'question', description: 'q', type: 3, required: true }]
}
const poll: Poll = {
  multiple: false, anonymous: false, closed: false, closes_at: '2099-01-01T00:00:00Z', total_voters: 0,
  options: [{ id: '00000000-0000-4000-8000-0000000000e1', label: 'Yes', votes: 0, voters: [] }, { id: '00000000-0000-4000-8000-0000000000e2', label: 'No', votes: 0, voters: [] }]
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
    if (u.endsWith('/commands')) return json([builtinPoll])
    if (u.endsWith('/polls')) return json(messageFixture({ id: '00000000-0000-4000-8000-0000000000f1', channel_id: CH, content: 'Lunch?', poll }), 201)
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

async function fillAndPost() {
  const dialog = document.querySelector('[data-testid="poll-dialog"]')
  expect(dialog).not.toBeNull()
  const set = async (selector: string, value: string, index = 0) => {
    const input = document.querySelectorAll<HTMLInputElement>(selector)[index]!
    input.value = value
    input.dispatchEvent(new Event('input'))
  }
  await set('#poll-question', 'Lunch?')
  await set('[data-testid="poll-answer"]', 'Yes', 0)
  await set('[data-testid="poll-answer"]', 'No', 1)
  await flushPromises()
  dialog!.dispatchEvent(new Event('submit', { cancelable: true }))
  await flushPromises()
}

describe('polls in the composer', () => {
  it('opens the poll dialog from /poll in the command menu and posts the poll into the channel', async () => {
    const ta = w.get('textarea')
    await ta.setValue('/po')
    await flushPromises()
    const menu = w.get('[data-testid="slash-suggestions"]')
    expect(menu.text()).toContain('/poll')
    expect(menu.text()).toContain('Built-in')

    await ta.trigger('keydown', { key: 'Enter' })
    expect(w.find('[data-testid="slash-bar"]').exists()).toBe(false)
    expect((ta.element as HTMLTextAreaElement).value).toBe('')
    await fillAndPost()

    const posted = requests.find(r => r.url.endsWith('/polls'))
    expect(posted).toMatchObject({ method: 'POST', body: { question: 'Lunch?', options: ['Yes', 'No'], duration: '24h' } })
    expect(document.querySelector('[data-testid="poll-dialog"]')).toBeNull()
    expect(w.find('[data-testid="poll"]').exists()).toBe(true)
  })

  it('has a poll button that opens the same dialog', async () => {
    await w.get('[data-testid="poll-button"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="poll-dialog"]')).not.toBeNull()
    expect(requests.some(r => r.url.endsWith('/interactions'))).toBe(false)
    await fillAndPost()
    expect(requests.some(r => r.url.endsWith('/polls'))).toBe(true)
  })

  it('offers no poll button to a member who cannot send', async () => {
    const readOnly = { owner: false, permissions: PERMISSION_BITS.view_channel, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [], channels: [{ channel_id: CH, permissions: PERMISSION_BITS.view_channel }] }
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(JSON.stringify(readOnly), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await usePermissionsStore().load()
    await flushPromises()
    expect(w.find('[data-testid="poll-button"]').exists()).toBe(false)
  })

  it('offers to edit an ordinary message of mine but not a poll', async () => {
    const chat = useChatStore()
    chat.messages = [
      messageFixture({ id: '00000000-0000-4000-8000-0000000000f2', channel_id: CH, user_id: 'me', content: 'plain' }),
      messageFixture({ id: '00000000-0000-4000-8000-0000000000f3', channel_id: CH, user_id: 'me', content: 'Lunch?', poll })
    ]
    await flushPromises()
    const rows = w.findAll('[role="article"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]!.find('svg.lucide-pencil').exists()).toBe(true)
    expect(rows[1]!.find('[data-testid="poll"]').exists()).toBe(true)
    expect(rows[1]!.find('svg.lucide-pencil').exists()).toBe(false)
  })
})
