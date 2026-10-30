// The menus of the call buttons: microphone, headphones, camera and the
// screen share. Each builds ContextMenu items from the voice store and
// applies a choice through the same paths as the audio settings dialog, so
// the AI filter, the push-to-talk gate and the silence watcher keep working.
import { pttKeyLabel } from '../lib/pttKey'
import { ref } from 'vue'
import { Sliders, Video, VideoOff } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from './useWebRTC'
import { useActiveAudioSettings } from './useActiveAudioSettings'
import { useStreamQuality } from './useStreamQuality'
import { useContextMenuTrigger } from './useContextMenuTrigger'
import { canChooseOutputDevice } from '../lib/audioOutput'
import { effectiveThreshold } from '../lib/levelMeter'
import { t } from '../i18n'
import type { ContextMenuItem } from '../components/menuTypes'

interface DeviceChoice { deviceId: string; label: string }
const VALUE_MAX = 24
const SYSTEM_DEFAULT = '__default__'

function short(text: string): string {
  return text.length > VALUE_MAX ? `${text.slice(0, VALUE_MAX - 1)}…` : text
}

/** Names the browser shows only after a permission; until then a short id stands in. */
function deviceLabel(dev: DeviceChoice, fallbackKey: string): string {
  return dev.label || t(fallbackKey, { id: dev.deviceId.slice(0, 5) })
}

/** "Default" first, then the devices; the current one is checked. */
function deviceSubmenu(opts: {
  id: string
  label: string
  devices: DeviceChoice[]
  selected: string
  fallbackKey: string
  choose: (deviceId: string) => void
}): ContextMenuItem {
  const known = opts.devices.filter(d => d.deviceId && d.deviceId !== 'default' && d.deviceId !== 'communications')
  const current = known.find(d => d.deviceId === opts.selected)
  const radio = (id: string, label: string, deviceId: string): ContextMenuItem => ({
    id: `${opts.id}-${id}`,
    type: 'radio',
    label,
    checked: (opts.selected === deviceId) || (deviceId === '' && !current),
    keepOpen: true,
    action: () => opts.choose(deviceId)
  })
  const items: ContextMenuItem[] = [radio(SYSTEM_DEFAULT, t('ctxMenu.systemDefault'), ''), ...known.map(d => radio(d.deviceId, deviceLabel(d, opts.fallbackKey), d.deviceId))]
  if (known.some(d => !d.label)) items.push({ type: 'separator' }, { type: 'label', label: t('ctxMenu.namesNeedAccess') })
  return {
    id: opts.id,
    type: 'submenu',
    label: opts.label,
    value: short(current ? deviceLabel(current, opts.fallbackKey) : t('ctxMenu.systemDefault')),
    minWidth: 280,
    items
  }
}

/** The camera list lives here: the voice store only keeps audio devices. */
const cameras = ref<MediaDeviceInfo[]>([])
async function refreshCameras() {
  try {
    if (!navigator.mediaDevices?.enumerateDevices) return
    cameras.value = (await navigator.mediaDevices.enumerateDevices()).filter(d => d.kind === 'videoinput')
  } catch (err) {
    console.warn('enumerateDevices error:', err)
  }
}

type VoiceStore = ReturnType<typeof useVoiceStore>
interface MenuContext {
  voice: VoiceStore
  rtc: ReturnType<typeof useWebRTC>
  applyAudio: ReturnType<typeof useActiveAudioSettings>
  quality: ReturnType<typeof useStreamQuality>
  fullscreen: () => boolean
}

function settingsItems({ voice, fullscreen }: MenuContext): ContextMenuItem[] {
  if (fullscreen()) return []
  return [{ type: 'separator' }, { id: 'audio-settings', label: t('audio.settings'), icon: Sliders, action: () => { voice.showAudioSettings = true } }]
}

function micItems(cx: MenuContext): ContextMenuItem[] {
  const { voice } = cx
  const ptt = voice.inputMode === 'ptt'
  const chooseInput = (deviceId: string) => {
    voice.selectedInputDeviceId = deviceId
    voice.saveSettings()
    return cx.applyAudio()
  }
  const toggleNoise = () => {
    voice.toggleNoiseCancelling()
    return cx.applyAudio()
  }
  const togglePtt = () => {
    voice.inputMode = ptt ? 'activity' : 'ptt'
    voice.saveSettings()
  }
  const items: ContextMenuItem[] = [
    deviceSubmenu({ id: 'mic-device', label: t('audio.inputDevice'), devices: voice.availableInputDevices, selected: voice.selectedInputDeviceId, fallbackKey: 'audio.deviceFallback', choose: chooseInput }),
    { id: 'mic-volume', type: 'slider', label: t('audio.inputVolume'), value: voice.inputVolume, min: 0, max: 200, step: 5, onInput: v => { voice.inputVolume = v } }
  ]
  // The level exists while the microphone is open (in a call or in the mic test).
  if (voice.localAudioStream || voice.isMicTesting) {
    items.push({ id: 'mic-level', type: 'meter', label: t('audio.levelLabel'), value: voice.currentInputLevel, ...(ptt ? {} : { threshold: effectiveThreshold(voice) }) })
  }
  items.push(
    { type: 'separator' },
    { id: 'mic-noise', type: 'checkbox', label: t('audio.noise'), checked: voice.noiseCancelling, keepOpen: true, action: toggleNoise },
    { id: 'mic-ptt', type: 'checkbox', label: t('audio.ptt'), checked: ptt, keepOpen: true, ...(ptt && voice.pttKey ? { shortcut: pttKeyLabel(voice.pttKey) } : {}), action: togglePtt },
    ...settingsItems(cx)
  )
  return items
}

