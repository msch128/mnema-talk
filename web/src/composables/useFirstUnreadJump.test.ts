import type { ApiOptions } from '../lib/api'
import type { EffectScope } from 'vue'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useFirstUnreadJump } from './useFirstUnreadJump'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { required } from '../store-test-support.fixture'
import { channelFixture, messageFixture, userFixture, fixtureId } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const SINCE = '2026-01-02T00:00:00.000Z'
const read = (n: number) => messageFixture({ id: fixtureId(100 + n), user_id: fixtureId(9), created_at: '2026-01-01T00:00:00.000Z' })
const unread = (n: number) => messageFixture({ id: fixtureId(200 + n), user_id: fixtureId(9), created_at: '2026-01-03T00:00:00.000Z' })

let scope: EffectScope | undefined
let chat: ReturnType<typeof useChatStore>

function setup(dividerBeforeId: string | null = null) {
  const container = ref<HTMLElement | null>(document.createElement('div'))
  const since = ref<string | null>(SINCE)
  const divider = ref<string | null>(dividerBeforeId)
  scope = effectScope()
  const api = scope.run(() => useFirstUnreadJump({ container, since, dividerBeforeId: divider }))
  if (!api) throw new Error('composable did not start')
  return { ...api, container, since, divider }
}

beforeEach(() => {
  apiMock.mockReset()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: fixtureId(1) })
  chat = useChatStore()
  chat.activeChannel = channelFixture({ id: 'ch1', type: 'text' })
})

afterEach(() => scope?.stop())

describe('useFirstUnreadJump', () => {
  it('asks the server for the first unread message when the loaded page starts inside the unread run', async () => {
    chat.messages = [unread(1), unread(2)]
    chat.hasMoreBefore = true
    apiMock.mockResolvedValue({ message_id: fixtureId(150) })
    const jump = setup()
    await vi.waitFor(() => expect(jump.showJumpToUnread.value).toBe(true))
    expect(apiMock.mock.calls[0]?.[0]).toBe(`/api/channels/ch1/first-unread?since=${encodeURIComponent(SINCE)}`)

    const to = vi.spyOn(chat, 'jumpToMessage').mockResolvedValue(true)
    await jump.jumpToFirstUnread()
    expect(to).toHaveBeenCalledWith(fixtureId(150))
  })

  it('does not ask while the divider is in the loaded messages', async () => {
    chat.messages = [read(1), unread(1)]
    chat.hasMoreBefore = true
    const jump = setup(unread(1).id)
    await nextTick()
    expect(apiMock).not.toHaveBeenCalled()
    expect(jump.showJumpToUnread.value).toBe(false)
  })

  it('shows the pill once the divider has scrolled above the viewport and scrolls back to it', async () => {
    chat.messages = [read(1), unread(1)]
    const jump = setup(unread(1).id)
    const root = required(jump.container.value)
    const divider = document.createElement('div')
    divider.setAttribute('data-unread-divider', '')
    divider.scrollIntoView = vi.fn()
    root.append(divider)
    root.getBoundingClientRect = () => ({ top: 100 }) as DOMRect
    divider.getBoundingClientRect = () => ({ bottom: 40 }) as DOMRect

    root.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(true)

    await jump.jumpToFirstUnread()
    expect(divider.scrollIntoView).toHaveBeenCalledWith({ block: 'start' })

    divider.getBoundingClientRect = () => ({ bottom: 160 }) as DOMRect
    root.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(false)
  })

  it('stays hidden when the server cannot tell (older server, desktop client)', async () => {
    chat.messages = [unread(1)]
    chat.hasMoreBefore = true
    apiMock.mockRejectedValue(new Error('unavailable'))
    const jump = setup()
    await vi.waitFor(() => expect(apiMock).toHaveBeenCalled())
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(false)
  })

  it('hides the pill once the channel was marked read (no marker to show)', async () => {
    chat.messages = [unread(1)]
    chat.hasMoreBefore = true
    apiMock.mockResolvedValue({ message_id: fixtureId(150) })
    const jump = setup()
    await vi.waitFor(() => expect(jump.showJumpToUnread.value).toBe(true))
    jump.since.value = null
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(false)
  })

  it('ignores an answer for a channel that is no longer open', async () => {
    chat.messages = [unread(1)]
    chat.hasMoreBefore = true
    let answer: (value: unknown) => void = () => {}
    apiMock.mockReturnValue(new Promise(resolve => { answer = resolve }))
    const jump = setup()
    await vi.waitFor(() => expect(apiMock).toHaveBeenCalled())
    chat.hasMoreBefore = false
    await nextTick()
    answer({ message_id: fixtureId(150) })
    await nextTick()
    expect(jump.showJumpToUnread.value).toBe(false)
  })
})

