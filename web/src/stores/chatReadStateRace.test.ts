import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { channelFixture, fixtureId, readStateFixture } from '../test-fixtures.fixture'

const pending = vi.hoisted(() => [] as { url: string; resolve: (value: unknown) => void }[])

vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
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
})
