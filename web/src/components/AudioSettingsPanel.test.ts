import { requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import AudioSettingsPanel from './AudioSettingsPanel.vue'
import { useVoiceStore } from '../stores/voice'
import { AUTO_THRESHOLD } from '../lib/levelMeter'
import { i18nPlugin, t } from '../i18n'
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

function meter(wrapper: ReturnType<typeof mount<typeof AudioSettingsPanel>>) {
  const bar = wrapper.find('[role="meter"]')
  const [fill, ...markers] = bar.findAll('div')
  return { fill, threshold: markers.at(-1) }
}

describe('AudioSettingsPanel level meter', () => {
  let voice!: ReturnType<typeof useVoiceStore>

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
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill } = meter(wrapper)
    expect(requireValue(fill).attributes('style')).toContain('width: 40%')
    expect(requireValue(fill).classes()).toContain('bg-mnema-warning')
    expect(wrapper.text()).toContain('Pegel 40 %')
  })

  it('turns green once the level reaches the threshold', async () => {
    voice.sensitivityThreshold = 30
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    voice.currentInputLevel = 45
    await nextTick()

    expect(requireValue(meter(wrapper).fill).classes()).toContain('bg-mnema-accent')
  })

  it('places the marker at the threshold the gate uses in auto mode', async () => {
    voice.sensitivityThreshold = 80
    voice.autoSensitivity = true
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    voice.currentInputLevel = 40
    await nextTick()

    const { fill, threshold } = meter(wrapper)
    expect(requireValue(threshold).attributes('style')).toContain(`left: ${AUTO_THRESHOLD}%`)
    expect(requireValue(fill).classes()).toContain('bg-mnema-accent')
    expect(wrapper.text()).toContain('Pegel 40 %')
  })
})

