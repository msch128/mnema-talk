import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatComposer from './ChatComposer.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { closeLater, pendingLater } from '../lib/laterDialog'
import { setLocale } from '../i18n'
import { channelFixture, fixtureId } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  closeLater()
  useChatStore().activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
})

const mountComposer = (props: Record<string, unknown> = {}) => mount(ChatComposer, {
  props: { modelValue: 'good morning', canSend: true, ...props },
  global: { directives: { tooltip: () => undefined } },
  attachTo: document.body
})

describe('Send later next to the send button', () => {
  it('is not offered when the server has no scheduled messages', () => {
    expect(mountComposer().find('[data-testid="send-later-button"]').exists()).toBe(false)
  })

  it('is not offered to a member who cannot send', () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    expect(mountComposer({ canSend: false }).find('[data-testid="send-later-button"]').exists()).toBe(false)
  })

  it('stays disabled while there is nothing to send', () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    expect(mountComposer({ modelValue: '  ' }).find('[data-testid="send-later-button"]').attributes('disabled')).toBeDefined()
  })

  it('opens the time picker with the composed text and the reply target, and empties the composer once scheduled', async () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    const w = mountComposer({ replyingTo: { id: fixtureId(9), username: 'bob' } })
    await w.find('[data-testid="send-later-button"]').trigger('click')
    await flushPromises()
    const item = document.body.querySelector<HTMLElement>('[role="menuitem"]')
    expect(item?.textContent).toContain('Send later')
    item?.click()
    await flushPromises()
    expect(pendingLater.value).toMatchObject({ kind: 'schedule', channelId: fixtureId(5), content: 'good morning', replyToId: fixtureId(9) })

    if (pendingLater.value?.kind === 'schedule') pendingLater.value.onScheduled?.()
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([''])
    expect(w.emitted('cancel-reply')).toHaveLength(1)
  })
})
