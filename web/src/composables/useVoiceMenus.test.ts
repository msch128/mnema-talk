import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { useVoiceMenus } from './useVoiceMenus'
import { useVoiceStore } from '../stores/voice'
import { requireValue } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { ContextMenuItem } from '../components/menuTypes'

const rtc = {
  refreshAudioDevices: vi.fn(),
  applyAudioSettings: vi.fn(),
  toggleCamera: vi.fn(),
  restartCamera: vi.fn(),
  startScreenShare: vi.fn(),
  stopScreenShare: vi.fn()
}
vi.mock('./useWebRTC', () => ({ useWebRTC: () => rtc }))

const dev = (kind: MediaDeviceKind, deviceId: string, label: string) => ({ kind, deviceId, label, groupId: '' }) as MediaDeviceInfo

const NAMES = ['mic', 'output', 'camera', 'share'] as const
type Menus = ReturnType<typeof useVoiceMenus>
type Name = typeof NAMES[number]

// One real button per menu, bound as the call bars bind it.
function setup(options: Parameters<typeof useVoiceMenus>[0] = {}) {
  let menus!: Menus
  const wrapper = mount(defineComponent({
    setup() {
      menus = useVoiceMenus(options)
      return () => h('div', NAMES.map(name => h('button', { 'data-name': name, ...menus[name].on })))
    }
  }), { attachTo: document.body })
  wrappers.push(wrapper)
  const button = (name: Name) => requireValue(wrapper.find<HTMLButtonElement>(`[data-name="${name}"]`).element)
  return { ...menus, button }
}
const wrappers: Array<{ unmount(): void }> = []
type Setup = ReturnType<typeof setup>

function rightClick(menus: Setup, name: Name) {
  const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 5, clientY: 5 })
  menus.button(name).dispatchEvent(event)
  return event
}

async function open(menus: Setup, name: Name) {
  rightClick(menus, name)
  await nextTick(); await nextTick()
  return menus[name].menu.items.value
}
function find(items: ContextMenuItem[], id: string): ContextMenuItem {
  return requireValue(items.find(i => i.id === id))
}
function sub(items: ContextMenuItem[], id: string) {
  const item = find(items, id)
  if (item.type !== 'submenu') throw new Error(`${id} is not a submenu`)
  return item
}
function act(item: ContextMenuItem) {
  if (!('action' in item) || !item.action) throw new Error('no action')
  return item.action()
}

afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); document.body.innerHTML = '' })
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  localStorage.clear()
  Object.values(rtc).forEach(fn => fn.mockClear())
})

describe('microphone menu', () => {
  it('lists the devices with the current one checked and "default" first', async () => {
    const voice = useVoiceStore()
    voice.availableInputDevices = [dev('audioinput', 'a', 'Studio mic'), dev('audioinput', 'b', 'Headset')]
    voice.selectedInputDeviceId = 'b'
    const items = await open(setup(), 'mic')
    const devices = sub(items, 'mic-device')
    expect(devices.value).toBe('Headset')
    expect(devices.items.map(i => [i.label, i.type === 'radio' && i.checked])).toEqual([['System default', false], ['Studio mic', false], ['Headset', true]])
    expect(rtc.refreshAudioDevices).toHaveBeenCalled()
  })

  it('switches the device through the saved setting and the running pipeline', async () => {
    const voice = useVoiceStore()
    voice.availableInputDevices = [dev('audioinput', 'a', 'Studio mic')]
    voice.localAudioStream = new MediaStream()
    const items = await open(setup(), 'mic')
    act(requireValue(sub(items, 'mic-device').items[1]))
    expect(voice.selectedInputDeviceId).toBe('a')
    expect(localStorage.getItem('mnema_input_dev')).toBe('a')
    expect(rtc.applyAudioSettings).toHaveBeenCalledTimes(1)
  })

  it('does not open a microphone outside a call just to apply a device', async () => {
    const voice = useVoiceStore()
    voice.availableInputDevices = [dev('audioinput', 'a', 'Studio mic')]
    const items = await open(setup(), 'mic')
    act(requireValue(sub(items, 'mic-device').items[1]))
    expect(rtc.applyAudioSettings).not.toHaveBeenCalled()
  })

  it('falls back to a short id while the browser hides device names', async () => {
    const voice = useVoiceStore()
    voice.availableInputDevices = [dev('audioinput', 'abcdefgh', '')]
    const items = await open(setup(), 'mic')
    const labels = sub(items, 'mic-device').items.map(i => i.label)
    expect(labels).toContain('Microphone abcde…')
    expect(labels).toContain('Device names appear once the browser has allowed access.')
  })

  it('shows the live level only while the microphone is open, with the speaking threshold', async () => {
    const voice = useVoiceStore()
    const menus = setup()
    expect((await open(menus, 'mic')).some(i => i.type === 'meter')).toBe(false)
    voice.localAudioStream = new MediaStream()
    voice.currentInputLevel = 42
    const meter = find(await open(menus, 'mic'), 'mic-level')
    expect(meter).toMatchObject({ type: 'meter', value: 42 })
    voice.inputMode = 'ptt'
    expect(find(await open(menus, 'mic'), 'mic-level')).not.toHaveProperty('threshold')
  })

  it('toggles noise suppression live and push to talk by setting', async () => {
    const voice = useVoiceStore()
    voice.localAudioStream = new MediaStream()
    voice.noiseMode = 'ai'
    const menus = setup()
    act(find(await open(menus, 'mic'), 'mic-noise'))
    expect(voice.noiseMode).toBe('off')
    expect(rtc.applyAudioSettings).toHaveBeenCalledTimes(1)
    act(find(await open(menus, 'mic'), 'mic-ptt'))
    expect(voice.inputMode).toBe('ptt')
    // No key is chosen by default, so the row shows none; a chosen key shows as a word.
    const noKey = find(await open(menus, 'mic'), 'mic-ptt')
    expect(noKey).toMatchObject({ checked: true })
    expect(noKey).not.toHaveProperty('shortcut')
    voice.pttKey = 'KeyV'
    expect(find(await open(menus, 'mic'), 'mic-ptt')).toMatchObject({ checked: true, shortcut: 'V' })
    voice.pttKey = 'Space'
    expect(find(await open(menus, 'mic'), 'mic-ptt')).toMatchObject({ shortcut: 'Space' })
    act(find(await open(menus, 'mic'), 'mic-ptt'))
    expect(voice.inputMode).toBe('activity')
  })

  it('sets the input volume and opens the audio settings, except in full screen', async () => {
    const voice = useVoiceStore()
    const items = await open(setup(), 'mic')
    const slider = find(items, 'mic-volume')
    if (slider.type !== 'slider') throw new Error('slider expected')
    slider.onInput?.(150)
    expect(voice.inputVolume).toBe(150)
    act(find(items, 'audio-settings'))
    expect(voice.showAudioSettings).toBe(true)
    expect((await open(setup({ fullscreen: () => true }), 'mic')).some(i => i.id === 'audio-settings')).toBe(false)
  })
})

