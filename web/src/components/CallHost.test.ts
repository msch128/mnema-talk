import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import CallHost from './CallHost.vue'
import CallMissedNote from './CallMissedNote.vue'
import CallMark from './CallMark.vue'
import DmListRow from './DmListRow.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useDmCallsStore } from '../stores/dmCalls'
import { usePermissionsStore } from '../stores/permissions'
import { useVoiceStore } from '../stores/voice'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'
import { dmFixture, dmMemberFixture } from '../dm.fixture'
import { channelFixture, fixtureId, userFixture } from '../test-fixtures.fixture'
import type { ApiOptions } from '../lib/api'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const ringtone = vi.fn()
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: (...args: unknown[]) => ringtone(...args) }))
const joinVoiceChannel = vi.fn()
const leaveVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel, leaveVoiceChannel }) }))
const navigate = vi.fn()
vi.mock('../lib/router', async importOriginal => ({ ...(await importOriginal<typeof import('../lib/router')>()), navigate: (...a: unknown[]) => navigate(...a) }))

const me = fixtureId(1)
const ada = fixtureId(2)
const conversation = fixtureId(100)
const talk = channelFixture({ id: fixtureId(200), type: 'voice', name: 'Talk 1' })
const call = { conversationId: conversation, talkId: talk.id, messageId: fixtureId(300), callerId: ada, talkName: 'Talk 1' }

function world() {
  const chat = useChatStore()
  useAuthStore().user = userFixture({ id: me })
  chat.dms = [dmFixture({ id: conversation, members: [dmMemberFixture(1), dmMemberFixture(2, { display_name: 'Ada' })] })]
  chat.uncategorized = [talk]
  chat.fetchChannels = vi.fn(async () => {})
  chat.openDm = vi.fn(async () => {})
  const permissions = usePermissionsStore()
  permissions.info = {
    owner: false, permissions: PERMISSION_BITS.view_channel, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
    channels: [{ channel_id: talk.id, permissions: PERMISSION_BITS.view_channel | PERMISSION_BITS.connect }]
  }
  permissions.load = vi.fn(async () => {})
  return chat
}

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  ringtone.mockReset()
  joinVoiceChannel.mockReset()
  leaveVoiceChannel.mockReset()
  navigate.mockReset()
})
afterEach(() => { useDmCallsStore().reset(); vi.useRealTimers() })

describe('the incoming call card', () => {
  it('names who calls and offers Join, the conversation and Dismiss', () => {
    world()
    useDmCallsStore().ringIncoming(call, 45, false)
    const w = mount(CallHost)
    const card = w.get('[data-testid="incoming-call"]')
    expect(card.text()).toContain('Ada')
    expect(card.text()).toContain('is calling you')
    expect(card.text()).toContain('Direct message')
    expect(card.attributes('role')).toBe('alert')
    expect(w.find('[data-testid="incoming-call-join"]').exists()).toBe(true)
    expect(w.find('[data-testid="incoming-call-message"]').exists()).toBe(true)
    expect(w.find('[data-testid="incoming-call-dismiss"]').exists()).toBe(true)
    w.unmount()
  })

  it('shows nothing when nothing rings', () => {
    world()
    const w = mount(CallHost)
    expect(w.find('[data-testid="call-host"]').exists()).toBe(false)
    w.unmount()
  })

  it('joins the Talk like any other, and the card is gone', async () => {
    world()
    const calls = useDmCallsStore()
    calls.ringIncoming(call, 45, false)
    const w = mount(CallHost)
    await w.get('[data-testid="incoming-call-join"]').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
    expect(navigate).toHaveBeenCalledWith(`/v/${talk.id}`)
    expect(calls.incoming).toEqual([])
    await flushPromises()
    expect(w.find('[data-testid="incoming-call"]').exists()).toBe(false)
    w.unmount()
  })

  it('waits for a Talk it has not heard of yet before joining', async () => {
    const chat = world()
    chat.uncategorized = []
    chat.fetchChannels = vi.fn(async () => { chat.uncategorized = [talk] })
    useDmCallsStore().ringIncoming(call, 45, false)
    const w = mount(CallHost)
    await w.get('[data-testid="incoming-call-join"]').trigger('click')
    await flushPromises()
    expect(chat.fetchChannels).toHaveBeenCalled()
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
    w.unmount()
  })

  it('closes with Dismiss and joins nothing', async () => {
    world()
    const calls = useDmCallsStore()
    calls.ringIncoming(call, 45, false)
    const w = mount(CallHost)
    await w.get('[data-testid="incoming-call-dismiss"]').trigger('click')
    expect(calls.incoming).toEqual([])
    expect(joinVoiceChannel).not.toHaveBeenCalled()
    w.unmount()
  })

  it('opens the conversation from the message button', async () => {
    const chat = world()
    useDmCallsStore().ringIncoming(call, 45, false)
    const w = mount(CallHost)
    await w.get('[data-testid="incoming-call-message"]').trigger('click')
    expect(chat.openDm).toHaveBeenCalledWith(conversation)
    w.unmount()
  })

  it('stacks at most three cards, the newest on top', () => {
    world()
    const calls = useDmCallsStore()
    for (let i = 0; i < 4; i++) calls.ringIncoming({ ...call, conversationId: fixtureId(110 + i), talkId: fixtureId(210 + i) }, 45, false)
    const w = mount(CallHost)
    expect(w.findAll('[data-testid="incoming-call"]')).toHaveLength(3)
    w.unmount()
  })
})

