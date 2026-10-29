import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import TalkReactButton from './TalkReactButton.vue'
import { setLocale } from '../i18n'

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => vi.useRealTimers())

describe('TalkReactButton', () => {
  const palette = '[data-testid="reaction-palette"]'
  const stub = { ReactionPalette: { template: '<div data-testid="reaction-palette"><input /></div>' } }

  it('closes the palette after a quiet moment', async () => {
    const w = mount(TalkReactButton, { global: { stubs: stub } })
    await w.get('[data-testid="talk-react"]').trigger('click')
    expect(w.find(palette).exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(8100)
    expect(w.find(palette).exists()).toBe(false)
  })

  it('stays open while the member is typing or moving inside it', async () => {
    const w = mount(TalkReactButton, { global: { stubs: stub } })
    await w.get('[data-testid="talk-react"]').trigger('click')
    await vi.advanceTimersByTimeAsync(7000)
    await w.get('input').trigger('keydown', { key: 'a' })
    await vi.advanceTimersByTimeAsync(7000)
    expect(w.find(palette).exists()).toBe(true)
    await w.get('input').trigger('pointermove')
    await vi.advanceTimersByTimeAsync(7000)
    expect(w.find(palette).exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(1500)
    expect(w.find(palette).exists()).toBe(false)
  })
})
