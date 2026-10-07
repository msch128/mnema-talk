

import { userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import ScreenViewers from './ScreenViewers.vue'

function seed(viewers = ["00000000-0000-4000-8000-0000000003f0", "00000000-0000-4000-8000-0000000003f2", "00000000-0000-4000-8000-0000000003f5"]) {
  const voice = useVoiceStore()
  voice.setChannel("00000000-0000-4000-8000-0000000003f3")
  voice.channelUsers = { "00000000-0000-4000-8000-0000000003f3": {
    "00000000-0000-4000-8000-0000000003ef": voiceUserFixture({ id: "00000000-0000-4000-8000-0000000003ef", username: 'anna', display_name: 'Anna' }),
    "00000000-0000-4000-8000-0000000003f0": voiceUserFixture({ id: "00000000-0000-4000-8000-0000000003f0", username: 'ben', display_name: 'Ben' }),
    "00000000-0000-4000-8000-0000000003f2": voiceUserFixture({ id: "00000000-0000-4000-8000-0000000003f2", username: 'carl', display_name: '' })
  } }
  // Someone who is not (any more) in the room's list still has a name.
  useChatStore().members = [userFixture({ id: "00000000-0000-4000-8000-0000000003f5", username: 'dora', display_name: 'Dora' })]
  voice.handleScreenViewers({ channel_id: "00000000-0000-4000-8000-0000000003f3", user_id: "00000000-0000-4000-8000-0000000003ef", viewers })
  return voice
}

const mountViewers = (props = {}) => mount(ScreenViewers, { props: { userId: "00000000-0000-4000-8000-0000000003ef", ...props }, attachTo: document.body })

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})
afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('ScreenViewers', () => {
  it('shows the count with an accessible list of names', () => {
    seed()
    const w = mountViewers()
    const button = w.get('[data-testid="screen-viewers-button"]')
    expect(button.text()).toBe('3')
    expect(button.find('svg')!.exists()).toBe(true)
    expect(button.attributes('aria-label')).toBe('3 viewers: Ben, carl, Dora')
    expect(button.attributes('aria-expanded')).toBe('false')
  })

  it('uses the singular for one viewer and German when switched', () => {
    seed(["00000000-0000-4000-8000-0000000003f0"])
    const w = mountViewers()
    expect(w.get('button').attributes('aria-label')).toBe('1 viewer: Ben')
    setLocale('de')
    return nextTick().then(() => {
      expect(w.get('button').attributes('aria-label')).toBe('1 Zuschauer: Ben')
    })
  })

  it('is hidden without viewers unless zero should show (my own share)', () => {
    seed([])
    expect(mountViewers().find('[data-testid="screen-viewers"]')!.exists()).toBe(false)
    const own = mountViewers({ showZero: true })
    expect(own.get('button').text()).toBe('0')
    expect(own.get('button').attributes('aria-label')).toBe('Nobody is watching yet')
  })

  it('a click pins the list with avatars and names, Escape closes it', async () => {
    seed()
    const w = mountViewers()
    await w.get('button').trigger('click')
    const list = w.get('[data-testid="screen-viewers-list"]')
    expect(list.text()).toContain('3 viewers')
    expect(list.findAll('[data-viewer] span.truncate').map(s => s.text())).toEqual(['Ben', 'carl', 'Dora'])
    expect(list.findAll('[data-viewer]').map(li => li.attributes('data-viewer'))).toEqual(["00000000-0000-4000-8000-0000000003f0", "00000000-0000-4000-8000-0000000003f2", "00000000-0000-4000-8000-0000000003f5"])
    expect(w.get('button').attributes('aria-expanded')).toBe('true')
    expect(w.get('button').attributes('aria-controls')).toBe(list.attributes('id'))

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(w.find('[data-testid="screen-viewers-list"]')!.exists()).toBe(false)
    expect(document.activeElement).toBe(w.get('button').element)
  })

  it('a second click or a press outside closes the pinned list', async () => {
    seed()
    const w = mountViewers()
    await w.get('button').trigger('click')
    await w.get('button').trigger('click')
    expect(w.find('[data-testid="screen-viewers-list"]')!.exists()).toBe(false)

    await w.get('button').trigger('click')
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(w.find('[data-testid="screen-viewers-list"]')!.exists()).toBe(false)
  })

  it('hovering shows the list, leaving hides it', async () => {
    vi.useFakeTimers()
    seed()
    const w = mountViewers()
    await w.trigger('mouseenter')
    vi.advanceTimersByTime(200)
    await nextTick()
    expect(w.find('[data-testid="screen-viewers-list"]')!.exists()).toBe(true)
    await w.trigger('mouseleave')
    expect(w.find('[data-testid="screen-viewers-list"]')!.exists()).toBe(false)
  })

  it('follows the store: the count changes and the list empties when the share ends', async () => {
    const voice = seed()
    const w = mountViewers()
    voice.handleScreenViewers({ channel_id: "00000000-0000-4000-8000-0000000003f3", user_id: "00000000-0000-4000-8000-0000000003ef", viewers: ["00000000-0000-4000-8000-0000000003f0"] })
    await nextTick()
    expect(w.get('button').text()).toBe('1')
    voice.handleScreenViewers({ channel_id: "00000000-0000-4000-8000-0000000003f3", user_id: "00000000-0000-4000-8000-0000000003ef", viewers: [] })
    await nextTick()
    expect(w.find('[data-testid="screen-viewers"]')!.exists()).toBe(false)
  })

  it('does not let a click reach the stage or card behind it', async () => {
    seed()
    const outer = vi.fn()
    const w = mount({
      components: { ScreenViewers },
      template: '<div @click="outer" @dblclick="outer"><ScreenViewers user-id="00000000-0000-4000-8000-0000000003ef" /></div>',
      methods: { outer }
    }, { attachTo: document.body })
    await w.get('button').trigger('click')
    await w.get('button').trigger('dblclick')
    expect(outer).not.toHaveBeenCalled()
  })
})

describe('ScreenViewers identity fallback', () => {
  it('shows unknown viewer when room is absent and uses card controls', async () => {
    const voice = seed(['unknown']); voice.channelUsers = {}; voice.currentChannelId = null
    // Use already retained viewer identity after navigating away from the room.
    const w = mountViewers({ variant: 'card', showZero: true })
    const button = w.get('button'); expect(button.classes()).toContain('h-7')
    await button.trigger('click'); expect(w.text()).toContain('?')
    w.unmount()
  })
})
