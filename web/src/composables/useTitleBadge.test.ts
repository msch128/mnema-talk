import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope, nextTick } from 'vue'
import { useChatStore } from '../stores/chat'
import { readStateFixture } from '../test-fixtures.fixture'
import { useTitleBadge } from './useTitleBadge'

let scope: ReturnType<typeof effectScope>

beforeEach(() => {
  setActivePinia(createPinia())
  document.title = 'Mnema Talk'
  scope = effectScope()
})
afterEach(() => scope.stop())

describe('useTitleBadge', () => {
  it('puts the mention count, then a dot, in front of the title and takes it away again', async () => {
    const chat = useChatStore()
    scope.run(() => useTitleBadge())
    expect(document.title).toBe('Mnema Talk')

    chat.readStates = { a: readStateFixture({ channel_id: 'a', unread_count: 2, mention_count: 0 }) }
    await nextTick()
    expect(document.title).toBe('• Mnema Talk')

    chat.readStates = { a: readStateFixture({ channel_id: 'a', unread_count: 2, mention_count: 3 }) }
    await nextTick()
    expect(document.title).toBe('(3) Mnema Talk')

    chat.readStates = { a: readStateFixture({ channel_id: 'a', unread_count: 5, mention_count: 0, notify_level: 'mute' }) }
    await nextTick()
    expect(document.title).toBe('Mnema Talk')

    chat.readStates = { a: readStateFixture({ channel_id: 'a', unread_count: 0, mention_count: 2 }) }
    await nextTick()
    scope.stop()
    expect(document.title).toBe('Mnema Talk')
  })
})
