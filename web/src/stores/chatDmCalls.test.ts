import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { useBlocksStore } from './blocks'
import { useDmCallsStore } from './dmCalls'
import { useServerFeaturesStore } from './serverFeatures'
import { useVoiceStore } from './voice'
import { platform } from '../lib/platform'
import type { OsNotification } from '../lib/osNotify'
import { setLocale } from '../i18n'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import { fixtureId, messageFixture, userFixture } from '../test-fixtures.fixture'
import * as router from '../lib/router'

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: vi.fn(() => Promise.resolve([]))
}))

const join = vi.hoisted(() => vi.fn())
vi.mock('../composables/useDmCall', () => ({ useDmCall: () => ({ join }) }))

const me = fixtureId(1)
const ada = fixtureId(2)
const conversation = fixtureId(100)
const talk = fixtureId(200)
const invite = fixtureId(300)

const ring = { conversation_id: conversation, talk_id: talk, message_id: invite, caller_id: ada, talk_name: 'Talk 1', ring_seconds: 45 }

let shown: OsNotification[]

function setup(options: { presence?: 'online' | 'dnd'; level?: 'all' | 'mute'; away?: boolean; permission?: NotificationPermission | 'unsupported' } = {}) {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me, presence: options.presence ?? 'online' })
  useServerFeaturesStore().list = ['dm']
  chat.dms = [dmFixture({ id: conversation, members: [dmMemberFixture(1), dmMemberFixture(2, { display_name: 'Ada' })] })]
  chat.readStates = { [conversation]: { channel_id: conversation, unread_count: 0, mention_count: 0, last_read_at: null, notify_level: options.level ?? 'all' } }
  vi.spyOn(platform, 'notificationPermission').mockReturnValue(options.permission ?? 'granted')
  vi.spyOn(platform, 'notify').mockImplementation(n => { shown.push(n) })
  vi.spyOn(document, 'hasFocus').mockReturnValue(!options.away)
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => options.away === true })
  return chat
}

beforeEach(() => {
  shown = []
  vi.useFakeTimers()
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => {
  useDmCallsStore().reset()
  useChatStore().resetCommunityState()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('an incoming call', () => {
  it('shows the card and, for a member who looks elsewhere, a notice that says who calls and how to join', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    const calls = useDmCallsStore()
    expect(calls.incoming).toHaveLength(1)
    expect(calls.incoming[0]?.silent).toBe(false)
    expect(shown).toHaveLength(1)
    expect(shown[0]).toMatchObject({ title: 'Ada is calling you', body: 'Direct message · Click to join', channelId: conversation, messageId: invite, call: true })
  })

  it('leaves the operating system alone while the member looks at the app', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    expect(useDmCallsStore().incoming).toHaveLength(1)
    expect(shown).toEqual([])
  })

  it('opens the conversation at the invitation when the notice is used', () => {
    const chat = setup({ away: true })
    const navigate = vi.spyOn(router, 'navigate').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    shown[0]?.onClick()
    expect(navigate).toHaveBeenCalledWith(`/dm/member2/m/${invite}`)
  })

  it('joins the Talk as well when the toast\'s Join button was used', async () => {
    const chat = setup({ away: true })
    vi.spyOn(router, 'navigate').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    shown[0]?.onClick('join')
    await vi.waitFor(() => expect(join).toHaveBeenCalledWith(talk))
    join.mockClear()
    shown[0]?.onClick()
    await Promise.resolve()
    expect(join).not.toHaveBeenCalled()
  })

  it('shows the card without sound or notice in do not disturb', () => {
    const chat = setup({ presence: 'dnd', away: true })
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    expect(useDmCallsStore().incoming[0]?.silent).toBe(true)
    expect(shown).toEqual([])
  })

  it('shows the card without sound while the member is in a call already', () => {
    const chat = setup()
    useVoiceStore().isConnected = true
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    expect(useDmCallsStore().incoming[0]?.silent).toBe(true)
  })

  it('rings nothing for a conversation the member muted, or a caller they blocked', () => {
    const muted = setup({ level: 'mute', away: true })
    muted.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    expect(useDmCallsStore().incoming).toEqual([])
    expect(shown).toEqual([])
    expect(useDmCallsStore().markOf(conversation)).toBe('active')

    useDmCallsStore().reset()
    setup({ away: true })
    vi.spyOn(useBlocksStore(), 'isBlocked').mockImplementation(id => id === ada)
    useChatStore().handleWSEvent({ type: 'dm_call_ring', payload: ring })
    expect(useDmCallsStore().incoming).toEqual([])
    expect(useDmCallsStore().markOf(conversation)).toBeNull()
  })

  it('shows the calling state to the member who started it, on every device of theirs', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'dm_call_ring', payload: { ...ring, caller_id: me } })
    const calls = useDmCallsStore()
    expect(calls.incoming).toEqual([])
    expect(calls.outgoing).toMatchObject({ phase: 'calling', talkId: talk })
    expect(shown).toEqual([])
  })
})

