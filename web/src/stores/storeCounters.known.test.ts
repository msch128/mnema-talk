// Failures found by the store trace (storeTrace.test.ts), each shrunk to its
// smallest case; fixed, so they are normal tests now.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from './auth'
import { useChatStore } from './chat'
import { channelFixture, fixtureId, messageFixture, readStateFixture, userFixture } from '../test-fixtures.fixture'

const ME = fixtureId(1)
const BOB = fixtureId(9)
const X = fixtureId(2) // a channel the member is not looking at

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: ME, username: 'max' })
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: fixtureId(4) })
  chat.readStates = { [X]: readStateFixture({ channel_id: X }) }
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

const mention = messageFixture({ id: fixtureId(30), channel_id: X, user_id: BOB, username: 'bob', content: '@max look', mentions: [ME] })
const recount = (unread: number, mentions: number) =>
  ({ type: 'read_state' as const, payload: { channel_id: X, last_read_at: null, unread_count: unread, mention_count: mentions } })
const counts = () => {
  const s = useChatStore().readStates[X]
  return [s?.unread_count ?? 0, s?.mention_count ?? 0]
}

// H-L5-01 (fixed). The server counts unread messages and mentions from the
// messages that exist now and from their current mentions
// (internal/chat/readstate/readstate.go, ReadStateSQL); a client that just
// loaded shows what it says. A delete or an edit brings no message the open
// client could count, so the server sends the members whose count it may move
// their counts again (read_state with the counts, readstate.Recount), right
// after message_delete or message_update; these cases replay both frames as
// the server sends them (internal/server/readstate_recount_integration_test.go
// checks that it does).
describe('unread and mention counters of an open client against the server', () => {
  it('sanity: a mention from someone else counts once as unread and once as a mention', () => {
    useChatStore().handleWSEvent({ type: 'message_create', payload: mention })
    expect(counts()).toEqual([1, 1])
  })

  it('a deleted message no longer counts (server: 0 and 0)', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'message_create', payload: mention })
    chat.handleWSEvent({ type: 'message_delete', payload: { id: mention.id, channel_id: X, parent_id: null } })
    chat.handleWSEvent(recount(0, 0))
    expect(counts()).toEqual([0, 0])
  })

  it('an edit that removes the mention no longer counts as a mention (server: 1 and 0)', () => {
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'message_create', payload: mention })
    chat.handleWSEvent({ type: 'message_update', payload: { ...mention, content: 'never mind', mentions: [], is_edited: true } })
    chat.handleWSEvent(recount(1, 0))
    expect(counts()).toEqual([1, 0])
  })

  it('an edit that adds a mention counts as a mention (server: 1 and 1)', () => {
    const chat = useChatStore()
    const plain = { ...mention, content: 'hello', mentions: [] }
    chat.handleWSEvent({ type: 'message_create', payload: plain })
    chat.handleWSEvent({ type: 'message_update', payload: { ...plain, content: '@max look', mentions: [ME], is_edited: true } })
    chat.handleWSEvent(recount(1, 1))
    expect(counts()).toEqual([1, 1])
  })
})
