import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatComposer from './ChatComposer.vue'
import LifetimeMark from './LifetimeMark.vue'
import { useChatStore } from '../../stores/chat'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { useLifetimeStore } from '../../stores/lifetime'
import { now } from '../../shared/lib/clock'
import { setLocale } from '../../i18n'
import { channelFixture, fixtureId } from '../../test-fixtures.fixture'

// The composer's timer (doc/timed-messages.md): offered only when the server
// knows lifetimes and the community allows them, a menu of presets and a
// custom value, the choice shown next to the send button and kept per
// conversation, and one line that says what a lifetime does not protect.

enableAutoUnmount(afterEach)

let settings = { message_lifetimes_enabled: true, message_lifetime_max_seconds: 2592000 }

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  localStorage.clear()
  settings = { message_lifetimes_enabled: true, message_lifetime_max_seconds: 2592000 }
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async () => new Response(JSON.stringify(settings), { status: 200, headers: { 'Content-Type': 'application/json' } })))
  useChatStore().activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
})
afterEach(() => vi.unstubAllGlobals())

const mountComposer = () => mount(ChatComposer, {
  props: { modelValue: 'see you', canSend: true },
  global: { directives: { tooltip: () => undefined } },
  attachTo: document.body
})

async function pick(label: string) {
  const item = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => el.textContent?.trim() === label)
  expect(item, `menu item ${label}`).toBeTruthy()
  item!.click()
  await flushPromises()
}

describe('the composer timer', () => {
  it('is not offered by a server without lifetimes or a community that turned them off', async () => {
    expect(mountComposer().find('[data-testid="lifetime-button"]').exists()).toBe(false)
    useServerFeaturesStore().list = ['messages.lifetime']
    settings.message_lifetimes_enabled = false
    const w = mountComposer()
    await flushPromises()
    expect(w.find('[data-testid="lifetime-button"]').exists()).toBe(false)
  })

  it('offers the presets up to the ceiling, shows the choice by the send button and keeps it for the conversation', async () => {
    useServerFeaturesStore().list = ['messages.lifetime']
    settings.message_lifetime_max_seconds = 3600
    const w = mountComposer()
    await flushPromises()
    const button = w.get('[data-testid="lifetime-button"]')
    expect(button.attributes('aria-haspopup')).toBe('menu')
    await button.trigger('click')
    await flushPromises()
    const labels = [...document.body.querySelectorAll('[role="menuitem"]')].map(el => el.textContent?.trim())
    expect(labels.slice(0, 5)).toEqual(['Off', '1 min', '5 min', '1 h', 'Custom…'])
    expect(labels.join(' ')).not.toContain('8 h')
    await pick('5 min')
    expect(w.get('[data-testid="lifetime-chip"]').text()).toBe('5 min')
    expect(w.get('[data-testid="lifetime-help"]').text()).toContain('Copies, screenshots and notifications can keep it')
    expect(useLifetimeStore().forChannel(fixtureId(5))).toBe(300)
    expect(useLifetimeStore().forChannel(fixtureId(6))).toBe(0)
    expect(JSON.parse(localStorage.getItem('mnema.lifetimes.v1') ?? '{}')).toEqual({ [fixtureId(5)]: 300 })
  })

  it('takes a custom lifetime within the bounds only', async () => {
    useServerFeaturesStore().list = ['messages.lifetime']
    const w = mountComposer()
    await flushPromises()
    await w.get('[data-testid="lifetime-button"]').trigger('click')
    await flushPromises()
    await pick('Custom…')
    const form = document.body.querySelector<HTMLFormElement>('[data-testid="lifetime-custom"]')!
    const amount = form.querySelector<HTMLInputElement>('#lifetime-amount')!
    const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!
    amount.value = '0'
    amount.dispatchEvent(new Event('input'))
    await flushPromises()
    expect(submit.disabled).toBe(true)
    amount.value = '45'
    amount.dispatchEvent(new Event('input'))
    await flushPromises()
    submit.click()
    await flushPromises()
    expect(useLifetimeStore().forChannel(fixtureId(5))).toBe(45 * 60)
  })
})

describe('the remaining-time mark', () => {
  it('counts down on the shared clock and is named for screen readers', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse('2026-11-13T10:00:00Z'))
    try {
      const w = mount(LifetimeMark, { props: { expiresAt: '2026-11-13T12:30:00Z' }, global: { directives: { tooltip: () => undefined } } })
      expect(w.text()).toBe('3 h')
      expect(w.attributes('aria-label')).toBe('Disappears in 3 h')
      vi.setSystemTime(Date.parse('2026-11-13T12:28:30Z'))
      now.value = Date.now()
      await flushPromises()
      expect(w.text()).toBe('2 min')
      vi.setSystemTime(Date.parse('2026-11-13T12:29:50Z'))
      now.value = Date.now()
      await flushPromises()
      expect(w.text()).toBe('10 s')
    } finally {
      vi.useRealTimers()
    }
  })
})