describe('the ringtone', () => {
  it('rings now and every few seconds while a call rings, and stops with it', async () => {
    world()
    const calls = useDmCallsStore()
    calls.ringIncoming(call, 45, false)
    const w = mount(CallHost)
    expect(ringtone).toHaveBeenCalledTimes(1)
    expect(ringtone).toHaveBeenCalledWith('call_ring')
    vi.advanceTimersByTime(3000)
    expect(ringtone).toHaveBeenCalledTimes(2)
    calls.dismiss(conversation)
    await w.vm.$nextTick()
    vi.advanceTimersByTime(10_000)
    expect(ringtone).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('stays quiet for a silent call, and starts when a loud one arrives', async () => {
    world()
    const calls = useDmCallsStore()
    calls.ringIncoming(call, 45, true)
    const w = mount(CallHost)
    vi.advanceTimersByTime(10_000)
    expect(ringtone).not.toHaveBeenCalled()
    calls.ringIncoming({ ...call, conversationId: fixtureId(111), talkId: fixtureId(211) }, 45, false)
    await w.vm.$nextTick()
    expect(ringtone).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('stops when the host goes away', () => {
    world()
    useDmCallsStore().ringIncoming(call, 45, false)
    const w = mount(CallHost)
    w.unmount()
    vi.advanceTimersByTime(10_000)
    expect(ringtone).toHaveBeenCalledTimes(1)
  })
})

describe('the caller\'s card', () => {
  it('says who is being called and cancels by leaving the Talk', async () => {
    world()
    const calls = useDmCallsStore()
    calls.ringOutgoing({ ...call, callerId: me }, 45)
    useVoiceStore().currentChannelId = talk.id
    const w = mount(CallHost)
    const card = w.get('[data-testid="outgoing-call"]')
    expect(card.text()).toContain('Calling Ada…')
    await w.get('[data-testid="outgoing-call-cancel"]').trigger('click')
    expect(leaveVoiceChannel).toHaveBeenCalled()
    expect(calls.outgoing).toBeNull()
    w.unmount()
  })

  it('says that nobody has joined when the ring ran out, with a way to write instead', async () => {
    const chat = world()
    const calls = useDmCallsStore()
    calls.ringOutgoing({ ...call, callerId: me }, 45)
    useVoiceStore().currentChannelId = talk.id
    const w = mount(CallHost)
    vi.advanceTimersByTime(45_000)
    await w.vm.$nextTick()
    const card = w.get('[data-testid="outgoing-call"]')
    expect(card.attributes('data-phase')).toBe('nobody')
    expect(card.text()).toContain('Ada hasn’t joined')
    expect(card.text()).toContain('Hang up')
    await w.get('[data-testid="outgoing-call-message"]').trigger('click')
    expect(chat.openDm).toHaveBeenCalledWith(conversation)
    w.unmount()
  })

  it('words a group call without a name', async () => {
    const chat = world()
    chat.dms = [dmFixture({ id: conversation, group: true, members: [dmMemberFixture(1), dmMemberFixture(2, { display_name: 'Ada' }), dmMemberFixture(3, { display_name: 'Bo' })] })]
    useDmCallsStore().ringOutgoing({ ...call, callerId: me }, 45)
    useVoiceStore().currentChannelId = talk.id
    const w = mount(CallHost)
    expect(w.get('[data-testid="outgoing-call"]').text()).toContain('Calling Ada, Bo…')
    vi.advanceTimersByTime(45_000)
    await w.vm.$nextTick()
    expect(w.get('[data-testid="outgoing-call"]').text()).toContain('Nobody has joined yet')
    w.unmount()
  })
})

describe('the caller\'s other windows', () => {
  it('show no strip: only the window in the Talk can hang up', async () => {
    world()
    useDmCallsStore().ringOutgoing({ ...call, callerId: me }, 45)
    const w = mount(CallHost)
    expect(w.find('[data-testid="outgoing-call"]').exists()).toBe(false)
    useVoiceStore().currentChannelId = talk.id
    await w.vm.$nextTick()
    expect(w.find('[data-testid="outgoing-call"]').exists()).toBe(true)
    useVoiceStore().currentChannelId = null
    await w.vm.$nextTick()
    expect(w.find('[data-testid="outgoing-call"]').exists()).toBe(false)
    w.unmount()
  })
})

describe('a missed call in the conversation', () => {
  it('tells the callee who called and lets them call back', async () => {
    world()
    apiMock.mockResolvedValue({ channel_id: talk.id, message: {} })
    const w = mount(CallMissedNote, { props: { from: 'Ada', fromId: ada, conversationId: conversation } })
    expect(w.get('[data-testid="call-missed-text"]').text()).toBe('Missed call from Ada')
    await w.get('[data-testid="call-missed-back"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/dms/${conversation}/talk`, expect.objectContaining({ method: 'POST' }))
    expect(joinVoiceChannel).toHaveBeenCalled()
  })

  it('tells the caller that nobody answered, without a call back', () => {
    world()
    const w = mount(CallMissedNote, { props: { from: 'Me', fromId: me, conversationId: conversation } })
    expect(w.get('[data-testid="call-missed-text"]').text()).toBe('Nobody answered your call')
    expect(w.find('[data-testid="call-missed-back"]').exists()).toBe(false)
  })

  it('is what a message with a missed-call embed shows', () => {
    world()
    const w = mount(MessageEmbeds, { props: { embeds: [{ title: 'Talk 1', call: 'missed' }], author: 'Ada', authorId: ada, channelId: conversation } })
    expect(w.find('[data-testid="call-missed"]').exists()).toBe(true)
    expect(w.find('[data-testid="embed"]').exists()).toBe(false)
  })
})

describe('the conversation list', () => {
  const dm = dmFixture({ id: conversation, members: [dmMemberFixture(1), dmMemberFixture(2, { display_name: 'Ada' })], last_message: { user_id: ada, text: '', has_attachment: false, has_poll: false, call: 'missed' } })

  it('marks a conversation with a call and says so instead of the preview', () => {
    world()
    const w = mount(DmListRow, { props: { dm, call: 'ringing' } })
    expect(w.get('[data-testid="dm-call"]').text()).toBe('Incoming call')
    expect(w.find('[data-testid="dm-preview"]').exists()).toBe(false)
    expect(w.get('[data-testid="call-mark"]').attributes('data-mark')).toBe('ringing')
    expect(w.get('[data-testid="dm-open"]').attributes('aria-label')).toContain('Incoming call')
    for (const [mark, text] of [['calling', 'Calling…'], ['active', 'In a call']] as const) {
      expect(mount(DmListRow, { props: { dm, call: mark } }).get('[data-testid="dm-call"]').text()).toBe(text)
    }
  })

  it('previews a missed call as one', () => {
    world()
    const w = mount(DmListRow, { props: { dm } })
    expect(w.get('[data-testid="dm-preview"]').text()).toBe('Missed call')
  })

  it('moves the phone only while it rings', () => {
    expect(mount(CallMark, { props: { mark: 'ringing' } }).classes()).toContain('call-phone-ring')
    expect(mount(CallMark, { props: { mark: 'active' } }).classes()).not.toContain('call-phone-ring')
  })
})
