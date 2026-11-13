import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ThreadComposer from './ThreadComposer.vue'
import { useChatStore } from '../../stores/chat'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { threadLifetimeKey, useLifetimeStore } from '../../stores/lifetime'
import { setLocale } from '../../i18n'
import { channelFixture, fixtureId, messageFixture } from '../../test-fixtures.fixture'

// The thread composer has the same timer as the channel's, and a thread keeps
// its choice apart from its channel (doc/timed-messages.md). A reply sent with
// it carries lifetime_seconds.

enableAutoUnmount(afterEach)

let posts: unknown[]

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  localStorage.clear()
  posts = []
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    if (String(url).includes('/messages') && init?.method === 'POST') {
      posts.push(JSON.parse(String(init.body)))
      return new Response(JSON.stringify(messageFixture({ id: fixtureId(30), channel_id: fixtureId(5), parent_id: fixtureId(9) })), { status: 201, headers: { 'Content-Type': 'application/json' } })
    }
    return new Response(JSON.stringify({ message_lifetimes_enabled: true, message_lifetime_max_seconds: 2592000 }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
  useServerFeaturesStore().list = ['messages.lifetime']
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
  chat.activeThread = messageFixture({ id: fixtureId(9), channel_id: fixtureId(5) })
})
afterEach(() => vi.unstubAllGlobals())

describe('the thread composer timer', () => {
  it('keeps a lifetime for the thread apart from its channel and shows what it does not protect', async () => {
    const w = mount(ThreadComposer, { props: { modelValue: 'in the thread', canSend: true }, global: { directives: { tooltip: () => undefined } }, attachTo: document.body })
    await flushPromises()
    await w.get('[data-testid="lifetime-button"]').trigger('click')
    await flushPromises()
    const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => el.textContent?.trim() === '1 h')
    item!.click()
    await flushPromises()
    const lifetimes = useLifetimeStore()
    expect(lifetimes.forChannel(threadLifetimeKey(fixtureId(9)))).toBe(3600)
    expect(lifetimes.forChannel(fixtureId(5))).toBe(0)
    expect(w.get('[data-testid="lifetime-chip"]').text()).toBe('1 h')
    expect(w.get('[data-testid="lifetime-help"]').text()).toContain('Disappears for everyone after 1 h')
  })

  it('sends a reply with its lifetime', async () => {
    await useChatStore().sendThreadReply(fixtureId(9), fixtureId(5), 'short reply', null, [], 300)
    expect(posts).toEqual([expect.objectContaining({ content: 'short reply', parent_id: fixtureId(9), lifetime_seconds: 300 })])
  })
})