function outputItems(cx: MenuContext): ContextMenuItem[] {
  const { voice } = cx
  const device: ContextMenuItem = canChooseOutputDevice()
    ? deviceSubmenu({ id: 'out-device', label: t('audio.outputDevice'), devices: voice.availableOutputDevices, selected: voice.selectedOutputDeviceId, fallbackKey: 'ctxMenu.outputFallback', choose: id => { voice.selectedOutputDeviceId = id } })
    : { id: 'out-device', label: t('audio.outputDevice'), disabled: true, disabledReason: t('audio.outputDeviceUnsupported') }
  const toggleSounds = () => {
    voice.soundEffectsEnabled = !voice.soundEffectsEnabled
    voice.saveSettings()
  }
  return [
    device,
    { id: 'out-volume', type: 'slider', label: t('audio.outputVolume'), value: voice.outputVolume, min: 0, max: 200, step: 5, onInput: v => { voice.outputVolume = v } },
    { type: 'separator' },
    { id: 'out-sounds', type: 'checkbox', label: t('audio.soundsTitle'), checked: voice.soundEffectsEnabled, keepOpen: true, action: toggleSounds },
    ...settingsItems(cx)
  ]
}

function cameraItems({ voice, rtc }: MenuContext): ContextMenuItem[] {
  const on = voice.isCameraOn
  const choose = (deviceId: string) => {
    voice.selectedCameraDeviceId = deviceId
    voice.saveSettings()
    // A running camera moves to the new device; a stopped one uses it next time.
    return rtc.restartCamera()
  }
  return [
    { id: 'camera-toggle', label: on ? t('talk.stopCamera') : t('talk.startCamera'), icon: on ? VideoOff : Video, action: () => { void rtc.toggleCamera() } },
    { type: 'separator' },
    deviceSubmenu({ id: 'camera-device', label: t('ctxMenu.cameraDevice'), devices: cameras.value, selected: voice.selectedCameraDeviceId, fallbackKey: 'ctxMenu.cameraFallback', choose }),
    { id: 'camera-hide-others', type: 'checkbox', label: t('talk.allCamerasOff'), checked: voice.allCamerasOff, keepOpen: true, action: () => voice.setAllCamerasOff(!voice.allCamerasOff) }
  ]
}

function shareItems({ rtc, quality, fullscreen }: MenuContext): ContextMenuItem[] {
  // The quality choices apply live (useWebRTC follows the store); nothing here restarts the share.
  const items = quality.menuItems({ onChangeSource: () => rtc.startScreenShare(), settings: !fullscreen() })
  const explained = items.map(i => i.id === 'mute-sound' && i.disabled ? { ...i, disabledReason: t('ctxMenu.noShareSound') } : i)
  return [...explained, { type: 'separator' }, { id: 'stop-share', label: t('voice.stopShare'), danger: true, action: () => { void rtc.stopScreenShare() } }]
}

export function useVoiceMenus({ fullscreen = () => false }: { fullscreen?: () => boolean } = {}) {
  const voice = useVoiceStore()
  const rtc = useWebRTC()
  const cx: MenuContext = {
    voice,
    rtc,
    applyAudio: useActiveAudioSettings(),
    quality: useStreamQuality({
      getStats: () => rtc.getScreenSendStats?.() ?? null,
      getCaptureSettings: () => rtc.getScreenCaptureSettings?.() ?? null
    }),
    fullscreen
  }
  const refreshAudio = () => { void rtc.refreshAudioDevices() }
  return {
    mic: useContextMenuTrigger(() => micItems(cx), { onOpen: refreshAudio }),
    output: useContextMenuTrigger(() => outputItems(cx), { onOpen: refreshAudio }),
    camera: useContextMenuTrigger(() => cameraItems(cx), { onOpen: () => { void refreshCameras() } }),
    share: useContextMenuTrigger(() => shareItems(cx), { enabled: () => voice.isScreenSharing })
  }
}
