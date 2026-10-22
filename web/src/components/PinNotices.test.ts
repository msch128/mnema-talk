import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import PinNotices from './PinNotices.vue'
import { useChatStore } from '../stores/chat'
import { fixtureId } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

const pin = (chat: ReturnType<typeof useChatStore>, message: number, pinned = true, channel = 2) =>
  chat.handleWSEvent({ type: 'pins_changed', payload: { channel_id: fixtureId(channel), message_id: fixtureId(message), pinned, actor_id: fixtureId(1), actor_name: 'Mod' } })

describe('PinNotices', () => {
  it('notes a new pin of this channel and drops the note when it is unpinned', async () => {
    const chat = useChatStore()
    const w = mount(PinNotices, { props: { channelId: fixtureId(2) } })
    expect(w.find('[data-testid="pin-notices"]').exists()).toBe(false)
    pin(chat, 30)
    pin(chat, 31, true, 5)
    await w.vm.$nextTick()
    expect(w.findAll('[role="status"]')).toHaveLength(1)
    expect(w.text()).toContain('Mod')
    pin(chat, 30, false)
    await w.vm.$nextTick()
    expect(w.find('[data-testid="pin-notices"]').exists()).toBe(false)
  })

  it('keeps only the latest few notes and lets the member dismiss one', async () => {
    const chat = useChatStore()
    const w = mount(PinNotices, { props: { channelId: fixtureId(2) } })
    for (const n of [30, 31, 32, 33]) pin(chat, n)
    await w.vm.$nextTick()
    expect(w.findAll('[role="status"]')).toHaveLength(3)
    await w.find('button[aria-label]').trigger('click')
    expect(w.findAll('[role="status"]')).toHaveLength(2)
  })

  it('jumps to the pinned message', async () => {
    const chat = useChatStore()
    const jump = vi.spyOn(chat, 'jumpToMessage').mockResolvedValue(true)
    const w = mount(PinNotices, { props: { channelId: fixtureId(2) } })
    pin(chat, 30)
    await w.vm.$nextTick()
    await w.findAll('button')[0]!.trigger('click')
    expect(jump).toHaveBeenCalledWith(fixtureId(30))
    expect(chat.pinNotices).toHaveLength(0)
  })
})