describe('AudioSettingsPanel mic test and extra settings', () => {
  let voice!: ReturnType<typeof useVoiceStore>

  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
    voice = useVoiceStore()
  })

  it('triggers toggleMicTest when clicking mic test button', async () => {
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    expect(wrapper.text()).toContain('Mikrofon testen')

    const btn = wrapper.findAll('button').find(b => b.text().includes('Mikrofon testen'))
    expect(btn).toBeDefined()
    await requireValue(btn).trigger('click')

    expect(mockToggleMicTest).toHaveBeenCalledTimes(1)

    voice.isMicTesting = true
    await nextTick()
    expect(wrapper.text()).toContain('Test beenden')
    expect(wrapper.text()).toContain('Aktiv (stumm für andere)')
    await wrapper.get('[data-testid="mic-test-toggle"]').trigger('click')
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
  })

  it('toggles QoS high priority and applies settings', async () => {
    voice.localAudioStream = new MediaStream()
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const qosBtn = wrapper.find('button[aria-label="Netzwerkpriorität für Sprache und Video"]')
    expect(qosBtn.exists()).toBe(true)
    expect(qosBtn.attributes('aria-checked')).toBe('true')
    expect(voice.qosHighPriority).toBe(true)

    await qosBtn.trigger('click')
    expect(voice.qosHighPriority).toBe(false)
    expect(mockApplyAudioSettings).toHaveBeenCalled()
    expect(qosBtn.attributes('aria-checked')).toBe('false')
  })

  it('toggles warning switches and persists to voiceStore', async () => {
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const noAudioBtn = wrapper.find('button[aria-label="Warnen, wenn mein Mikrofon keinen Ton sendet"]')
    const switchChBtn = wrapper.find('button[aria-label="Bestätigung beim Talk-Wechsel"]')

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
    const wrapper = mount(AudioSettingsPanel, mountOpts)
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
    const wrapper = mount(AudioSettingsPanel, mountOpts)
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

describe('AudioSettingsPanel mic test lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    setActivePinia(createPinia())
  })

  function deferred() {
    let resolve!: (value: boolean) => void
    const promise = new Promise<boolean>(r => { resolve = r })
    return { promise, resolve }
  }

  it('lists devices without starting capture or loopback when opened', async () => {
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    await nextTick()
    expect(mockRefreshAudioDevices).toHaveBeenCalledTimes(1)
    expect(mockStartMicTest).not.toHaveBeenCalled()
    expect(mockToggleMicTest).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="mic-test-toggle"]').attributes('aria-pressed')).toBe('false')
    expect(wrapper.text()).toContain('Dieser Test bleibt lokal und wird nicht an andere übertragen.')
    wrapper.unmount()
  })

  it('saves input preferences without starting capture outside a call or test', async () => {
    const voice = useVoiceStore()
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    await wrapper.get('button[aria-label="Netzwerkpriorität für Sprache und Video"]').trigger('click')
    expect(voice.qosHighPriority).toBe(false)
    expect(mockApplyAudioSettings).not.toHaveBeenCalled()
    expect(mockStartMicTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('never starts the mic test when closed while devices are loading', async () => {
    const devices = deferred()
    mockRefreshAudioDevices.mockReturnValueOnce(devices.promise)
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    wrapper.unmount()
    devices.resolve(true)
    await devices.promise
    await nextTick()
    expect(mockStartMicTest).not.toHaveBeenCalled()
  })

  it('stops immediately when closed during test startup without late global cleanup', async () => {
    const started = deferred()
    mockToggleMicTest.mockReturnValueOnce(started.promise)
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    await wrapper.get('[data-testid="mic-test-toggle"]').trigger('click')
    expect(mockToggleMicTest).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
    started.resolve(true)
    await started.promise
    await nextTick()
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
  })

  it('does not stop a newer dialog test when an old startup finishes', async () => {
    const voice = useVoiceStore()
    const started = deferred()
    let owner = null
    mockToggleMicTest.mockImplementationOnce(() => {
      owner = 'old'
      voice.isMicTesting = true
      return started.promise
    }).mockImplementationOnce(() => {
      owner = 'new'
      voice.isMicTesting = true
      return Promise.resolve(true)
    })
    const release = () => { owner = null; voice.isMicTesting = false }
    mockStopMicTest.mockImplementationOnce(release).mockImplementationOnce(release)

    const previous = mount(AudioSettingsPanel, mountOpts)
    await previous.get('[data-testid="mic-test-toggle"]').trigger('click')
    previous.unmount()
    expect(owner).toBeNull()
    const current = mount(AudioSettingsPanel, mountOpts)
    await current.get('[data-testid="mic-test-toggle"]').trigger('click')
    expect(owner).toBe('new')
    started.resolve(false)
    await started.promise
    await nextTick()

    expect(owner).toBe('new')
    expect(voice.isMicTesting).toBe(true)
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
    current.unmount()
    expect(owner).toBeNull()
  })
})

describe('AudioSettingsPanel first check and push to talk', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.clearAllMocks() })

  it('puts the microphone, the level bar and the test first, and the jargon behind Fine-tune', () => {
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const headings = wrapper.findAll('h3').map(h => h.text())
    expect(headings[0]).toBe(t('audio.groupMeter'))
    const first = wrapper.get('section')
    expect(first.find('#audio-input-device').exists()).toBe(true)
    expect(first.find('[role="meter"]').exists()).toBe(true)
    expect(first.find('[data-testid="mic-test-toggle"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="audio-advanced"]').find('[data-testid="input-volume"]').exists()).toBe(true)
    expect(first.text()).not.toMatch(/Schwelle \d+ %/)
    wrapper.unmount()
  })

  it('confirms once the test hears the member', async () => {
    const voice = useVoiceStore(); voice.autoSensitivity = false; voice.sensitivityThreshold = 30
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    expect(wrapper.find('[data-testid="mic-heard"]').exists()).toBe(false)
    voice.isMicTesting = true; voice.currentInputLevel = 10; await nextTick()
    expect(wrapper.find('[data-testid="mic-heard"]').exists()).toBe(false)
    voice.currentInputLevel = 60; await nextTick()
    voice.currentInputLevel = 0; await nextTick()
    expect(wrapper.get('[data-testid="mic-heard"]').text()).toBe(t('audio.micHeard'))
    voice.isMicTesting = false; await nextTick()
    expect(wrapper.find('[data-testid="mic-heard"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('puts the focus on the key button when push to talk asks for a key, so the next key is heard', async () => {
    const wrapper = mount(AudioSettingsPanel, { ...mountOpts, attachTo: document.body })
    await requireValue(wrapper.findAll('button').find(b => b.text().includes('Push-to-Talk'))).trigger('click')
    await nextTick()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="ptt-key-button"]').element)
    wrapper.unmount()
  })

  it('lights the key line while the push-to-talk key is held', async () => {
    const voice = useVoiceStore(); voice.inputMode = 'ptt'; voice.pttKey = 'F8'
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    // The key listens during the microphone test (or a Talk) only, and says so.
    expect(wrapper.get('[data-testid="ptt-key-state"]').text()).toBe(t('audio.pttKeyIdleTest', { key: 'F8' }))
    voice.isMicTesting = true; await nextTick()
    expect(wrapper.get('[data-testid="ptt-key-state"]').text()).toBe(t('audio.pttKeyIdle', { key: 'F8' }))
    voice.isPttPressed = true; await nextTick()
    expect(wrapper.get('[data-testid="ptt-key-state"]').text()).toBe(t('audio.pttKeyHeld'))
    expect(wrapper.text()).toContain(t('audio.pttKeyHint'))
    wrapper.unmount()
  })
})

