import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PinnedMessages from './PinnedMessages.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { t } from '../i18n'
import { channelFixture, fixtureId, messageFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)
const channel = channelFixture({ id: fixtureId(2), name: 'general' })

beforeEach(() => {
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
  useChatStore().activeChannel = channel
})

const mountPins = () => mount(PinnedMessages, { global: { directives: { tooltip: () => undefined } } })
const pinned = (id: number, content: string) => messageFixture({ id: fixtureId(id), content, is_pinned: true })

describe('PinnedMessages', () => {
  it('lists the pinned messages and jumps to the chosen one', async () => {
    const msg = pinned(30, '**Rules** are in the topic')
    apiMock.mockResolvedValue([msg])
    const chat = useChatStore()
    const go = vi.spyOn(chat, 'goToMessage').mockImplementation(() => undefined)
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${channel.id}/pins`, expect.anything())
    const item = w.find('[data-testid="pinned-item"]')
    expect(item.text()).toContain('Rules are in the topic')
    await item.trigger('click')
    expect(go).toHaveBeenCalledWith(msg)
    expect(w.find('[data-testid="pinned-panel"]').exists()).toBe(false)
  })

  it('says so when nothing is pinned and when loading fails', async () => {
    apiMock.mockResolvedValueOnce([])
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="pinned-empty"]').exists()).toBe(true)
    await w.find('[data-testid="pinned-button"]').trigger('click')
    apiMock.mockRejectedValueOnce(new Error('offline'))
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('refetches an open list when the server reports a pin change', async () => {
    apiMock.mockResolvedValueOnce([pinned(30, 'one')])
    const chat = useChatStore()
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    apiMock.mockResolvedValueOnce([pinned(31, 'two'), pinned(30, 'one')])
    chat.handleWSEvent({ type: 'pins_changed', payload: { channel_id: channel.id, message_id: fixtureId(31), pinned: true, actor_id: fixtureId(1), actor_name: 'Mod' } })
    await flushPromises()
    expect(w.findAll('[data-testid="pinned-item"]')).toHaveLength(2)
    // A change in another channel leaves it alone.
    const calls = apiMock.mock.calls.length
    chat.handleWSEvent({ type: 'pins_changed', payload: { channel_id: fixtureId(9), message_id: fixtureId(32), pinned: true, actor_id: fixtureId(1), actor_name: 'Mod' } })
    await flushPromises()
    expect(apiMock.mock.calls.length).toBe(calls)
  })

  it('offers unpinning only to members who manage messages', async () => {
    apiMock.mockResolvedValue([pinned(30, 'one')])
    const permissions = usePermissionsStore()
    const can = vi.spyOn(permissions, 'canIn').mockReturnValue(false)
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="pinned-unpin"]').exists()).toBe(false)
    w.unmount()

    can.mockReturnValue(true)
    const chat = useChatStore()
    const unpin = vi.spyOn(chat, 'setPinned').mockResolvedValue(pinned(30, 'one'))
    const moderator = mountPins()
    await moderator.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    await moderator.find('[data-testid="pinned-unpin"]').trigger('click')
    await flushPromises()
    expect(unpin).toHaveBeenCalledWith(channel.id, fixtureId(30), false)
    expect(moderator.find('[data-testid="pinned-item"]').exists()).toBe(false)
  })

  it('shows an attachment-only pin, keeps the row when unpinning fails and resets on another channel', async () => {
    const withFile = messageFixture({ id: fixtureId(40), content: '', is_pinned: true, attachments: [{ id: fixtureId(41), original_filename: 'a.png', mime_type: 'image/png', size_bytes: 1, url: '/api/media/x', is_deleted: false }] })
    apiMock.mockResolvedValue([withFile])
    vi.spyOn(usePermissionsStore(), 'canIn').mockReturnValue(true)
    const chat = useChatStore()
    vi.spyOn(chat, 'setPinned').mockRejectedValue(new Error('nope'))
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="pinned-item"]').text()).toContain(t('chat.attachment'))
    await w.find('[data-testid="pinned-unpin"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="pinned-item"]').exists()).toBe(true)
    chat.activeChannel = channelFixture({ id: fixtureId(7), name: 'other' })
    await flushPromises()
    expect(w.find('[data-testid="pinned-panel"]').exists()).toBe(false)
  })

  it('leaves a closed list alone when the server reports a change', async () => {
    const chat = useChatStore()
    mountPins()
    chat.handleWSEvent({ type: 'pins_changed', payload: { channel_id: channel.id, message_id: fixtureId(31), pinned: false, actor_id: fixtureId(1), actor_name: 'Mod' } })
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()
  })
})