describe('the end of a call', () => {
  it('removes the card where it was answered elsewhere and withdraws the notice', () => {
    const chat = setup({ away: true })
    const close = vi.spyOn(platform, 'closeNotification').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    chat.handleWSEvent({ type: 'dm_call_end', payload: { conversation_id: conversation, talk_id: talk, reason: 'answered', active: true, user_id: me } })
    expect(useDmCallsStore().incoming).toEqual([])
    expect(close).toHaveBeenCalledWith(conversation)
  })

  it('turns an unanswered ring into nothing but the missed-call message the server posts', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    vi.advanceTimersByTime(45_000)
    expect(useDmCallsStore().incoming).toEqual([])
  })
})

describe('the messages of a call', () => {
  const callMessage = (embed: object) => messageFixture({ id: invite, channel_id: conversation, user_id: ada, username: 'ada', display_name: 'Ada', content: '', embeds: [embed] as never })

  it('are not announced twice: the invitation card leaves the notice to the ring', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'message_create', payload: callMessage({ title: 'Talk 1', talk_id: talk }) })
    expect(shown).toEqual([])
    expect(chat.readStates[conversation]?.unread_count).toBe(1)
  })

  it('announce the card late when nothing rings for it (somebody is in the Talk already)', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'message_create', payload: callMessage({ title: 'Talk 1', talk_id: talk }) })
    vi.advanceTimersByTime(1499)
    expect(shown).toEqual([])
    vi.advanceTimersByTime(2)
    expect(shown).toHaveLength(1)
    expect(shown[0]).toMatchObject({ title: 'Ada is calling you', channelId: conversation, messageId: invite })
  })

  it('stay quiet when the ring for the card is tracked, whatever became of it', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'message_create', payload: callMessage({ title: 'Talk 1', talk_id: talk }) })
    chat.handleWSEvent({ type: 'dm_call_ring', payload: ring })
    shown.length = 0
    vi.advanceTimersByTime(5000)
    expect(shown).toEqual([])
  })

  it('make a missed call a notice of its own, and an unread message', () => {
    const chat = setup({ away: true })
    chat.handleWSEvent({ type: 'message_create', payload: callMessage({ title: 'Talk 1', call: 'missed' }) })
    expect(shown).toHaveLength(1)
    expect(shown[0]).toMatchObject({ title: 'Missed call from Ada', body: 'Direct message' })
    expect(shown[0]?.call).toBeUndefined()
    expect(chat.readStates[conversation]?.unread_count).toBe(1)
  })

  it('show in the conversation list as a call, not as an empty message', () => {
    const chat = setup()
    chat.handleWSEvent({ type: 'message_create', payload: callMessage({ call: 'missed' }) })
    expect(chat.dms[0]?.last_message).toMatchObject({ call: 'missed', user_id: ada })
  })
})

describe('the conversation list', () => {
  it('brings back a call that rings after a reload or a reconnect, without announcing it again', async () => {
    const chat = setup({ away: true })
    const { api } = await import('../lib/api')
    vi.mocked(api).mockResolvedValueOnce([dmFixture({
      id: conversation,
      members: [dmMemberFixture(1), dmMemberFixture(2, { display_name: 'Ada' })],
      call: { talk_id: talk, caller_id: ada, message_id: invite, talk_name: 'Talk 1', ringing: true, ring_seconds: 20 }
    })])
    await chat.fetchDms()
    expect(useDmCallsStore().incoming).toHaveLength(1)
    expect(shown).toEqual([])
  })
})
