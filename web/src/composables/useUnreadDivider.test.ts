import { required } from '../store-test-support.fixture'
import type { EffectScope } from 'vue'
import { channelFixture, userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useUnreadDivider } from './useUnreadDivider'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

let scope: EffectScope | undefined
let chat: ReturnType<typeof useChatStore>

function setup() {
  scope = effectScope()
  return required(scope.run(() => useUnreadDivider()))
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me' })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
})

afterEach(() => {
  scope?.stop()
  vi.restoreAllMocks()
})

describe('useUnreadDivider', () => {
  it('markAllRead does nothing without an active channel', () => {
    chat.activeChannel = null
    expect(setup().markAllRead()).toBe(false)
  })

  it('markAllRead does nothing when nothing is unread', () => {
    const mark = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined as never)
    expect(setup().markAllRead()).toBe(false)
    expect(mark).not.toHaveBeenCalled()
  })

  it('markAllRead marks the channel read when it has unread messages', () => {
    chat.readStates = { ch1: { unread_count: 2, mention_count: 0, last_read_at: null } } as never
    const mark = vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined as never)
    expect(setup().markAllRead()).toBe(true)
    expect(mark).toHaveBeenCalledWith('ch1')
  })

  it('markAllRead also reacts to mentions alone', () => {
    chat.readStates = { ch1: { unread_count: 0, mention_count: 1, last_read_at: null } } as never
    vi.spyOn(chat, 'markChannelRead').mockResolvedValue(undefined as never)
    expect(setup().markAllRead()).toBe(true)
  })

  it('labels a divider from today with the time only', () => {
    chat.activeChannelLastReadAt = new Date().toISOString()
    expect(setup().dividerLabel()).toMatch(/\d/)
  })

  it('labels an older divider with the date', () => {
    chat.activeChannelLastReadAt = '2020-03-04T10:00:00Z'
    expect(setup().dividerLabel()).toMatch(/Mar|4/)
  })

  it('labels a cleared divider without throwing', () => {
    chat.activeChannelLastReadAt = null
    expect(typeof setup().dividerLabel()).toBe('string')
  })
})
