import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { categoryFixture, channelFixture, fixtureId, readStateFixture } from '../test-fixtures.fixture'

const pending = vi.hoisted(() => [] as { url: string; resolve: (value: unknown) => void }[])

vi.mock('../shared/lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../shared/lib/api')>()),
  api: vi.fn((url: string) => new Promise(resolve => { pending.push({ url, resolve }) }))
}))

const X = fixtureId(2)

beforeEach(() => { setActivePinia(createPinia()); pending.length = 0 })
afterEach(() => { useChatStore().closeWebSocket() })

const getPending = () => pending.filter(p => p.url === '/api/read-state')

describe('read state answers that arrive late', () => {
  it('do not bring back a badge that was just cleared', async () => {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ id: X })
    chat.readStates = { [X]: readStateFixture({ channel_id: X, unread_count: 3 }) }
    const fetched = chat.fetchReadState()
    const clearing = chat.markChannelRead(X)
    chat.activeChannel = channelFixture({ id: fixtureId(3) })
    pending.find(p => p.url === `/api/channels/${X}/read`)?.resolve({})
    await clearing
    getPending()[0]?.resolve([readStateFixture({ channel_id: X, unread_count: 3, mention_count: 1 })])
    await fetched
    expect(chat.readStates[X]?.unread_count).toBe(0)
    expect(chat.readStates[X]?.mention_count).toBe(0)
  })

  it('from an older request never replace the answer of a newer one', async () => {
    const chat = useChatStore()
    const older = chat.fetchReadState()
    const newer = chat.fetchReadState()
    getPending()[1]?.resolve([readStateFixture({ channel_id: X, unread_count: 1 })])
    await newer
    getPending()[0]?.resolve([readStateFixture({ channel_id: X, unread_count: 9 })])
    await older
    expect(chat.readStates[X]?.unread_count).toBe(1)
  })

  it('keep the fresh counts of channels outside the category that was marked read', async () => {
    const chat = useChatStore()
    const inside = fixtureId(4)
    chat.categories = [categoryFixture({ id: 'cat', channels: [channelFixture({ id: inside })] }), categoryFixture({ id: 'other', channels: [channelFixture({ id: X })] })]
    chat.readStates = { [inside]: readStateFixture({ channel_id: inside, unread_count: 2 }), [X]: readStateFixture({ channel_id: X, unread_count: 0 }) }
    const fetched = chat.fetchReadState()
    const marking = chat.markAllRead('cat')
    await Promise.resolve()
    pending.find(p => p.url === '/api/read-state/read-all')?.resolve({})
    await marking
    getPending()[0]?.resolve([readStateFixture({ channel_id: inside, unread_count: 2 }), readStateFixture({ channel_id: X, unread_count: 5 })])
    await fetched
    expect(chat.readStates[X]?.unread_count).toBe(5)
    expect(chat.readStates[inside]?.unread_count).toBe(0)
  })

  it('move the new-since divider to the kept answer when a newer fetch overtakes the unread mark', async () => {
    const chat = useChatStore()
    chat.activeChannel = channelFixture({ id: X })
    chat.readStates = { [X]: readStateFixture({ channel_id: X, last_read_at: '2026-01-01T00:00:00Z' }) }
    chat.activeChannelLastReadAt = '2026-01-01T00:00:00Z'
    const marking = chat.markChannelUnread(X, 'm1')
    pending.find(p => p.url === `/api/channels/${X}/unread`)?.resolve({})
    await vi.waitFor(() => expect(getPending()).toHaveLength(1))
    const newer = chat.fetchReadState()
    getPending()[0]?.resolve([readStateFixture({ channel_id: X, last_read_at: '2026-01-01T00:00:00Z' })])
    await new Promise(r => setTimeout(r, 0))
    getPending()[1]?.resolve([readStateFixture({ channel_id: X, last_read_at: '2026-02-02T00:00:00Z' })])
    await newer
    await marking
    expect(chat.activeChannelLastReadAt).toBe('2026-02-02T00:00:00Z')
  })
})
