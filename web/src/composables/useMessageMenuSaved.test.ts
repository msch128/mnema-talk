import type { ApiOptions } from '../lib/api'
import { required } from '../store-test-support.fixture'
import { useMessageActions } from './useMessageActions'
import type { ContextMenuItem } from '../components/menuTypes'
import { channelFixture, messageFixture } from '../test-fixtures.fixture'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { useMessageMenu } from './useMessageMenu'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { pendingForward } from '../lib/forwardDialog'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const msg = messageFixture({ id: 'm1', content: 'hello world', user_id: 'me' })
const labels = (items: ContextMenuItem[]) => items.map(i => i.type === 'separator' ? '—' : i.label)

function menu() {
  const actions = {
    ...useMessageActions(), pickerId: ref<string | null>(null),
    isOwn: vi.fn(() => false), canDelete: vi.fn(() => false), startEdit: vi.fn(), deleteMessage: vi.fn(async () => {}), closePicker: vi.fn()
  }
  return useMessageMenu({ actions, reply: vi.fn() })
}

beforeEach(() => {
  setLocale('en')
  apiMock.mockReset()
  setActivePinia(createPinia())
  useChatStore().activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
})

describe('the save item of the message menu', () => {
  it('is offered only when the server has saved messages', () => {
    expect(labels(menu().itemsFor(msg))).not.toContain('Save message')
    useServerFeaturesStore().list = ['saved']
    expect(labels(menu().itemsFor(msg))).toContain('Save message')
  })

  it('saves, then offers to remove, and says what happened', async () => {
    useServerFeaturesStore().list = ['saved']
    apiMock.mockResolvedValueOnce({ message_id: 'm1', note: '', saved_at: '2026-10-12T10:00:00Z', available: true })
    const m = menu()
    const first = required(m.itemsFor(msg).find(i => i.label === 'Save message'))
    await required(first.action)()
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/saved/m1', expect.objectContaining({ method: 'PUT' }))
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'success', text: 'Saved – find it under Saved messages', action: { label: 'View' } })

    apiMock.mockResolvedValueOnce(null)
    const second = required(m.itemsFor(msg).find(i => i.label === 'Remove from saved'))
    await required(second.action)()
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/saved/m1', expect.objectContaining({ method: 'DELETE' }))
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'success', text: 'Removed from your saved messages.' })
    expect(labels(m.itemsFor(msg))).toContain('Save message')
  })

  it('tells the member when saving fails', async () => {
    useServerFeaturesStore().list = ['saved']
    apiMock.mockRejectedValueOnce('offline')
    const item = required(menu().itemsFor(msg).find(i => i.label === 'Save message'))
    await required(item.action)()
    expect(useToastStore().toasts.at(-1)).toMatchObject({ type: 'error', text: 'Couldn’t save the message. Try again.' })
  })
})

describe('the forward item of the message menu', () => {
  it('is offered only when the server can forward, and opens the dialog', async () => {
    expect(labels(menu().itemsFor(msg))).not.toContain('Forward…')
    useServerFeaturesStore().list = ['forward']
    const item = required(menu().itemsFor(msg).find(i => i.label === 'Forward…'))
    required(item.action)()
    expect(pendingForward.value).toMatchObject({ messageId: 'm1', excerpt: 'hello world' })
  })
})
