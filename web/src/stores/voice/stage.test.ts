import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useVoiceStore } from '../voice'
import { voiceUserFixture } from '../../test-fixtures.fixture'
import { MAX_REACTIONS, REACTION_MS } from './stage'

const room = 'room-1'

function seatedStore() {
  const voice = useVoiceStore()
  const amy = voiceUserFixture({ id: 'amy' })
  const bob = voiceUserFixture({ id: 'bob' })
  voice.setVoiceSnapshot({ [room]: { amy, bob } })
  return voice
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('talk power and raised hands in a Stage', () => {
  it('shows a raised hand with its time and clears it when lowered', () => {
    const voice = seatedStore()
    voice.handleStageHand({ channel_id: room, user_id: 'amy', raised: true, at: '2026-01-01T10:00:00Z' })
    expect(voice.channelUsers[room]?.amy?.hand_raised_at).toBe('2026-01-01T10:00:00Z')
    expect(voice.channelUsers[room]?.bob?.hand_raised_at).toBeUndefined()
    voice.handleStageHand({ channel_id: room, user_id: 'amy', raised: false })
    expect(voice.channelUsers[room]?.amy?.hand_raised_at).toBeUndefined()
  })

  it('lets a granted member speak and takes the hand down with it', () => {
    const voice = seatedStore()
    voice.handleStageHand({ channel_id: room, user_id: 'amy', raised: true, at: '2026-01-01T10:00:00Z' })
    voice.handleStageTalkPower({ channel_id: room, user_id: 'amy', granted: true })
    expect(voice.hasTalkPower(room, 'amy')).toBe(true)
    expect(voice.channelUsers[room]?.amy?.hand_raised_at).toBeUndefined()
    expect(voice.hasTalkPower(room, 'bob')).toBe(false)
    voice.handleStageTalkPower({ channel_id: room, user_id: 'amy', granted: false })
    expect(voice.hasTalkPower(room, 'amy')).toBe(false)
  })

  it('ignores news about somebody who is not in the Talk', () => {
    const voice = seatedStore()
    voice.handleStageTalkPower({ channel_id: room, user_id: 'ghost', granted: true })
    voice.handleStageHand({ channel_id: 'other', user_id: 'amy', raised: true })
    expect(voice.channelUsers[room]?.ghost).toBeUndefined()
    expect(voice.channelUsers.other).toBeUndefined()
  })
})

describe('live reactions', () => {
  it('float for a few seconds and are then forgotten', () => {
    const voice = seatedStore()
    voice.setChannel(room)
    voice.handleTalkReaction({ channel_id: room, user_id: 'amy', emoji: '🎉' })
    expect(voice.talkReactions.map(r => r.emoji)).toEqual(['🎉'])
    vi.advanceTimersByTime(REACTION_MS - 1)
    expect(voice.talkReactions).toHaveLength(1)
    vi.advanceTimersByTime(2)
    expect(voice.talkReactions).toHaveLength(0)
  })

  it('count only for the Talk I am in', () => {
    const voice = seatedStore()
    voice.handleTalkReaction({ channel_id: room, user_id: 'amy', emoji: '🎉' })
    expect(voice.talkReactions).toHaveLength(0)
    voice.setChannel(room)
    voice.handleTalkReaction({ channel_id: 'other', user_id: 'amy', emoji: '🎉' })
    expect(voice.talkReactions).toHaveLength(0)
  })

  it('keep at most a screenful and end with the call', () => {
    const voice = seatedStore()
    voice.setChannel(room)
    for (let i = 0; i < MAX_REACTIONS + 10; i++) voice.handleTalkReaction({ channel_id: room, user_id: 'amy', emoji: '👍' })
    expect(voice.talkReactions).toHaveLength(MAX_REACTIONS)
    voice.disconnect()
    expect(voice.talkReactions).toHaveLength(0)
  })
})
