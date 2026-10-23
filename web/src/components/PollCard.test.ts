import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import PollCard from './PollCard.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale, i18nPlugin } from '../i18n'
import type { Message, Poll } from '../types/domain'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'

const confirmMock = vi.fn<() => Promise<boolean>>(async () => true)
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

const ME = fixtureId(1)
const OTHER = fixtureId(5)
const CH = fixtureId(2)
const [A, B, C] = [fixtureId(11), fixtureId(12), fixtureId(13)]
const NOW = '2026-10-22T12:00:00Z'

function poll(overrides: Partial<Poll> = {}): Poll {
  return {
    multiple: false, anonymous: false, closed: false, closes_at: '2026-10-22T14:00:00Z', total_voters: 2,
    options: [
      { id: A, label: 'Pizza', votes: 1, voters: [OTHER] },
      { id: B, label: 'Pasta', votes: 1, voters: [ME] },
      { id: C, label: 'Salad', votes: 0 }
    ],
    ...overrides
  }
}

let requests: { url: string; method: string; body: unknown }[]
let replies: Record<string, unknown>

function stubServer() {
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    const u = String(url)
    requests.push({ url: u, method: init?.method || 'GET', body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
    const key = Object.keys(replies).find(k => u.endsWith(k))
    if (!key) return new Response(null, { status: 404 })
    const reply = replies[key]
    return reply instanceof Response ? reply : new Response(JSON.stringify(reply), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
}

function mountCard(message: Partial<Message> = {}) {
  const chat = useChatStore()
  const msg = messageFixture({ id: fixtureId(3), channel_id: CH, user_id: OTHER, content: 'Lunch?', poll: poll(), ...message })
  chat.messages = [msg]
  chat.members = [userFixture({ id: ME, username: 'me', display_name: 'Me' }), userFixture({ id: OTHER, username: 'olivia', display_name: 'Olivia' })] as never
  const w = mount(PollCard, { props: { msg: chat.messages[0] as Message }, global: { plugins: [i18nPlugin] } })
  return { w, chat }
}

const option = (w: ReturnType<typeof mountCard>['w'], id: string) => w.get(`[data-option="${id}"]`)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  requests = []
  replies = {}
  confirmMock.mockClear()
  useAuthStore().user = userFixture({ id: ME, username: 'me', role: 'user' })
  vi.useFakeTimers()
  vi.setSystemTime(new Date(NOW))
  stubServer()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('PollCard', () => {
  it('shows each answer with its votes and share, who voted, and what is left', () => {
    const { w } = mountCard()
    expect(w.findAll('li')).toHaveLength(3)
    expect(option(w, A).text()).toContain('Pizza')
    expect(option(w, A).text()).toContain('1 · 50%')
    expect(option(w, C).text()).toContain('0 · 0%')
    expect(w.get('[data-testid="poll-total"]').text()).toBe('2 voters')
    expect(w.get('[data-testid="poll-left"]').text()).toBe('Ends in 2 hours')
    expect(w.findAll('[data-testid="poll-voters"]').map(v => v.text())).toEqual(['Olivia', 'Me'])
    expect(w.text()).toContain('Choose one')
    expect(w.find('[data-testid="poll-anonymous"]').exists()).toBe(false)
  })

  it('marks my own answer, taken from the voters of an open poll', () => {
    const { w } = mountCard()
    expect(option(w, B).attributes('aria-pressed')).toBe('true')
    expect(option(w, A).attributes('aria-pressed')).toBe('false')
  })

  it('votes for another answer in a single-choice poll and shows the new tallies at once', async () => {
    const answer = poll({ total_voters: 2, options: [
      { id: A, label: 'Pizza', votes: 2, voters: [OTHER, ME] }, { id: B, label: 'Pasta', votes: 0, voters: [] }, { id: C, label: 'Salad', votes: 0 }] })
    replies['/poll/votes'] = { ...answer, my_votes: [A] }
    const { w } = mountCard()
    await option(w, A).trigger('click')
    await flushPromises()
    expect(requests).toEqual([{ url: `/api/channels/${CH}/messages/${fixtureId(3)}/poll/votes`, method: 'PUT', body: { option_ids: [A] } }])
    expect(option(w, A).attributes('aria-pressed')).toBe('true')
    expect(option(w, B).attributes('aria-pressed')).toBe('false')
    expect(option(w, A).text()).toContain('2 · 100%')
  })

  it('takes my vote back with a click on my answer', async () => {
    replies['/poll/votes'] = { ...poll({ total_voters: 1, options: [{ id: A, label: 'Pizza', votes: 1, voters: [OTHER] }, { id: B, label: 'Pasta', votes: 0, voters: [] }] }), my_votes: [] }
    const { w } = mountCard()
    await option(w, B).trigger('click')
    await flushPromises()
    expect(requests[0]?.body).toEqual({ option_ids: [] })
    expect(option(w, B).attributes('aria-pressed')).toBe('false')
  })

  it('toggles answers in a multiple-choice poll', async () => {
    replies['/poll/votes'] = { ...poll({ multiple: true }), my_votes: [A, B] }
    const { w } = mountCard({ poll: poll({ multiple: true }) })
    expect(w.text()).toContain('Choose one or more')
    await option(w, A).trigger('click')
    await flushPromises()
    expect(requests[0]?.body).toEqual({ option_ids: [B, A] })
  })

  it('tells why a vote failed and stays usable', async () => {
    replies['/poll/votes'] = new Response(JSON.stringify({ error: { code: 'CONFLICT', message: 'the poll has ended' } }), { status: 409, headers: { 'Content-Type': 'application/json' } })
    const { w } = mountCard()
    await option(w, A).trigger('click')
    await flushPromises()
    expect(useToastStore().toasts).toHaveLength(1)
    expect(useToastStore().toasts[0]?.text).toBe('This poll has ended, so votes are closed.')
    expect(option(w, A).attributes('disabled')).toBeUndefined()
  })

  it('says why the answers are disabled when the member may not react in this channel', () => {
    usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 1, role_ids: [], top_position: 0, catalog: [], channels: [{ channel_id: CH, permissions: PERMISSION_BITS.view_channel }] }
    const { w } = mountCard()
    expect(option(w, A).attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="poll-cannot-vote"]').text()).toBe('You can’t vote in this channel.')
  })

  it('shows no such hint to a member who can vote', () => {
    const { w } = mountCard()
    expect(w.find('[data-testid="poll-cannot-vote"]').exists()).toBe(false)
  })

  it('asks the server for my own choice in an anonymous poll, and never lists voters', async () => {
    const anon = poll({ anonymous: true, options: [{ id: A, label: 'Pizza', votes: 1 }, { id: B, label: 'Pasta', votes: 1 }, { id: C, label: 'Salad', votes: 0 }] })
    replies['/poll'] = { ...anon, my_votes: [A] }
    const { w } = mountCard({ poll: anon })
    await flushPromises()
    expect(requests.map(r => `${r.method} ${r.url}`)).toEqual([`GET /api/channels/${CH}/messages/${fixtureId(3)}/poll`])
    expect(option(w, A).attributes('aria-pressed')).toBe('true')
    expect(option(w, B).attributes('aria-pressed')).toBe('false')
    expect(w.find('[data-testid="poll-anonymous"]').exists()).toBe(true)
    expect(w.find('[data-testid="poll-voters"]').exists()).toBe(false)
  })

  it('does not ask for votes in a poll that names its voters', () => {
    mountCard()
    expect(requests).toEqual([])
  })

  it('shows the result and refuses votes once the poll has ended', async () => {
    const { w } = mountCard({ poll: poll({ closed: true }) })
    expect(w.get('[data-testid="poll-ended"]').text()).toBe('Ended')
    expect(w.find('[data-testid="poll-left"]').exists()).toBe(false)
    expect(option(w, A).attributes('disabled')).toBeDefined()
    expect(w.find('[data-testid="poll-end"]').exists()).toBe(false)
    await option(w, A).trigger('click')
    expect(requests).toEqual([])
  })

  it('turns to ended by itself when the end time comes', async () => {
    const { w } = mountCard()
    expect(w.find('[data-testid="poll-ended"]').exists()).toBe(false)
    vi.setSystemTime(new Date('2026-10-22T14:00:01Z'))
    await vi.advanceTimersByTimeAsync(15_000)
    expect(w.find('[data-testid="poll-ended"]').exists()).toBe(true)
    expect(option(w, A).attributes('disabled')).toBeDefined()
  })

  it('lets the author end the poll after confirming, and nobody else', async () => {
    const closed = { ...poll({ closed: true }), my_votes: [B] }
    replies['/poll/end'] = closed
    const { w: others } = mountCard()
    expect(others.find('[data-testid="poll-end"]').exists()).toBe(false)

    const { w } = mountCard({ user_id: ME })
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="poll-end"]').trigger('click')
    await flushPromises()
    expect(requests).toEqual([])

    await w.get('[data-testid="poll-end"]').trigger('click')
    await flushPromises()
    expect(requests).toEqual([{ url: `/api/channels/${CH}/messages/${fixtureId(3)}/poll/end`, method: 'POST', body: undefined }])
    expect(w.find('[data-testid="poll-ended"]').exists()).toBe(true)
  })

  it('lets the Owner end any poll', () => {
    useAuthStore().user = userFixture({ id: ME, username: 'me', role: 'admin' })
    const { w } = mountCard()
    expect(w.find('[data-testid="poll-end"]').exists()).toBe(true)
  })

  it('follows poll_update events and shortens a long voter list', async () => {
    const { w, chat } = mountCard()
    const many = Array.from({ length: 5 }, (_, i) => fixtureId(40 + i))
    chat.handleWSEvent({ type: 'poll_update', payload: {
      message_id: fixtureId(3), channel_id: CH,
      poll: poll({ total_voters: 5, options: [{ id: A, label: 'Pizza', votes: 5, voters: many }, { id: B, label: 'Pasta', votes: 0 }, { id: C, label: 'Salad', votes: 0 }] })
    } })
    await flushPromises()
    expect(option(w, A).text()).toContain('5 · 100%')
    expect(w.get('[data-testid="poll-voters"]').text()).toBe('Someone, Someone, Someone +2 more')
    expect(w.get('[data-testid="poll-total"]').text()).toBe('5 voters')
  })

  it('says German things in German', () => {
    setLocale('de')
    const { w } = mountCard()
    expect(w.text()).toContain('Umfrage')
    expect(w.get('[data-testid="poll-left"]').text()).toBe('Endet in 2 Stunden')
  })
})
