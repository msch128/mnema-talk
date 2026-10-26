import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatComposer from './ChatComposer.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { closeLater, pendingLater } from '../lib/laterDialog'
import { setLocale } from '../i18n'
import { channelFixture, fixtureId } from '../test-fixtures.fixture'

const compact = vi.hoisted(() => ({ value: false }))
vi.mock('../composables/useCompactLayout', async () => {
  const { ref: makeRef } = await import('vue')
  const isCompact = makeRef(false)
  return { useCompactLayout: () => { isCompact.value = compact.value; return { isCompact, navOpen: makeRef(false), openNav: () => undefined, closeNav: () => undefined } } }
})

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  closeLater()
  compact.value = false
  useChatStore().activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
})

const mountComposer = (props: Record<string, unknown> = {}) => mount(ChatComposer, {
  props: { modelValue: 'good morning', canSend: true, canAttach: true, ...props },
  global: { directives: { tooltip: () => undefined } },
  attachTo: document.body
})

const menuLabels = () => [...document.body.querySelectorAll('[role="menuitem"]')].map(el => el.textContent?.trim())

describe('the composer on a wide screen', () => {
  it('keeps the file, poll and send-later buttons of their own', () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    const w = mountComposer()
    expect(w.find('[data-testid="poll-button"]').exists()).toBe(true)
    expect(w.find('[data-testid="send-later-button"]').exists()).toBe(true)
    expect(w.find('[data-testid="composer-plus"]').attributes('aria-haspopup')).toBeUndefined()
  })

  it('opens the file chooser from the plus button', async () => {
    const w = mountComposer()
    const click = vi.spyOn(w.get('input[type="file"]').element as HTMLInputElement, 'click').mockImplementation(() => undefined)
    await w.get('[data-testid="composer-plus"]').trigger('click')
    expect(click).toHaveBeenCalledOnce()
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
  })
})

describe('the composer on a phone', () => {
  beforeEach(() => { compact.value = true })

  it('puts file, poll and send later behind one plus button', async () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    const w = mountComposer()
    expect(w.find('[data-testid="poll-button"]').exists()).toBe(false)
    expect(w.find('[data-testid="send-later-button"]').exists()).toBe(false)
    const plus = w.get('[data-testid="composer-plus"]')
    expect(plus.attributes('aria-haspopup')).toBe('menu')
    await plus.trigger('click')
    await flushPromises()
    expect(menuLabels()).toEqual(['Upload a file or image', 'Create a poll', 'Send later'])
    expect(plus.attributes('aria-expanded')).toBe('true')
  })

  it('opens the poll dialog and the time picker from the menu', async () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    const w = mountComposer()
    await w.get('[data-testid="composer-plus"]').trigger('click')
    await flushPromises()
    const items = [...document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')]
    items[2]?.click()
    await flushPromises()
    expect(pendingLater.value).toMatchObject({ kind: 'schedule', content: 'good morning' })
    await w.get('[data-testid="composer-plus"]').trigger('click')
    await flushPromises()
    document.body.querySelectorAll<HTMLElement>('[role="menuitem"]')[1]?.click()
    await flushPromises()
    expect(w.findComponent({ name: 'PollDialog' }).exists()).toBe(true)
  })

  it('offers only what the member may do, and disables send later without text', async () => {
    useServerFeaturesStore().list = ['messages.scheduled']
    const w = mountComposer({ canAttach: false, modelValue: '' })
    await w.get('[data-testid="composer-plus"]').trigger('click')
    await flushPromises()
    expect(menuLabels()).toEqual(['Create a poll', 'Send later'])
    expect(document.body.querySelectorAll('[role="menuitem"]')[1]?.getAttribute('aria-disabled')).toBe('true')
  })

  it('has no plus button when the member can neither attach nor send', () => {
    expect(mountComposer({ canAttach: false, canSend: false }).find('[data-testid="composer-plus"]').exists()).toBe(false)
  })
})

describe('the composer focus', () => {
  it('shows the field focus on its border, not as a ring', () => {
    const w = mountComposer()
    const frame = w.get('textarea').element.parentElement
    expect(frame?.className).toContain('field-focus-within')
    expect(frame?.className).not.toContain('ring-')
  })
})
