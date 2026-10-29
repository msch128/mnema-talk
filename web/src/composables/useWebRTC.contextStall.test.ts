import { afterEach, describe, it, expect, vi } from 'vitest'
import { toRaw } from 'vue'
import { present } from '../media-test.fixture'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { installHarness, FakeAudioContext, fakeStream, setup } from './useWebRTC.harness.fixture'
import { suppressor, type SuppressorDouble } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()
afterEach(() => { vi.useRealTimers() })

// Firefox without an audio output device (and any browser that holds audio back
// until a gesture) never renders: the context stays suspended and resume() hangs.
class StalledContext extends FakeAudioContext {
  constructor() { super(); this.state = 'suspended' }
  resume() { return new Promise<void>(() => {}) }
}

const toasts = (key: string) => useToastStore().toasts.filter(toast => toast.text === t(key))
const node = (): SuppressorDouble => ({ connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() })

const captures: MediaStream[] = []

async function stalledCall(mode: 'ai' | 'browser' = 'browser', inputVolume = 100) {
  vi.useFakeTimers()
  vi.stubGlobal('AudioContext', StalledContext)
  captures.length = 0
  vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => { const stream = fakeStream(); captures.push(stream); return stream })
  const call = setup()
  call.voice.noiseMode = mode
  call.voice.inputVolume = inputVolume
  call.voice.warnNoAudioDetected = false
  call.voice.hangoverMs = 0
  call.voice.autoSensitivity = false
  call.voice.sensitivityThreshold = 20
  await call.rtc.joinVoiceChannel('stalled')
  return call
}

describe('an audio context that never starts rendering', () => {
  it('is worked around after 3 s: the member is told once and the microphone is captured again', async () => {
    const { voice } = await stalledCall()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2900)
    expect(toasts('audio.engineStalled')).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(200)
    expect(toasts('audio.engineStalled')).toHaveLength(1)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(2)
    // The capture itself is sent: a stream made inside the dead context would be silent.
    expect(toRaw(voice.localAudioStream)).toBe(captures[1])
  })

  it('does not route the capture through the context for an input volume either', async () => {
    const { voice } = await stalledCall('browser', 50)
    await vi.advanceTimersByTimeAsync(3100)
    await vi.advanceTimersByTimeAsync(100)
    expect(toRaw(voice.localAudioStream)).toBe(captures[1])
  })

  it('does not drop the AI filter for later calls and says so once per page load', async () => {
    suppressor.node = node()
    const { rtc } = await stalledCall('ai')
    await vi.advanceTimersByTimeAsync(3100)
    expect(toasts('audio.engineStalled')).toHaveLength(1)
    // The fallback rebuilt the call without the AI filter.
    expect(suppressor.models).toEqual(['dfn3'])
    rtc.leaveVoiceChannel()
    await rtc.joinVoiceChannel('stalled-again')
    await vi.advanceTimersByTimeAsync(3100)
    expect(toasts('audio.engineStalled')).toHaveLength(1)
  })

  it('keeps the gate open: the dead meter reads silence and must not mute the member', async () => {
    const { voice } = await stalledCall()
    FakeAudioContext.sample = 128
    await vi.advanceTimersByTimeAsync(3100)
    await vi.advanceTimersByTimeAsync(200)
    const track = present(present(voice.localAudioStream).getAudioTracks()[0])
    expect(track.enabled).toBe(true)
    voice.isMuted = true
    await vi.advanceTimersByTimeAsync(100)
    expect(track.enabled).toBe(false)
  })

  it('leaves push-to-talk alone: it needs no meter', async () => {
    const { voice } = await stalledCall()
    voice.inputMode = 'ptt'
    await vi.advanceTimersByTimeAsync(3300)
    const track = present(present(voice.localAudioStream).getAudioTracks()[0])
    expect(track.enabled).toBe(false)
    voice.isPttPressed = true
    await vi.advanceTimersByTimeAsync(100)
    expect(track.enabled).toBe(true)
  })

  it('is no business of a context that starts in time', async () => {
    vi.useFakeTimers()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => fakeStream())
    const { rtc } = setup()
    await rtc.joinVoiceChannel('fine')
    await vi.advanceTimersByTimeAsync(5000)
    expect(toasts('audio.engineStalled')).toHaveLength(0)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
  })
})

describe('the AI filter where it cannot run', () => {
  it('tells the member once that it is unavailable and captures with the browser suppression', async () => {
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const join = rtc.joinVoiceChannel('ch-1')
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => fakeStream())
    await join
    expect(toasts('audio.filterUnsupported')).toHaveLength(1)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenLastCalledWith({ audio: expect.objectContaining({ noiseSuppression: true }) })
    rtc.leaveVoiceChannel()
    await rtc.joinVoiceChannel('ch-1')
    expect(toasts('audio.filterUnsupported')).toHaveLength(1)
  })

  it('falls back to the browser suppression when the context refuses to connect the capture', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const original = FakeAudioContext.prototype.createMediaStreamSource
    let refused = 0
    vi.spyOn(FakeAudioContext.prototype, 'createMediaStreamSource').mockImplementation(function (this: FakeAudioContext, stream: MediaStream) {
      if (refused++ === 0) throw new DOMException('Connecting AudioNodes from AudioContexts with different sample-rate is currently not supported', 'NotSupportedError')
      return original.call(this, stream)
    })
    const filter = node()
    suppressor.node = filter
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => fakeStream())
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    await rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(2))
    expect(filter.destroy).toHaveBeenCalled()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenLastCalledWith({ audio: expect.objectContaining({ noiseSuppression: true }) })
    expect(voice.localAudioStream).not.toBeNull()
  })
})

describe('leaving the page', () => {
  it('takes the filter and the audio context down before the browser cancels them', async () => {
    const filter = node()
    suppressor.node = filter
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => fakeStream())
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    await rtc.joinVoiceChannel('ch-1')
    const ctx = present(FakeAudioContext.instances.at(-1))
    window.dispatchEvent(Object.assign(new Event('pagehide'), { persisted: false }))
    expect(filter.destroy).toHaveBeenCalled()
    expect(ctx.closed).toBe(true)
    window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: false }))
  })

  it('leaves a page that may come back from the back/forward cache alone', async () => {
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(async () => fakeStream())
    const { rtc } = setup()
    await rtc.joinVoiceChannel('ch-1')
    const ctx = present(FakeAudioContext.instances.at(-1))
    window.dispatchEvent(Object.assign(new Event('pagehide'), { persisted: true }))
    expect(ctx.closed).toBe(false)
  })
})
