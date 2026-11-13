import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useLoadRetry } from './useLoadRetry'
import { useChatStore } from '../../stores/chat'
import { channelFixture, fixtureId } from '../../test-fixtures.fixture'

const channel = channelFixture({ id: fixtureId(7), name: 'general', type: 'text' })

function column(): HTMLElement {
  const el = document.createElement('div')
  el.tabIndex = -1
  document.body.append(el)
  return el
}

beforeEach(() => {
  setActivePinia(createPinia())
})

afterEach(() => {
  document.body.replaceChildren()
})

describe('trying the message load again', () => {
  it('does nothing without an open channel', async () => {
    const chat = useChatStore()
    const fetch = vi.spyOn(chat, 'fetchMessages').mockResolvedValue(undefined)
    await useLoadRetry(() => column())()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('moves focus to the chat once the messages load', async () => {
    const chat = useChatStore()
    chat.activeChannel = channel
    const fetch = vi.spyOn(chat, 'fetchMessages').mockResolvedValue(undefined)
    const el = column()
    await useLoadRetry(() => el)()
    expect(fetch).toHaveBeenCalledWith(channel.id)
    expect(document.activeElement).toBe(el)
  })

  it('moves focus to the new retry button when the load fails again', async () => {
    const chat = useChatStore()
    chat.activeChannel = channel
    vi.spyOn(chat, 'fetchMessages').mockImplementation(async () => {
      document.body.insertAdjacentHTML('beforeend', '<div data-testid="chat-load-failed"><button data-testid="state-retry">Try again</button></div>')
    })
    await useLoadRetry(() => column())()
    expect((document.activeElement as HTMLElement | null)?.dataset.testid).toBe('state-retry')
  })

  it('leaves focus alone when the chat has gone', async () => {
    const chat = useChatStore()
    chat.activeChannel = channel
    vi.spyOn(chat, 'fetchMessages').mockResolvedValue(undefined)
    await expect(useLoadRetry(() => null)()).resolves.toBeUndefined()
    expect(document.activeElement).toBe(document.body)
  })
})
