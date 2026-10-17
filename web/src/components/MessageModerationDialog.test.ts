import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import MessageModerationDialog from './MessageModerationDialog.vue'
import BaseDialog from './BaseDialog.vue'
import { setLocale } from '../i18n'
import { moderateMessages, pendingMessageModeration } from '../lib/messageModeration'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

const submit = (w: ReturnType<typeof mount>) => w.get('[data-testid="message-moderation-submit"]')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  pendingMessageModeration.value = null
})

describe('lib/messageModeration', () => {
  it('opens one dialog at a time; a new request cancels the old one', async () => {
    const first = moderateMessages({ kind: 'delete', channelId: 'c', messageId: 'm', excerpt: '' })
    const second = moderateMessages({ kind: 'bulk', channelId: 'c', userId: 'u', name: 'U' })
    expect(await first).toBe(false)
    pendingMessageModeration.value?.resolve(true)
    expect(await second).toBe(true)
    expect(pendingMessageModeration.value).toBeNull()
    const old = moderateMessages({ kind: 'delete', channelId: 'c', messageId: 'm', excerpt: '' })
    const stale = pendingMessageModeration.value
    void moderateMessages({ kind: 'delete', channelId: 'c', messageId: 'n', excerpt: '' })
    stale?.resolve(false)
    await old
    expect(pendingMessageModeration.value?.request).toMatchObject({ messageId: 'n' })
  })
})

describe('MessageModerationDialog', () => {
  it('renders nothing until asked', () => {
    expect(mount(MessageModerationDialog).find('form').exists()).toBe(false)
  })

  it('deletes a message with the reason', async () => {
    const chat = useChatStore()
    const remove = vi.spyOn(chat, 'deleteMessage').mockResolvedValue()
    const w = mount(MessageModerationDialog)
    const done = moderateMessages({ kind: 'delete', channelId: 'c1', messageId: 'm1', excerpt: 'rude words' })
    await nextTick()
    expect(w.text()).toContain('Delete message')
    expect(w.text()).toContain('rude words')
    expect(w.find('[data-testid="bulk-count"]').exists()).toBe(false)
    await w.get('[data-testid="reason"]').setValue('  insult ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith('c1', 'm1', 'insult')
    expect(await done).toBe(true)
  })

  it('deletes the latest messages of a member and says how many went', async () => {
    apiMock.mockResolvedValue({ deleted: 3 })
    const w = mount(MessageModerationDialog)
    const done = moderateMessages({ kind: 'bulk', channelId: 'c1', userId: 'u1', name: 'Troll' })
    await nextTick()
    expect(w.text()).toContain('Delete the latest messages of Troll')
    expect((w.get('[data-testid="bulk-count"]').element as HTMLInputElement).value).toBe('10')
    await w.get('[data-testid="bulk-count"]').setValue('3')
    await w.get('[data-testid="reason"]').setValue('flooding')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/channels/c1/messages/bulk-delete', { method: 'POST', json: { user_id: 'u1', count: 3, reason: 'flooding' } })
    expect(useToastStore().toasts.map(t => t.text)).toContain('3 messages from Troll deleted.')
    expect(await done).toBe(true)
  })

  it('tolerates an answer without a count, and validates the range', async () => {
    apiMock.mockResolvedValue(null)
    const w = mount(MessageModerationDialog)
    void moderateMessages({ kind: 'bulk', channelId: 'c1', userId: 'u1', name: 'Troll' })
    await nextTick()
    for (const bad of ['0', '101', '1.5', '']) {
      await w.get('[data-testid="bulk-count"]').setValue(bad)
      expect(submit(w).attributes('disabled')).toBeDefined()
    }
    expect(w.text()).toContain('Between 1 and 100.')
    await w.get('form').trigger('submit')
    expect(apiMock).not.toHaveBeenCalled()
    await w.get('[data-testid="bulk-count"]').setValue('5')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('0 messages from Troll deleted.')
  })

  it('shows a failure and stays open; cancel and unmount resolve false', async () => {
    const chat = useChatStore()
    vi.spyOn(chat, 'deleteMessage').mockRejectedValue(new Error('denied'))
    const w = mount(MessageModerationDialog)
    const done = moderateMessages({ kind: 'delete', channelId: 'c1', messageId: 'm1', excerpt: '' })
    await nextTick()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('denied')
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(await done).toBe(false)

    const dismissed = moderateMessages({ kind: 'delete', channelId: 'c1', messageId: 'm3', excerpt: '' })
    await nextTick()
    w.getComponent(BaseDialog).vm.$emit('close')
    expect(await dismissed).toBe(false)

    const pending = moderateMessages({ kind: 'delete', channelId: 'c1', messageId: 'm2', excerpt: '' })
    await nextTick()
    w.unmount()
    expect(await pending).toBe(false)
  })
})