describe('headphones menu', () => {
  it('picks the output device and volume, and explains when the browser cannot route output', async () => {
    const voice = useVoiceStore()
    voice.availableOutputDevices = [dev('audiooutput', 'o1', 'Speakers')]
    Object.defineProperty(HTMLMediaElement.prototype, 'setSinkId', { configurable: true, value: vi.fn() })
    try {
      const items = await open(setup(), 'output')
      act(requireValue(sub(items, 'out-device').items[1]))
      expect(voice.selectedOutputDeviceId).toBe('o1')
      const slider = find(items, 'out-volume')
      if (slider.type !== 'slider') throw new Error('slider expected')
      slider.onInput?.(80)
      expect(voice.outputVolume).toBe(80)
    } finally {
      Reflect.deleteProperty(HTMLMediaElement.prototype, 'setSinkId')
    }
    const unsupported = find(await open(setup(), 'output'), 'out-device')
    expect(unsupported).toMatchObject({ disabled: true })
    expect(unsupported.disabledReason).toMatch(/can’t pick an output device/)
  })

  it('toggles the sound effects', async () => {
    const voice = useVoiceStore()
    const was = voice.soundEffectsEnabled
    act(find(await open(setup(), 'output'), 'out-sounds'))
    expect(voice.soundEffectsEnabled).toBe(!was)
  })
})

describe('camera menu', () => {
  it('switches the camera and restarts a running one on the new device', async () => {
    const voice = useVoiceStore()
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { enumerateDevices: async () => [dev('videoinput', 'c1', 'Webcam'), dev('audioinput', 'm', 'Mic')] } })
    const menus = setup()
    await open(menus, 'camera')
    await new Promise(r => setTimeout(r, 0))
    const items = await open(menus, 'camera')
    expect(sub(items, 'camera-device').items.map(i => i.label)).toEqual(['System default', 'Webcam'])
    act(requireValue(sub(items, 'camera-device').items[1]))
    expect(voice.selectedCameraDeviceId).toBe('c1')
    expect(rtc.restartCamera).toHaveBeenCalled()
    act(find(items, 'camera-toggle'))
    expect(rtc.toggleCamera).toHaveBeenCalled()
  })
})

describe('screen share menu', () => {
  it('has quality choices and a danger "stop sharing", only while sharing', async () => {
    const voice = useVoiceStore()
    const menus = setup()
    expect(menus.share.menu.state.open).toBe(false)
    expect(rightClick(menus, 'share').defaultPrevented).toBe(false)
    voice.isScreenSharing = true
    const items = await open(menus, 'share')
    expect(sub(items, 'resolution').items.length).toBeGreaterThan(1)
    const stop = find(items, 'stop-share')
    expect(stop).toMatchObject({ danger: true })
    act(stop)
    expect(rtc.stopScreenShare).toHaveBeenCalled()
  })

  it('applies a resolution through the voice store without restarting the share', async () => {
    const voice = useVoiceStore()
    voice.isScreenSharing = true
    const items = await open(setup(), 'share')
    act(requireValue(sub(items, 'resolution').items.find(i => i.id === 'resolution-720')))
    expect(voice.screenQuality.resolution).toBe(720)
    expect(rtc.stopScreenShare).not.toHaveBeenCalled()
    expect(rtc.startScreenShare).not.toHaveBeenCalled()
  })

  it('explains why the stream sound cannot be muted when the share has none, and hides the stream dialog in full screen', async () => {
    const voice = useVoiceStore()
    voice.isScreenSharing = true
    const mute = find(await open(setup(), 'share'), 'mute-sound')
    expect(mute).toMatchObject({ disabled: true, disabledReason: 'This share has no sound to mute.' })
    expect((await open(setup({ fullscreen: () => true }), 'share')).some(i => i.id === 'stream-settings')).toBe(false)
  })
})
