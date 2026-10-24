import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useEventsStore } from './events'
import { useToastStore } from './toast'
import { useVoiceStore } from './voice'
import { useEmojiStore } from './emoji'
import { usePermissionsStore } from './permissions'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import type { CommunityEvent } from '../types/domain'

function makeEvent(overrides: Partial<CommunityEvent> = {}): CommunityEvent {
  return {
    id: fixtureId(1),
    title: 'Game Night',
    description: '',
    start_time: '2026-11-01T18:00:00Z',
    status: 'scheduled',
    created_at: FIXTURE_TIMESTAMP,
    updated_at: FIXTURE_TIMESTAMP,
    rsvp_counts: { going: 0, interested: 0, not_going: 0 },
    ...overrides
  }
}

beforeEach(() => { setActivePinia(createPinia()) })

describe('community event socket events', () => {
  it('adds a new event and replaces a changed one on event_update', () => {
    const chat = useChatStore()
    const events = useEventsStore()
    chat.handleWSEvent({ type: 'event_update', payload: makeEvent() })
    expect(events.events.map(e => e.title)).toEqual(['Game Night'])

    chat.handleWSEvent({ type: 'event_update', payload: makeEvent({ title: 'Renamed' }) })
    expect(events.events).toHaveLength(1)
    expect(events.events[0]?.title).toBe('Renamed')
  })

  it('removes only the deleted event on event_delete', () => {
    const chat = useChatStore()
    const events = useEventsStore()
    events.events = [makeEvent({ id: fixtureId(1) }), makeEvent({ id: fixtureId(2), title: 'Other' })]
    chat.handleWSEvent({ type: 'event_delete', payload: { id: fixtureId(1) } })
    expect(events.events.map(e => e.title)).toEqual(['Other'])
  })

  it('shows a reminder toast naming the event on event_reminder', () => {
    const chat = useChatStore()
    chat.handleWSEvent({
      type: 'event_reminder',
      payload: { event_id: fixtureId(1), title: 'Game Night', start_time: '2026-11-01T18:00:00Z' }
    })
    expect(useToastStore().toasts.some(t => t.text.includes('Game Night'))).toBe(true)
  })

  it('signals a forum tag change for the channel each time it happens', () => {
    const chat = useChatStore()
    expect(chat.forumTagsSignal).toBeNull()
    chat.handleWSEvent({ type: 'forum_tags_changed', payload: { channel_id: fixtureId(5) } })
    expect(chat.forumTagsSignal).toEqual({ channelId: fixtureId(5), seq: 1 })
    chat.handleWSEvent({ type: 'forum_tags_changed', payload: { channel_id: fixtureId(5) } })
    expect(chat.forumTagsSignal?.seq).toBe(2)
  })
})

describe('other live updates', () => {
  it('forwards stage and Talk reaction events to the voice layer', () => {
    const chat = useChatStore()
    const voice = useVoiceStore()
    const hand = vi.spyOn(voice, 'handleStageHand').mockImplementation(() => {})
    const power = vi.spyOn(voice, 'handleStageTalkPower').mockImplementation(() => {})
    const reaction = vi.spyOn(voice, 'handleTalkReaction').mockImplementation(() => {})
    const stage = fixtureId(2)
    const user = fixtureId(3)
    chat.handleWSEvent({ type: 'stage_hand', payload: { channel_id: stage, user_id: user, raised: true } })
    chat.handleWSEvent({ type: 'stage_talk_power', payload: { channel_id: stage, user_id: user, granted: true } })
    chat.handleWSEvent({ type: 'talk_reaction', payload: { channel_id: stage, user_id: user, emoji: 'x' } })
    expect(hand).toHaveBeenCalledWith({ channel_id: stage, user_id: user, raised: true })
    expect(power).toHaveBeenCalledWith({ channel_id: stage, user_id: user, granted: true })
    expect(reaction).toHaveBeenCalledWith({ channel_id: stage, user_id: user, emoji: 'x' })
  })

  it('reloads emoji when the emoji set changed', () => {
    const load = vi.spyOn(useEmojiStore(), 'load').mockResolvedValue()
    useChatStore().handleWSEvent({ type: 'emoji_changed', payload: null })
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('reloads roles and permissions when roles changed', () => {
    const permissions = usePermissionsStore()
    const roles = vi.spyOn(permissions, 'loadRoles').mockResolvedValue()
    const load = vi.spyOn(permissions, 'load').mockResolvedValue()
    useChatStore().handleWSEvent({ type: 'roles_changed', payload: null })
    expect(roles).toHaveBeenCalledTimes(1)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('collects a bot interaction response as an ephemeral message', () => {
    const chat = useChatStore()
    chat.handleWSEvent({
      type: 'interaction_response',
      payload: {
        channel_id: fixtureId(2),
        bot_id: fixtureId(3),
        bot_name: 'Helper',
        content: 'Only you see this',
        embeds: null,
        created_at: FIXTURE_TIMESTAMP
      }
    })
    expect(chat.ephemeral).toHaveLength(1)
    expect(chat.ephemeral[0]?.content).toBe('Only you see this')
  })
})
