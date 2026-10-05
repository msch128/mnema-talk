import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AudioSettingsModal from './AudioSettingsModal.vue'
import { useVoiceStore } from '../stores/voice'
import { AUTO_THRESHOLD } from '../lib/levelMeter'
import { i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

const mockToggleMicTest = vi.fn()
const mockApplyAudioSettings = vi.fn()
const mockRefreshAudioDevices = vi.fn()
const mockStartMicTest = vi.fn()
const mockStopMicTest = vi.fn()

vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({
    refreshAudioDevices: mockRefreshAudioDevices,
    startMicTest: mockStartMicTest,
    applyAudioSettings: mockApplyAudioSettings,
    stopMicTest: mockStopMicTest,
    toggleMicTest: mockToggleMicTest,
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
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    voice = useVoiceStore()
    voice.inputMode = 'activity'
    voice.autoSensitivity = false
  })

  it('shows the level visibly when it is below a high threshold', async () => {
    voice.sensitivityThreshold = 80
    const wrapper = mount(AudioSettingsModal, mountOpts)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill } = meter(wrapper)
    expect(fill.attributes('style')).toContain('width: 40%')
    expect(fill.classes()).toContain('bg-mnema-warning')
    expect(wrapper.text()).toContain('Pegel 40 %')
  })

  it('turns green once the level reaches the threshold', async () => {
    voice.sensitivityThreshold = 30
    const wrapper = mount(AudioSettingsModal, mountOpts)
    voice.currentInputLevel = 45
    await nextTick()

    expect(meter(wrapper).fill.classes()).toContain('bg-mnema-accent')
  })

  it('places the marker at the threshold the gate uses in auto mode', async () => {
    voice.sensitivityThreshold = 80
    voice.autoSensitivity = true
    const wrapper = mount(AudioSettingsModal, mountOpts)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill, threshold } = meter(wrapper)
    expect(threshold.attributes('style')).toContain(`left: ${AUTO_THRESHOLD}%`)
    expect(fill.classes()).toContain('bg-mnema-accent')
    expect(wrapper.text()).toContain('Pegel 40 %')
  })
})

describe('AudioSettingsModal mic test and extra settings', () => {
  let voice

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    voice = useVoiceStore()
  })

  it('triggers toggleMicTest when clicking mic test button', async () => {
    const wrapper = mount(AudioSettingsModal, mountOpts)
    expect(wrapper.text()).toContain('Mikrofon testen')

    const btn = wrapper.findAll('button').find(b => b.text().includes('Mikrofon testen'))
    expect(btn).toBeDefined()
    await btn.trigger('click')

    expect(mockToggleMicTest).toHaveBeenCalledTimes(1)

    voice.isMicTesting = true
    await nextTick()
    expect(wrapper.text()).toContain('Test beenden')
    expect(wrapper.text()).toContain('Aktiv (stumm für andere)')
  })

  it('toggles QoS high priority and applies settings', async () => {
    const wrapper = mount(AudioSettingsModal, mountOpts)
    const qosBtn = wrapper.find('button[aria-label="Quality of Service (Hohe Paketpriorität)"]')
    expect(qosBtn.exists()).toBe(true)
    expect(qosBtn.attributes('aria-checked')).toBe('true')
    expect(voice.qosHighPriority).toBe(true)

    await qosBtn.trigger('click')
    expect(voice.qosHighPriority).toBe(false)
    expect(mockApplyAudioSettings).toHaveBeenCalled()
    expect(qosBtn.attributes('aria-checked')).toBe('false')
  })

  it('toggles warning switches and persists to voiceStore', async () => {
    const wrapper = mount(AudioSettingsModal, mountOpts)
    const noAudioBtn = wrapper.find('button[aria-label="Warnung bei fehlendem Tonsignal"]')
    const switchChBtn = wrapper.find('button[aria-label="Bestätigung beim Kanalwechsel"]')

    expect(noAudioBtn.exists()).toBe(true)
    expect(switchChBtn.exists()).toBe(true)

    expect(voice.warnNoAudioDetected).toBe(true)
    expect(voice.warnSwitchChannel).toBe(true)

    await noAudioBtn.trigger('click')
    expect(voice.warnNoAudioDetected).toBe(false)

    await switchChBtn.trigger('click')
    expect(voice.warnSwitchChannel).toBe(false)
  })

  it('toggles sound effects and updates volume slider', async () => {
    const wrapper = mount(AudioSettingsModal, mountOpts)
    const soundsBtn = wrapper.find('button[aria-label="Soundeffekte"]')
    expect(soundsBtn.exists()).toBe(true)
    expect(voice.soundEffectsEnabled).toBe(true)

    // Volume slider is visible when sound effects enabled
    const slider = wrapper.find('input[aria-label="Lautstärke der Soundeffekte"]')
    expect(slider.exists()).toBe(true)

    await slider.setValue(50)
    expect(voice.soundEffectsVolume).toBe(50)

    await soundsBtn.trigger('click')
    expect(voice.soundEffectsEnabled).toBe(false)
    await nextTick()
    expect(wrapper.find('input[aria-label="Lautstärke der Soundeffekte"]').exists()).toBe(false)
  })

  it('allows toggling individual sound events granularly', async () => {
    const wrapper = mount(AudioSettingsModal, mountOpts)
    expect(wrapper.text()).toContain('Einzelne Töne')

    const muteSoundBtn = wrapper.find('button[aria-label="Stummschalten"]')
    expect(muteSoundBtn.exists()).toBe(true)
    expect(muteSoundBtn.attributes('aria-checked')).toBe('true')
    expect(voice.soundEvents.mute).toBe(true)

    await muteSoundBtn.trigger('click')
    expect(voice.soundEvents.mute).toBe(false)
    expect(muteSoundBtn.attributes('aria-checked')).toBe('false')

    await muteSoundBtn.trigger('click')
    expect(voice.soundEvents.mute).toBe(true)
    expect(muteSoundBtn.attributes('aria-checked')).toBe('true')
  })
})

describe('AudioSettingsModal mic test lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
  })

  function deferred() {
    let resolve
    const promise = new Promise(r => { resolve = r })
    return { promise, resolve }
  }

  it('never starts the mic test when closed while devices are loading', async () => {
    const devices = deferred()
    mockRefreshAudioDevices.mockReturnValueOnce(devices.promise)
    const wrapper = mount(AudioSettingsModal, mountOpts)
    wrapper.unmount()
    devices.resolve()
    await devices.promise
    await nextTick()
    expect(mockStartMicTest).not.toHaveBeenCalled()
  })

  it('stops the mic test again when closed while it was starting', async () => {
    const started = deferred()
    mockStartMicTest.mockReturnValueOnce(started.promise)
    const wrapper = mount(AudioSettingsModal, mountOpts)
    await vi.waitFor(() => expect(mockStartMicTest).toHaveBeenCalledTimes(1))
    wrapper.unmount()
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
    started.resolve()
    await started.promise
    await nextTick()
    expect(mockStopMicTest).toHaveBeenCalledTimes(2)
  })
})
