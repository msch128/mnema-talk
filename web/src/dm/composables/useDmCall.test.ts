import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from '../../stores/chat'
import { useVoiceStore } from '../../stores/voice'
import { useDmCallsStore } from '../../stores/dmCalls'
import { fixtureId } from '../../test-fixtures.fixture'
import { useDmCall } from './useDmCall'

const order: string[] = []
const leaveVoiceChannel = vi.fn(async () => { order.push('leave call') })
vi.mock('../../talk/composables/useWebRTC', () => ({ useWebRTC: () => ({ joinVoiceChannel: vi.fn(), leaveVoiceChannel }) }))

const conversation = fixtureId(30)
const talk = fixtureId(40)

function inCall(channelId: string | null) {
  useVoiceStore().currentChannelId = channelId
  const chat = useChatStore()
  vi.spyOn(chat, 'leaveDm').mockImplementation(async () => { order.push('leave conversation') })
  return chat
}

beforeEach(() => {
  setActivePinia(createPinia())
  order.length = 0
  leaveVoiceChannel.mockClear()
})

describe('leaving a group conversation', () => {
  it('leaves the call first when the call is in the conversation\'s Talk', async () => {
    const chat = inCall(talk)
    useDmCallsStore().track({ conversationId: conversation, talkId: talk, messageId: fixtureId(61), callerId: fixtureId(2), talkName: 'Talk 1' })
    await useDmCall().leaveConversation({ id: conversation })
    expect(order).toEqual(['leave call', 'leave conversation'])
    expect(chat.leaveDm).toHaveBeenCalledWith(conversation)
  })

  it('knows the Talk from the conversation list when no call is tracked', async () => {
    inCall(talk)
    await useDmCall().leaveConversation({ id: conversation, call: { talk_id: talk, caller_id: fixtureId(2), message_id: fixtureId(61), ring_seconds: 0, ringing: false, talk_name: 'Talk 1' } })
    expect(order).toEqual(['leave call', 'leave conversation'])
  })

  it('stays in a call that is somewhere else', async () => {
    inCall(fixtureId(41))
    useDmCallsStore().track({ conversationId: conversation, talkId: talk, messageId: fixtureId(61), callerId: fixtureId(2), talkName: 'Talk 1' })
    await useDmCall().leaveConversation({ id: conversation })
    expect(leaveVoiceChannel).not.toHaveBeenCalled()
    expect(order).toEqual(['leave conversation'])
  })

  it('leaves nothing but the conversation when not in a call', async () => {
    inCall(null)
    await useDmCall().leaveConversation({ id: conversation })
    expect(order).toEqual(['leave conversation'])
  })
})