describe('AudioSettingsPanel silence saving', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.clearAllMocks() })

  it('is on by default, can be turned off and stays off', async () => {
    const voice = useVoiceStore()
    expect(voice.silenceSaving).toBe(true)
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const toggle = wrapper.get('[data-testid="silence-saving"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(wrapper.text()).toContain(t('audio.silenceSavingHint'))
    await toggle.trigger('click')
    expect(voice.silenceSaving).toBe(false)
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(localStorage.getItem('mnema_dtx')).toBe('false')
    setActivePinia(createPinia())
    expect(useVoiceStore().silenceSaving).toBe(false)
    await toggle.trigger('click')
    expect(voice.silenceSaving).toBe(true)
    wrapper.unmount()
  })
})

describe('AudioSettingsPanel preference interactions', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.clearAllMocks() })
  it('switches input mode, records code or key, ignores keys while not recording, changes manual sensitivity', async () => {
    const voice = useVoiceStore(); voice.inputMode = 'activity'; voice.autoSensitivity = false
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const ptt = requireValue(wrapper.findAll('button').find(b => b.text().includes('Push-to-Talk')))
    await ptt.trigger('click'); expect(voice.inputMode).toBe('ptt')
    const record = wrapper.get('[data-testid="ptt-key-button"]')
    // No key yet: choosing push to talk asks for one at once, and says so until it is set.
    expect(voice.pttKey).toBe('')
    expect(record.text()).toBe(t('audio.pressKey'))
    expect(wrapper.text()).toContain(t('audio.pttKeyNone'))
    await record.trigger('keydown', { key: 'q', code: 'KeyQ' }); expect(voice.pttKey).toBe('KeyQ')
    expect(record.text()).toBe('Q')
    await record.trigger('click'); await record.trigger('keydown', { key: 'F', code: 'KeyF' }); expect(voice.pttKey).toBe('KeyF')
    await record.trigger('keydown', { key: 'z', code: 'KeyZ' }); expect(voice.pttKey).toBe('KeyF')
    await record.trigger('click'); await record.trigger('keydown', { key: 'x', code: '' }); expect(voice.pttKey).toBe('x')
    await requireValue(wrapper.findAll('button').find(b => b.text().includes(t('audio.activity')))).trigger('click')
    await wrapper.get(`input[aria-label="${t('audio.sensitivity')}"]`).setValue(70); expect(voice.sensitivityThreshold).toBe(70)
    await wrapper.get('input[type="checkbox"]').setValue(true); expect(voice.autoSensitivity).toBe(true)
    expect(wrapper.find(`input[aria-label="${t('audio.sensitivity')}"]`).exists()).toBe(false)
    wrapper.unmount()
  })
  it('lets Tab and lone modifiers through, cancels recording on Escape and blur, and rolls the noise mode with the arrows', async () => {
    const voice = useVoiceStore(); voice.inputMode = 'ptt'; voice.pttKey = 'KeyV'
    const wrapper = mount(AudioSettingsPanel, { ...mountOpts, attachTo: document.body })
    const previous = voice.pttKey
    const record = wrapper.get('[data-testid="ptt-key-button"]')
    await record.trigger('click')
    await record.trigger('keydown', { key: 'Tab', code: 'Tab' }); expect(voice.pttKey).toBe(previous)
    await record.trigger('keydown', { key: 'Shift', code: 'ShiftLeft' }); expect(voice.pttKey).toBe(previous)
    await record.trigger('keydown', { key: 'Escape', code: 'Escape' }); expect(voice.pttKey).toBe(previous)
    await record.trigger('keydown', { key: 'q', code: 'KeyQ' }); expect(voice.pttKey).toBe(previous)
    await record.trigger('click'); await record.trigger('blur')
    await record.trigger('keydown', { key: 'q', code: 'KeyQ' }); expect(voice.pttKey).toBe(previous)

    const radios = wrapper.findAll('[role="radio"]')
    const selected = radios.findIndex(r => r.attributes('aria-checked') === 'true')
    expect(radios.map(r => r.attributes('tabindex'))).toEqual(radios.map((_, i) => (i === selected ? '0' : '-1')))
    await radios[selected]!.trigger('keydown', { key: 'ArrowRight' })
    expect(wrapper.findAll('[role="radio"]')[(selected + 1) % radios.length]!.attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
  it('applies AGC/echo/noise only to a running call or local mic test and ignores unchanged noise', async () => {
    const voice = useVoiceStore(); voice.isMicTesting = true
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const agc = wrapper.get(`button[aria-label="${t('audio.agc')}"]`)
    const echo = wrapper.get('button[aria-label="Echounterdrückung"]')
    const initialAgc = voice.autoGainControl; const initialEcho = voice.echoCancellation
    await agc.trigger('click'); await echo.trigger('click')
    expect(voice.autoGainControl).toBe(!initialAgc); expect(voice.echoCancellation).toBe(!initialEcho)
    const selected = wrapper.get('[role="radio"][aria-checked="true"]')
    const before = mockApplyAudioSettings.mock.calls.length; await selected.trigger('click'); expect(mockApplyAudioSettings).toHaveBeenCalledTimes(before)
    for (const radio of wrapper.findAll('[role="radio"]')) { await radio.trigger('click'); expect(radio.attributes('aria-checked')).toBe('true') }
    voice.isMicTesting = false; mockApplyAudioSettings.mockClear(); await agc.trigger('click'); await echo.trigger('click'); expect(mockApplyAudioSettings).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('changes input/output volumes, enables sound effects, previews default and individual sound events', async () => {
    const voice = useVoiceStore(); voice.soundEffectsEnabled = false
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    await wrapper.get('[data-testid="input-volume"]').setValue(150); await wrapper.get('[data-testid="output-volume"]').setValue(75)
    expect(voice.inputVolume).toBe(150); expect(voice.outputVolume).toBe(75)
    await wrapper.get('button[aria-label="Soundeffekte"]').trigger('click'); expect(voice.soundEffectsEnabled).toBe(true)
    for (const button of wrapper.findAll('button')) { if (button.attributes('aria-label')?.startsWith(t('audio.soundsTest')) || button.text() === t('audio.soundsTest')) await button.trigger('click') }
    wrapper.unmount()
  })
})

describe('AudioSettingsPanel device selection', () => {
  function device(deviceId: string, kind: MediaDeviceKind, label: string): MediaDeviceInfo {
    return { deviceId, kind, label, groupId: 'test-device-group', toJSON: () => ({ deviceId, kind, label, groupId: 'test-device-group' }) }
  }
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.clearAllMocks() })
  it('lists named and anonymous input/output devices, filters system output duplicates and applies selected input', async () => {
    const voice = useVoiceStore()
    voice.availableInputDevices = [device('mic-a', 'audioinput', 'Microphone A'), device('mic-b', 'audioinput', '')]
    voice.availableOutputDevices = [device('', 'audiooutput', 'Empty'), device('default', 'audiooutput', 'Default'), device('speaker-a', 'audiooutput', 'Speaker A'), device('speaker-b', 'audiooutput', '')]
    voice.localAudioStream = new MediaStream()
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const input = wrapper.get('#audio-input-device'); const output = wrapper.get('[data-testid="output-device"]')
    expect(input.text()).toContain('Microphone A'); expect(input.text()).toContain('mic-b')
    expect(output.findAll('option')).toHaveLength(3); expect(output.text()).toContain('Speaker A'); expect(output.text()).toContain('speak')
    await input.setValue('mic-b'); expect(voice.selectedInputDeviceId).toBe('mic-b'); expect(mockApplyAudioSettings).toHaveBeenCalledOnce()
    await output.setValue('speaker-a'); expect(voice.selectedOutputDeviceId).toBe('speaker-a')
    wrapper.unmount()
  })
})

it('explains system output routing when the browser has no device selection API', () => {
  const descriptor = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'setSinkId')
  Reflect.deleteProperty(HTMLMediaElement.prototype, 'setSinkId')
  try {
    setActivePinia(createPinia())
    const w = mount(AudioSettingsPanel, mountOpts)
    expect(w.find('[data-testid="output-device"]').exists()).toBe(false)
    expect(w.text()).toContain(t('audio.outputDeviceUnsupported'))
    w.unmount()
  } finally {
    if (descriptor) Object.defineProperty(HTMLMediaElement.prototype, 'setSinkId', descriptor)
  }
})

describe('AudioSettingsPanel as a section of the settings', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); vi.clearAllMocks() })

  it('tells the call code to publish the input level while it is shown, and stops when left', () => {
    const voice = useVoiceStore()
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    expect(voice.showAudioSettings).toBe(true)
    wrapper.unmount()
    expect(voice.showAudioSettings).toBe(false)
    expect(mockStopMicTest).toHaveBeenCalledTimes(1)
  })
})

describe('AudioSettingsPanel saves at once', () => {
  it('says "Saved" whenever a control stored a setting', async () => {
    vi.useFakeTimers()
    localStorage.clear()
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    const wrapper = mount(AudioSettingsPanel, mountOpts)
    const indicator = () => wrapper.find('[data-testid="saved-indicator"]').text()
    expect(indicator()).toBe('')
    voice.saveSettings()
    await nextTick()
    expect(indicator()).toBe(t('draft.saved'))
    await vi.advanceTimersByTimeAsync(2500)
    expect(indicator()).toBe('')
    wrapper.unmount()
    vi.useRealTimers()
  })
})
