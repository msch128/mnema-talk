import { describe, it, expect, vi } from 'vitest'
import { toRaw } from 'vue'
import { present } from '../media-test.fixture'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import * as noiseSuppressor from '../lib/noiseSuppressor'
import { installHarness, FakeAudioContext, fakeStream, grantMic, joined, setup } from './useWebRTC.harness.fixture'
import { suppressor } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

function toastTexts() {
  return useToastStore().toasts.map(toast => toast.text)
}

describe('joining without a usable microphone', () => {
  it.each([
    ['NotAllowedError', 'voice.micDenied'],
    ['NotFoundError', 'voice.micNotFound'],
    ['NotReadableError', 'voice.micBusy'],
    ['TypeError', 'voice.micFailed'],
  ])('tells the user why a %s leaves the join listen-only', async (name, key) => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new DOMException('no', name))
    const { rtc, voice } = setup()
    await rtc.joinVoiceChannel('silent')
    expect(voice.localAudioStream).toBeNull()
    expect(toastTexts()).toContain(t(key))
  })

  it('falls back to the default device when the saved one is unplugged', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const spare = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia)
      .mockRejectedValueOnce(new DOMException('gone', 'OverconstrainedError'))
      .mockResolvedValueOnce(spare)
    const { rtc, voice } = setup()
    voice.selectedInputDeviceId = 'usb-headset'
    await rtc.joinVoiceChannel('ch-1')
    const calls = vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls
    expect(present(calls[0])[0]).toMatchObject({ audio: { deviceId: { exact: 'usb-headset' } } })
    expect(present(present(calls[1])[0]?.audio)).not.toHaveProperty('deviceId')
    expect(voice.localAudioStream).not.toBeNull()
    expect(voice.selectedInputDeviceId).toBe('')
    expect(toastTexts()).toContain(t('voice.micDeviceReset'))
  })

  it('does not retry a denied permission on the default device', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new DOMException('no', 'NotAllowedError'))
    const { rtc, voice } = setup()
    voice.selectedInputDeviceId = 'usb-headset'
    await rtc.joinVoiceChannel('ch-1')
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
    expect(voice.selectedInputDeviceId).toBe('usb-headset')
  })

  it('reports a failing mic test', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new DOMException('no', 'NotFoundError'))
    const { rtc, voice } = setup()
    expect(await rtc.startMicTest()).toBe(false)
    expect(voice.isMicTesting).toBe(false)
    expect(toastTexts()).toContain(t('voice.micNotFound'))
  })
})

describe('switching the microphone in a call', () => {
  it('tells the user when the new device is not applied and keeps the running one', async () => {
    const { rtc, voice, mic } = await joined()
    const old = voice.localAudioStream
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new DOMException('busy', 'NotReadableError'))
    await rtc.applyAudioSettings()
    expect(voice.localAudioStream).toBe(old)
    expect(present(mic.getTracks()[0]).stop).not.toHaveBeenCalled()
    expect(toastTexts()).toContain(t('voice.micBusy'))
  })

  it('drops the dead microphone and says so when the new pipeline cannot be built', async () => {
    Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 'owned-test')
    vi.stubGlobal('location', { protocol: 'https:' })
    try {
      const { rtc, voice, audio } = await joined()
      expect(voice.localAudioStream).not.toBeNull()
      vi.spyOn(FakeAudioContext.prototype, 'createGain').mockImplementation(() => { throw new Error('unavailable') })
      vi.spyOn(console, 'warn').mockImplementation(() => {})
      const swap = rtc.applyAudioSettings()
      const replacement = await grantMic()
      await swap
      expect(voice.localAudioStream).toBeNull()
      expect(present(replacement.getTracks()[0]).stop).toHaveBeenCalled()
      expect(audio.replaceTrack).toHaveBeenLastCalledWith(null)
      expect(toastTexts()).toContain(t('voice.micLost'))
    } finally { Reflect.deleteProperty(window, '__MNEMA_GAMING_BRIDGE__'); vi.unstubAllGlobals() }
  })

  it('keeps the running microphone while an AI model is still downloading', async () => {
    const { rtc, voice, mic } = await joined()
    let loaded: (() => void) | undefined
    vi.spyOn(noiseSuppressor, 'preloadNoiseSuppressor').mockImplementation(() => new Promise<void>(resolve => { loaded = resolve }))
    voice.noiseMode = 'ai'
    const swap = rtc.applyAudioSettings()
    const replacement = await grantMic()
    await new Promise(resolve => setTimeout(resolve, 10))
    expect(present(mic.getTracks()[0]).stop).not.toHaveBeenCalled()
    suppressor.node = { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
    present(loaded)()
    await swap
    expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.localAudioStream).not.toBeNull()
    expect(present(replacement.getTracks()[0]).stop).not.toHaveBeenCalled()
  })

  it('takes the speaking ring down and keeps an unpressed push-to-talk track closed', async () => {
    vi.useFakeTimers()
    const { rtc, voice, sent } = setup()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    await rtc.joinVoiceChannel('ch-1')
    voice.isMuted = false
    await vi.advanceTimersByTimeAsync(60)
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: true } })

    voice.inputMode = 'ptt'
    voice.isPttPressed = false
    const replacement = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(replacement)
    await rtc.applyAudioSettings()
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: false } })
    // The sent track is the one that stays closed; the capture behind it keeps feeding the meter.
    expect(present(toRaw(voice.localAudioStream)?.getAudioTracks()[0]).enabled).toBe(false)
    expect(present(replacement.getAudioTracks()[0]).enabled).toBe(true)
  })
})
