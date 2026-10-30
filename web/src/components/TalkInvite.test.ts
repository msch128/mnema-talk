import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import TalkInviteCard from './TalkInviteCard.vue'
import TalkLeaderMark from './TalkLeaderMark.vue'
import DmTalkActions from './DmTalkActions.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useDmCallsStore } from '../stores/dmCalls'
import { PERMISSION_BITS } from '../lib/permissions'
import { channelFixture, fixtureId, messageFixture } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import { ApiError } from '../lib/api'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const joinVoiceChannel = vi.fn()
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel }) }))
const navigate = vi.fn()
vi.mock('../lib/router', async importOriginal => ({ ...(await importOriginal<typeof import('../lib/router')>()), navigate: (...a: unknown[]) => navigate(...a) }))

const talk = channelFixture({ id: fixtureId(40), type: 'voice', name: 'Talk 1' })

function world(connect: boolean, known = true) {
  const chat = useChatStore()
  chat.uncategorized = known ? [talk] : []
  chat.fetchChannels = vi.fn(async () => {})
  const permissions = usePermissionsStore()
  permissions.info = {
    owner: false, permissions: PERMISSION_BITS.view_channel, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
    channels: connect ? [{ channel_id: talk.id, permissions: PERMISSION_BITS.view_channel | PERMISSION_BITS.connect }] : []
  }
  permissions.load = vi.fn(async () => {})
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  joinVoiceChannel.mockReset()
  navigate.mockReset()
})

describe('the invitation card', () => {
  it('joins the Talk like any other when the member may', async () => {
    world(true)
    const w = mount(TalkInviteCard, { props: { talkId: talk.id, title: 'Talk 1', from: 'Ann' } })
    expect(w.text()).toContain('Ann invites you to a Talk')
    await w.find('[data-testid="talk-invite-join"]').trigger('click')
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
    expect(navigate).toHaveBeenCalledWith(`/v/${talk.id}`)
  })

  it('offers no button for a Talk the member cannot connect to or cannot see', () => {
    world(false)
    const denied = mount(TalkInviteCard, { props: { talkId: talk.id, title: 'Talk 1' } })
    expect(denied.find('[data-testid="talk-invite-join"]').exists()).toBe(false)
    expect(denied.find('[data-testid="talk-invite-unavailable"]').exists()).toBe(true)

    world(true, false)
    const unknown = mount(TalkInviteCard, { props: { talkId: talk.id, title: 'Talk 1' } })
    expect(unknown.find('[data-testid="talk-invite-join"]').exists()).toBe(false)
  })

  it('says so when the member is in the Talk already', () => {
    world(true)
    useVoiceStore().currentChannelId = talk.id
    const w = mount(TalkInviteCard, { props: { talkId: talk.id } })
    expect(w.find('[data-testid="talk-invite-in"]').exists()).toBe(true)
    expect(w.find('[data-testid="talk-invite-join"]').exists()).toBe(false)
  })

  it('replaces the plain embed for a message with talk_id and leaves other embeds alone', () => {
    world(true)
    const msg = messageFixture({ embeds: [{ title: 'Talk 1', talk_id: talk.id }, { title: 'News' }] })
    const w = mount(MessageEmbeds, { props: { embeds: msg.embeds ?? [], author: 'Ann' } })
    expect(w.findAll('[data-testid="talk-invite"]')).toHaveLength(1)
    expect(w.findAll('[data-testid="embed"]')).toHaveLength(1)
  })
})

describe('the leader mark', () => {
  it('is a labelled icon', () => {
    const w = mount(TalkLeaderMark)
    expect(w.attributes('aria-label')).toBe('Talk leader')
  })
})

describe('Start a Talk and Invite to my Talk', () => {
  const conversation = fixtureId(60)

  it('starts a Talk, then joins it', async () => {
    world(true)
    apiMock.mockResolvedValue({ channel_id: talk.id, message: messageFixture() })
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: true } })
    await w.find('[data-testid="dm-start-talk"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/dms/${conversation}/talk`, expect.objectContaining({ method: 'POST' }))
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
  })

  it('says so when the Talk could not be started', async () => {
    world(true)
    apiMock.mockRejectedValue(new ApiError(429, 'RATE_LIMITED', 'Too many requests.'))
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: true } })
    await w.find('[data-testid="dm-start-talk"]').trigger('click')
    await flushPromises()
    expect(joinVoiceChannel).not.toHaveBeenCalled()
    expect(useToastStore().toasts[0]?.text).toBe('Too many requests.')
  })

  it('invites only from inside a Talk', async () => {
    world(true)
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: true } })
    expect(w.find('[data-testid="dm-invite-to-talk"]').exists()).toBe(false)
    useVoiceStore().currentChannelId = talk.id
    await w.vm.$nextTick()
    apiMock.mockResolvedValue(messageFixture())
    await w.find('[data-testid="dm-invite-to-talk"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/talks/${talk.id}/invites`, expect.objectContaining({ json: { conversation_id: conversation } }))
  })

  it('joins the call that is going on instead of starting a second Talk', async () => {
    world(true)
    useDmCallsStore().track({ conversationId: conversation, talkId: talk.id, messageId: fixtureId(61), callerId: fixtureId(2), talkName: 'Talk 1' })
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: true } })
    expect(w.find('[data-testid="dm-start-talk"]').exists()).toBe(false)
    await w.find('[data-testid="dm-join-talk"]').trigger('click')
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalledWith(`/api/dms/${conversation}/talk`, expect.anything())
    expect(joinVoiceChannel).toHaveBeenCalledWith(talk.id)
  })

  it('offers neither once the member is in the call', () => {
    world(true)
    useDmCallsStore().track({ conversationId: conversation, talkId: talk.id, messageId: fixtureId(61), callerId: fixtureId(2), talkName: 'Talk 1' })
    useVoiceStore().currentChannelId = talk.id
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: true } })
    expect(w.find('[data-testid="dm-start-talk"]').exists()).toBe(false)
    expect(w.find('[data-testid="dm-join-talk"]').exists()).toBe(false)
  })

  it('is disabled where the member cannot write', () => {
    world(true)
    const w = mount(DmTalkActions, { props: { conversationId: conversation, canSend: false } })
    expect(w.find<HTMLButtonElement>('[data-testid="dm-start-talk"]').element.disabled).toBe(true)
  })
})
