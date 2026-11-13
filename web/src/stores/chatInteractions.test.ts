import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { FIXTURE_TIMESTAMP, fixtureId, messageFixture } from '../test-fixtures.fixture'
import type { InteractionResponse } from '../types/events'
import type { ChatActionRow } from '../types/rest'

beforeEach(() => setActivePinia(createPinia()))

const button: ChatActionRow = { type: 1, components: [{ type: 2, style: 1, label: 'Again', custom_id: 'again' }] }
const note = (id: number, content: string, extra: Partial<InteractionResponse> = {}): InteractionResponse => ({
  id: fixtureId(id), channel_id: fixtureId(2), bot_id: fixtureId(3), bot_name: 'Helper', content, embeds: null, created_at: FIXTURE_TIMESTAMP, ...extra
})

describe('private notes of a bot', () => {
  it('replaces the note with the same id in place, with the new components', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'interaction_response', payload: note(10, 'first', { components: [button] }) })
    chat.handleWSEvent({ type: 'interaction_response', payload: note(11, 'other') })
    const key = chat.ephemeral[0]!.id
    chat.handleWSEvent({ type: 'interaction_response', payload: note(10, 'updated') })
    expect(chat.ephemeral.map(n => n.content)).toEqual(['updated', 'other'])
    expect(chat.ephemeral[0]!.id).toBe(key)
    expect(chat.ephemeral[0]!.note_id).toBe(fixtureId(10))
    expect(chat.ephemeral[0]!.components).toEqual([])
  })

  it('removes the note a deletion names, and nothing else', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'interaction_response', payload: note(10, 'first') })
    chat.handleWSEvent({ type: 'interaction_response', payload: note(11, 'second') })
    chat.handleWSEvent({ type: 'interaction_response', payload: note(10, '', { deleted: true }) })
    expect(chat.ephemeral.map(n => n.content)).toEqual(['second'])
  })
})

describe('components of a bot message', () => {
  it('go when an update carries none', () => {
    const chat = useChatStore()
    const msg = messageFixture({ id: fixtureId(7), is_bot: true, components: [button] })
    chat.messages = [msg]
    const rest = { ...msg }
    delete rest.components
    chat.handleWSEvent({ type: 'message_update', payload: { ...rest, content: 'done', is_edited: true } })
    expect(chat.messages[0]!.content).toBe('done')
    expect(chat.messages[0]!.components).toBeUndefined()
    chat.handleWSEvent({ type: 'message_update', payload: { ...rest, components: [button] } })
    expect(chat.messages[0]!.components).toEqual([button])
  })
})

describe('a click the bot deferred', () => {
  it('is kept by its interaction id until the community is left', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'interaction_deferred', payload: { interaction_id: fixtureId(40), channel_id: fixtureId(2), bot_id: fixtureId(3) } })
    expect(chat.botDeferral).toMatchObject({ interactionId: fixtureId(40), channelId: fixtureId(2) })
    chat.resetCommunityState()
    expect(chat.botDeferral).toBeNull()
  })
})
