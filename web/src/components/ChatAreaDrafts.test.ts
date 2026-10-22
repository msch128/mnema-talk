import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import ChatArea from './ChatArea.vue'
import ReplyComposerBar from './ReplyComposerBar.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import { channelFixture, messageFixture, onboardingStateFixture, userFixture } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)

const general = channelFixture({ id: 'ch1', name: 'allgemein', type: 'text' })
const random = channelFixture({ id: 'ch2', name: 'zufall', type: 'text' })

function open() {
  return mount(ChatArea, { attachTo: document.body })
}

const box = (view: ReturnType<typeof open>) => view.get('textarea')

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me', role: 'admin' })
  useOnboardingStore().state = onboardingStateFixture({ dismissed_tips: ['search', 'mention', 'talk'] })
  const chat = useChatStore()
  chat.activeChannel = general
  chat.members = [userFixture({ id: 'u2', username: 'anna', display_name: 'Anna' })]
  chat.messages = [messageFixture({ id: 'm1', channel_id: 'ch1', user_id: 'u2', username: 'anna', display_name: 'Anna', attachments: [], reactions: [] })]
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, { status: 204 })))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('drafts per channel', () => {
  it('keeps the unsent text of each channel while another is open and after a reload', async () => {
    const chat = useChatStore()
    const view = open()
    await box(view).setValue('half a thought')
    chat.activeChannel = random
    await nextTick()
    expect(box(view).element.value).toBe('')
    await box(view).setValue('other channel')
    chat.activeChannel = general
    await nextTick()
    expect(box(view).element.value).toBe('half a thought')

    vi.advanceTimersByTime(500)
    view.unmount()
    const reloaded = open()
    expect(box(reloaded).element.value).toBe('half a thought')
    chat.activeChannel = random
    await nextTick()
    expect(box(reloaded).element.value).toBe('other channel')
  })

  it('restores the reply target with the text', async () => {
    const chat = useChatStore()
    const view = open()
    await view.get('[data-msg-id=m1]').trigger('keydown', { key: 'r' })
    expect(view.findComponent(ReplyComposerBar).text()).toContain('Anna')
    await box(view).setValue('answer')

    chat.activeChannel = random
    await nextTick()
    expect(view.findComponent(ReplyComposerBar).exists()).toBe(false)

    chat.activeChannel = general
    await nextTick()
    expect(box(view).element.value).toBe('answer')
    expect(view.findComponent(ReplyComposerBar).text()).toContain('Anna')
  })
})
