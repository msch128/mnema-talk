import type { ApiOptions } from '../lib/api'
import type { EffectScope } from 'vue'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useFirstUnreadJump } from './useFirstUnreadJump'
import { useUnreadDivider } from './useUnreadDivider'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { channelFixture, messageFixture, readStateFixture, userFixture, fixtureId } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const ME = fixtureId(1)
const CHANNEL = fixtureId(2)
const OPENED = '2026-01-02T00:00:00.000Z'
const theirs = (n: number, at: string) => messageFixture({ id: fixtureId(200 + n), channel_id: CHANNEL, user_id: fixtureId(9), created_at: at })

let scope: EffectScope | undefined

// The divider sits above the visible part of the list, as after scrolling to the bottom.
function listWithDividerAbove() {
  const el = document.createElement('div')
  el.getBoundingClientRect = () => ({ top: 100, bottom: 500 }) as DOMRect
  const divider = document.createElement('div')
  divider.setAttribute('data-unread-divider', '')
  divider.getBoundingClientRect = () => ({ top: -60, bottom: -20 }) as DOMRect
  el.appendChild(divider)
  return el
}

function open() {
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: CHANNEL, type: 'text' })
  chat.readStates = { [CHANNEL]: readStateFixture({ channel_id: CHANNEL, unread_count: 2, last_read_at: OPENED }) }
  chat.activeChannelLastReadAt = OPENED
  chat.messages = [theirs(1, '2026-01-03T00:00:00.000Z'), theirs(2, '2026-01-03T01:00:00.000Z')]
  const list = listWithDividerAbove()
  scope = effectScope()
  const api = scope.run(() => {
    const divider = useUnreadDivider()
    const jump = useFirstUnreadJump({ container: ref(list), since: divider.dividerSince, dividerBeforeId: divider.dividerBeforeId })
    return { divider, jump }
  })
  if (!api) throw new Error('composables did not start')
  list.dispatchEvent(new Event('scroll'))
  return { chat, ...api }
}

beforeEach(() => {
  apiMock.mockReset()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: ME })
})

afterEach(() => scope?.stop())

describe('the "Jump to first unread" pill and sending', () => {
  it('goes away when you send a message, and returns for what others post afterwards', async () => {
    const { chat, divider, jump } = open()
    await nextTick()
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(true)

    apiMock.mockResolvedValue(messageFixture({ id: fixtureId(300), channel_id: CHANNEL, user_id: ME, created_at: '2026-01-03T02:00:00.000Z' }))
    await chat.sendMessage('my answer')
    await nextTick()
    await nextTick()

    expect(jump.showJumpToUnread.value).toBe(false)
    expect(divider.dividerBeforeId.value).toBeNull()
    expect(chat.readStates[CHANNEL]).toMatchObject({ unread_count: 0, mention_count: 0, last_read_at: '2026-01-03T02:00:00.000Z' })

    chat.messages = [...chat.messages, theirs(3, '2026-01-03T03:00:00.000Z')]
    await nextTick()
    await nextTick()
    expect(divider.dividerBeforeId.value).toBe(fixtureId(203))
    expect(jump.showJumpToUnread.value).toBe(true)
  })

  it('leaves the channel unread when the message is a thread reply', async () => {
    const { chat, jump } = open()
    await nextTick()
    apiMock.mockResolvedValue(messageFixture({ id: fixtureId(301), channel_id: CHANNEL, user_id: ME, parent_id: fixtureId(201), created_at: '2026-01-03T02:00:00.000Z' }))
    await chat.sendMessage('in the thread', fixtureId(201))
    await nextTick()
    await nextTick()
    expect(chat.readStates[CHANNEL]?.unread_count).toBe(2)
    expect(jump.showJumpToUnread.value).toBe(true)
  })
})
