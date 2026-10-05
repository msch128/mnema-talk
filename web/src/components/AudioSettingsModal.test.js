import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AudioSettingsModal from './AudioSettingsModal.vue'
import { useVoiceStore } from '../stores/voice'
import { AUTO_THRESHOLD } from '../lib/levelMeter'

vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({
    refreshAudioDevices: vi.fn(),
    startMicTest: vi.fn(),
    applyAudioSettings: vi.fn(),
    stopMicTest: vi.fn(),
  }),
}))

function meter(wrapper) {
  const bar = wrapper.find('.relative.h-6')
  const [fill, ...markers] = bar.findAll('div')
  return { fill, threshold: markers.at(-1) }
}

describe('AudioSettingsModal level meter', () => {
  let voice

  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    voice = useVoiceStore()
    voice.inputMode = 'activity'
    voice.autoSensitivity = false
  })

  it('shows the level visibly when it is below a high threshold', async () => {
    voice.sensitivityThreshold = 80
    const wrapper = mount(AudioSettingsModal)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill } = meter(wrapper)
    expect(fill.attributes('style')).toContain('width: 40%')
    expect(fill.classes()).toContain('bg-mnema-warning')
    expect(wrapper.text()).toContain('Aktueller Pegel: 40%')
  })

  it('turns green once the level reaches the threshold', async () => {
    voice.sensitivityThreshold = 30
    const wrapper = mount(AudioSettingsModal)
    voice.currentInputLevel = 45
    await nextTick()

    expect(meter(wrapper).fill.classes()).toContain('bg-mnema-accent')
  })

  it('places the marker at the threshold the gate uses in auto mode', async () => {
    voice.sensitivityThreshold = 80
    voice.autoSensitivity = true
    const wrapper = mount(AudioSettingsModal)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill, threshold } = meter(wrapper)
    expect(threshold.attributes('style')).toContain(`left: ${AUTO_THRESHOLD}%`)
    expect(fill.classes()).toContain('bg-mnema-accent')
    expect(wrapper.text()).toContain('Aktueller Pegel: 40%')
  })
})
