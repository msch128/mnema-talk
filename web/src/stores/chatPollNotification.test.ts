// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'
import { fixtureId, memoryStorageFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'

beforeEach(() => { setActivePinia(createPinia()); vi.stubGlobal('localStorage', memoryStorageFixture()); vi.useFakeTimers(); setLocale('en') })
afterEach(() => { useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('notifications for polls', () => {
  it('say "Poll: question" instead of the bare question', () => {
    const created: { options: NotificationOptions }[] = []
    class HeadlessNotification {
      static permission: NotificationPermission = 'granted'
      onclick: (() => void) | null = null
      close = vi.fn()
      constructor(public title: string, public options: NotificationOptions) { created.push(this) }
    }
    vi.stubGlobal('Notification', HeadlessNotification)
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    const chat = useChatStore()
    const poll = { multiple: false, anonymous: false, closed: false, closes_at: '2099-01-01T00:00:00Z', total_voters: 0, options: [] }
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ user_id: fixtureId(9), content: 'Pizza or pasta?', poll }) })
    chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ id: fixtureId(8), user_id: fixtureId(9), content: 'Plain words' }) })
    expect(created.map(n => required(n).options.body)).toEqual(['Poll: Pizza or pasta?', 'Plain words'])
  })
})
