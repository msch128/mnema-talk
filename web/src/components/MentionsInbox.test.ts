import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MentionsInbox from './MentionsInbox.vue'
import { useChatStore } from '../stores/chat'
import { channelFixture, fixtureId, messageFixture, readStateFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)
beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  const chat = useChatStore()
  chat.categories = [{ id: fixtureId(4), name: 'Cat', sort_order: 0, created_at: '2026-01-01T00:00:00Z', channels: [channelFixture({ id: fixtureId(2), name: 'general' })] }]
})

const mountInbox = () => mount(MentionsInbox, { global: { directives: { tooltip: () => undefined } } })

describe('MentionsInbox', () => {
  it('shows the unread mention count over all channels, capped at 99+', async () => {
    const chat = useChatStore()
    const w = mountInbox()
    expect(w.find('[data-testid="mentions-inbox-badge"]').exists()).toBe(false)
    chat.readStates = { a: readStateFixture({ channel_id: 'a', mention_count: 2 }), b: readStateFixture({ channel_id: 'b', mention_count: 3 }) }
    await flushPromises()
    expect(w.find('[data-testid="mentions-inbox-badge"]').text()).toBe('5')
    chat.readStates = { a: readStateFixture({ channel_id: 'a', mention_count: 120 }) }
    await flushPromises()
    expect(w.find('[data-testid="mentions-inbox-badge"]').text()).toBe('99+')
  })

  it('opens, lists the mentions and jumps to the chosen message', async () => {
    const msg = messageFixture({ id: fixtureId(30), content: 'hey @member', display_name: 'Anna', channel_id: fixtureId(2) })
    apiMock.mockResolvedValue({ messages: [msg], has_more: false })
    const chat = useChatStore()
    const go = vi.spyOn(chat, 'goToMessage').mockImplementation(() => undefined)
    const w = mountInbox()
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/mentions?limit=20', expect.anything())
    const item = w.find('[data-testid="mentions-inbox-item"]')
    expect(item.text()).toContain('hey @member')
    expect(item.text()).toContain('general')
    await item.trigger('click')
    expect(go).toHaveBeenCalledWith(msg)
    expect(w.find('[data-testid="mentions-inbox"]').exists()).toBe(false)
  })

  it('shows the empty and failed states, and loads more', async () => {
    apiMock.mockResolvedValueOnce({ messages: [], has_more: false })
    const w = mountInbox()
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="mentions-inbox-empty"]').exists()).toBe(true)
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')

    apiMock.mockRejectedValueOnce(new Error('offline'))
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')

    apiMock.mockResolvedValueOnce({ messages: [messageFixture({ id: fixtureId(31), parent_id: fixtureId(30) })], has_more: true })
      .mockResolvedValueOnce({ messages: [messageFixture({ id: fixtureId(32) })], has_more: false })
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')
    await flushPromises()
    await w.find('[data-testid="mentions-inbox-more"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="mentions-inbox-item"]')).toHaveLength(2)
    expect(w.find('[data-testid="mentions-inbox-more"]').exists()).toBe(false)
  })

  it('closes on a press outside', async () => {
    apiMock.mockResolvedValue({ messages: [], has_more: false })
    const w = mount(MentionsInbox, { attachTo: document.body, global: { directives: { tooltip: () => undefined } } })
    await w.find('[data-testid="mentions-inbox-button"]').trigger('click')
    expect(w.find('[data-testid="mentions-inbox"]').exists()).toBe(true)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(w.find('[data-testid="mentions-inbox"]').exists()).toBe(false)
  })
})
