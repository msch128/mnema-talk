import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PinnedMessages from './PinnedMessages.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { dateTime, dayLabel } from '../lib/time'
import { locale, t } from '../i18n'
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

  it('offers to try again after a failed load', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    apiMock.mockResolvedValueOnce([pinned(30, 'back again')])
    await w.get('[data-testid="state-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[role="alert"]').exists()).toBe(false)
    expect(w.get('[data-testid="pinned-item"]').text()).toContain('back again')
  })

  it('writes dates the way every other list does', async () => {
    const now = new Date().toISOString()
    apiMock.mockResolvedValue([messageFixture({ id: fixtureId(30), content: 'x', is_pinned: true, created_at: now, pinned_at: now, pinned_by: { id: fixtureId(4), username: 'mod', display_name: 'Mod' } })])
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    const item = w.get('[data-testid="pinned-item"]')
    expect(item.text()).toContain(dateTime(now, locale.value))
    expect(w.get('[data-testid="pinned-by"]').text()).toContain(dayLabel(now, locale.value))
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

  it('marks polls, files with text and thread replies, and leaves plain messages unmarked', async () => {
    const file = { id: fixtureId(51), original_filename: 'a.png', mime_type: 'image/png', size_bytes: 1, url: '/api/media/x', is_deleted: false }
    const poll = { anonymous: false, closed: false, closes_at: '', multiple: false, options: [], total_voters: 0 }
    apiMock.mockResolvedValue([
      messageFixture({ id: fixtureId(50), content: 'Lunch?', is_pinned: true, poll }),
      messageFixture({ id: fixtureId(52), content: 'see picture', is_pinned: true, attachments: [file] }),
      messageFixture({ id: fixtureId(53), content: 'in a thread', is_pinned: true, parent_id: fixtureId(50) }),
      pinned(54, 'plain')
    ])
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    const marks = w.findAll('[data-testid="pinned-item"]').map(item => item.find('[data-testid="pinned-markers"]').exists() ? item.find('[data-testid="pinned-markers"]').text() : '')
    expect(marks).toEqual([t('pins.poll'), t('chat.attachment'), t('chat.inThread'), ''])
  })

  it('says who pinned a message and when, and stays quiet when the server does not say', async () => {
    apiMock.mockResolvedValue([
      messageFixture({ id: fixtureId(60), content: 'new', is_pinned: true, pinned_at: '2026-10-12T10:00:00Z', pinned_by: { id: fixtureId(1), username: 'mod', display_name: 'Mona' } }),
      messageFixture({ id: fixtureId(61), content: 'gone pinner', is_pinned: true, pinned_at: '2026-10-12T10:00:00Z' }),
      messageFixture({ id: fixtureId(62), content: 'no date', is_pinned: true, pinned_by: { id: fixtureId(1), username: 'mod', display_name: '' } }),
      pinned(63, 'old server')
    ])
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    const lines = w.findAll('[data-testid="pinned-item"]').map(item => item.find('[data-testid="pinned-by"]'))
    expect(lines[0]!.text().startsWith(`${t('pins.pinnedBy', { name: 'Mona' })} · `)).toBe(true)
    expect(lines[1]!.exists()).toBe(false)
    expect(lines[2]!.text()).toBe(t('pins.pinnedBy', { name: 'mod' }))
    expect(lines[3]!.exists()).toBe(false)
  })

  it('says unpinning failed when the server gives no reason', async () => {
    apiMock.mockResolvedValue([pinned(30, 'one')])
    vi.spyOn(usePermissionsStore(), 'canIn').mockReturnValue(true)
    vi.spyOn(useChatStore(), 'setPinned').mockRejectedValue(null)
    const w = mountPins()
    await w.find('[data-testid="pinned-button"]').trigger('click')
    await flushPromises()
    await w.find('[data-testid="pinned-unpin"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('pins.unpinFailed'))
  })

  it('leaves a closed list alone when the server reports a change', async () => {
    const chat = useChatStore()
    mountPins()
    chat.handleWSEvent({ type: 'pins_changed', payload: { channel_id: channel.id, message_id: fixtureId(31), pinned: false, actor_id: fixtureId(1), actor_name: 'Mod' } })
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()
  })
})
