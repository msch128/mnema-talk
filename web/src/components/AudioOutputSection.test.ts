import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AudioOutputSection from './AudioOutputSection.vue'
import { useVoiceStore } from '../stores/voice'
import { i18nPlugin } from '../i18n'

const sound = vi.hoisted(() => ({ play: vi.fn() }))
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: sound.play }))

const mountSection = () => mount(AudioOutputSection, { global: { plugins: [i18nPlugin] } })

beforeEach(() => {
  sound.play.mockReset()
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('message sounds setting', () => {
  it('offers all, mentions and direct messages, or off, defaulting to mentions', () => {
    const wrapper = mountSection()
    const select = wrapper.get('[data-testid="message-sounds"]')
    expect(select.findAll('option').map(o => o.element.value)).toEqual(['all', 'mentions', 'off'])
    expect((select.element as HTMLSelectElement).value).toBe('mentions')
  })

  it('does not play on its own, only when the preview button is pressed', async () => {
    const wrapper = mountSection()
    expect(sound.play).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="message-sounds-preview"]').trigger('click')
    expect(sound.play).toHaveBeenCalledExactlyOnceWith('message')
  })

  it('remembers the choice on this device', async () => {
    const wrapper = mountSection()
    await wrapper.get('[data-testid="message-sounds"]').setValue('all')
    expect(useVoiceStore().messageSounds).toBe('all')
    expect(localStorage.getItem('mnema_message_sounds')).toBe('all')
  })

  it('is hidden while sound effects are off', async () => {
    useVoiceStore().soundEffectsEnabled = false
    const wrapper = mountSection()
    expect(wrapper.find('[data-testid="message-sounds"]').exists()).toBe(false)
  })
})
