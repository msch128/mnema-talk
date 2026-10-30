import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import TalkReactButton from './TalkReactButton.vue'
import { useChatStore } from '../stores/chat'
import { useEmojiStore } from '../stores/emoji'
import { useVoiceStore } from '../stores/voice'
import { setLocale } from '../i18n'

// The allowance (5 per 10 s) is the limiter's own test; here every click goes out.
vi.mock('../lib/stage', async importOriginal => ({ ...(await importOriginal<typeof import('../lib/stage')>()), createReactionLimiter: () => () => true }))

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

  describe('what is sent', () => {
    const PARTY = '11111111-2222-3333-4444-555555555555'
    const picker = { EmojiPicker: { emits: ['pick'], template: '<button data-testid="stub-picker" @click="$emit(\'pick\', \':party:\')" />' } }

    function setup() {
      const chat = useChatStore()
      const send = vi.spyOn(chat, 'sendWSEvent').mockImplementation(() => undefined)
      useVoiceStore().currentChannelId = 'talk-1'
      useEmojiStore().items = [{ id: PARTY, name: 'party', animated: false, url: '/e/party.png' }]
      return send
    }

    it('sends every quick reaction as the emoji itself', async () => {
      const send = setup()
      const w = mount(TalkReactButton, { global: { stubs: picker } })
      const presets = ['👍', '❤️', '😂', '🔥', '🎉', '🚀']
      for (const emoji of presets) {
        await w.get('[data-testid="talk-react"]').trigger('click')
        const button = w.findAll('[data-testid="reaction-palette"] button').find(b => b.text() === emoji)
        expect(button, emoji).toBeDefined()
        await button?.trigger('click')
      }
      expect(send.mock.calls).toEqual(presets.map(emoji => ['talk_reaction', { emoji }]))
    })

    it('opens the full picker with "more" and sends a custom emoji as its token', async () => {
      const send = setup()
      const w = mount(TalkReactButton, { global: { stubs: picker } })
      await w.get('[data-testid="talk-react"]').trigger('click')
      await w.get('[data-testid="reaction-more"]').trigger('click')
      await w.get('[data-testid="stub-picker"]').trigger('click')
      expect(send).toHaveBeenCalledWith('talk_reaction', { emoji: `<:party:${PARTY}>` })
      expect(w.find(palette).exists()).toBe(false)
    })
  })
})
