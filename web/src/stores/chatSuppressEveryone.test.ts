// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { setLocale } from '../i18n'
import { fixtureId, memoryStorageFixture, messageFixture, readStateFixture, userFixture } from '../test-fixtures.fixture'

const me = fixtureId(1)
const created: unknown[] = []

class HeadlessNotification {
  static permission: NotificationPermission = 'granted'
  onclick: (() => void) | null = null
  close = vi.fn()
  constructor(public title: string, public options: NotificationOptions) { created.push(this) }
}

beforeEach(() => {
  setActivePinia(createPinia()); vi.stubGlobal('localStorage', memoryStorageFixture()); vi.useFakeTimers(); setLocale('en')
  created.length = 0
  vi.stubGlobal('Notification', HeadlessNotification)
  useAuthStore().user = userFixture({ id: me })
})
afterEach(() => { useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

function receiveAtAll(suppress: boolean) {
  const chat = useChatStore()
  chat.notifySettings = { channels: [], categories: [], server: { level: 'all', muted_until: null, suppress_everyone: suppress } }
  chat.readStates = { [fixtureId(2)]: readStateFixture({ notify_level: 'mentions' }) }
  chat.handleWSEvent({ type: 'message_create', payload: messageFixture({ user_id: fixtureId(9), content: '@all hello', mentions: [me] }) })
  return chat
}

describe('ignoring @all and @here for the whole community', () => {
  it('leaves the badge and the notification out of a live @all message', () => {
    const chat = receiveAtAll(true)
    expect(chat.readStates[fixtureId(2)]?.mention_count).toBe(0)
    expect(created).toHaveLength(0)
  })

  it('still counts and notifies a live @all message when it is not ignored', () => {
    const chat = receiveAtAll(false)
    expect(chat.readStates[fixtureId(2)]?.mention_count).toBe(1)
    expect(created).toHaveLength(1)
  })
})
