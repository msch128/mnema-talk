// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useChatStore } from './chat'
import { useAuthStore } from './auth'
import { fixtureId, memoryStorageFixture, messageFixture, userFixture } from '../test-fixtures.fixture'
import { required } from '../store-test-support.fixture'

beforeEach(() => { setActivePinia(createPinia()); vi.stubGlobal('localStorage', memoryStorageFixture()); vi.useFakeTimers() })
afterEach(() => { useChatStore().closeWebSocket(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('chat without browser globals', () => {
  it('initializes and closes without installing DOM or activity listeners', async () => {
    expect(typeof window).toBe('undefined'); expect(typeof document).toBe('undefined')
    const chat = useChatStore()
    expect(chat.members).toEqual([]); expect(chat.isConnected).toBe(false)
    expect(await chat.requestNotificationPermission()).toBe('unsupported')
    expect(vi.getTimerCount()).toBe(0)
    chat.closeWebSocket(); expect(chat.wasConnected).toBe(false)
  })

  it('closes a notification safely when no window is available for its click action', () => {
    const created: HeadlessNotification[] = []
    class HeadlessNotification {
      static permission: NotificationPermission = 'granted'
      onclick: (() => void) | null = null
      close = vi.fn()
      constructor(public title: string, public options: NotificationOptions) { created.push(this) }
    }
    vi.stubGlobal('Notification', HeadlessNotification)
    useAuthStore().user = userFixture({ id: fixtureId(1) })
    const chat = useChatStore()
    const message = messageFixture({ user_id: fixtureId(9) })
    chat.handleWSEvent({ type: 'message_create', payload: message })
    const notification = required(created[0])
    expect(notification.options.body).toBe(message.content)
    expect(() => required(notification.onclick)()).not.toThrow()
    expect(notification.close).toHaveBeenCalledOnce()
  })
})
