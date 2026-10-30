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
import { closeLater, pendingLater } from '../lib/laterDialog'
import { setLocale } from '../i18n'

const msg = messageFixture({ id: 'm1', content: '**call** mum', user_id: 'someone' })
const labels = (items: ContextMenuItem[]) => items.map(i => i.type === 'separator' ? '—' : i.label)

function menu() {
  const actions = {
    ...useMessageActions(), pickerId: ref<string | null>(null),
    isOwn: vi.fn(() => false), canDelete: vi.fn(() => false), startEdit: vi.fn(), deleteMessage: vi.fn(async () => true), closePicker: vi.fn()
  }
  return useMessageMenu({ actions, reply: vi.fn() })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  closeLater()
  useChatStore().activeChannel = channelFixture({ id: 'ch1', name: 'general', type: 'text' })
})

describe('the Remind me item of the message menu', () => {
  it('is offered only when the server has reminders', () => {
    expect(labels(menu().itemsFor(msg))).not.toContain('Remind me')
    useServerFeaturesStore().list = ['reminders']
    expect(labels(menu().itemsFor(msg))).toContain('Remind me')
  })

  it('opens the time picker for that message with a plain-text excerpt', () => {
    useServerFeaturesStore().list = ['reminders']
    const item = required(menu().itemsFor(msg).find(i => i.label === 'Remind me'))
    void required(item.action)()
    expect(pendingLater.value).toEqual({ kind: 'remind', messageId: 'm1', excerpt: 'call mum' })
  })
})
