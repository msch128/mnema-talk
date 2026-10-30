import { describe, it, expect, vi } from 'vitest'
import { TestStream, present } from '../media-test.fixture'
import { toRaw, nextTick } from 'vue'
import { t } from '../i18n'
import { setGamingPttLease } from './useWebRTC'
import { useToastStore } from '../stores/toast'
import { installHarness, FakeAudioContext, type FakeGain, audioElements, fakeStream, fakeTrack, grantMic, joined, negotiate, remoteStreamFor, setup } from './useWebRTC.harness.fixture'
import { suppressor, type SuppressorDouble } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('microphone pipeline fallbacks and filter cancellation', () => {
  function node(): SuppressorDouble { return { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() } }

  it('routes desktop PTT through an audio-clock lease and closes on pipeline failure', async () => {
    Reflect.set(window, '__MNEMA_GAMING_BRIDGE__', 'owned-test')
    vi.stubGlobal('location', { protocol: 'https:' })
    try {
      const { rtc, voice } = setup(); voice.inputMode = 'ptt'
      const join = rtc.joinVoiceChannel('ch-1'); const raw = await grantMic(); await join
      const gain = present(FakeAudioContext.gains.at(-1))
      expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream)
      expect(gain.gain.setValueAtTime).toHaveBeenCalledWith(0, 0)
      setGamingPttLease(true); expect(gain.gain.setValueAtTime).toHaveBeenCalledWith(0, 0.3)
      voice.isPttPressed = true; await new Promise(resolve => setTimeout(resolve, 70))
      // The speaking callback runs while the release timer's old pressed state
      // is still present: it must close instead of renewing after the stall.
      const stalledClock = vi.spyOn(performance, 'now').mockReturnValue(performance.now() + 1000)
      voice.isPttPressed = true
      await new Promise(resolve => setTimeout(resolve, 70))
      expect(voice.isPttPressed).toBe(false)
      expect(gain.gain.setValueAtTime).toHaveBeenLastCalledWith(0, 0)
      stalledClock.mockRestore()
      voice.inputMode = 'activity'; await nextTick(); expect(gain.gain.value).toBe(1)
      rtc.leaveVoiceChannel(); expect(gain.disconnect).toHaveBeenCalled(); expect(raw.getAudioTracks()[0]?.stop).toHaveBeenCalled()
      vi.spyOn(FakeAudioContext.prototype, 'createGain').mockImplementation(() => { throw new Error('unavailable') })
      const again = rtc.joinVoiceChannel('ch-1'); const failed = await grantMic(); await again
      expect(voice.localAudioStream).toBeNull(); expect(failed.getAudioTracks()[0]?.stop).toHaveBeenCalled()
    } finally { Reflect.deleteProperty(window, '__MNEMA_GAMING_BRIDGE__'); vi.unstubAllGlobals() }
  })

  it('switches only the sent track when the gate closes, so the meter that reopens it is never starved', async () => {
    // WebKit left a meter on a clone of the gated capture silent for good after
    // a reload: 0 %, the gate never reopened, the member stayed mute all call.
    vi.useFakeTimers()
    const { rtc, voice } = setup()
    voice.noiseMode = 'off'
    voice.inputVolume = 100
    voice.hangoverMs = 0
    voice.autoSensitivity = false
    voice.sensitivityThreshold = 20
    const capture = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(capture)
    await rtc.joinVoiceChannel('ch-1')
    const sent = present(toRaw(voice.localAudioStream)?.getAudioTracks()[0])
    const captured = present(capture.getAudioTracks()[0])

    FakeAudioContext.sample = 128
    await vi.advanceTimersByTimeAsync(120)
    expect(sent.enabled).toBe(false)
    expect(sent).not.toBe(captured)
    // The capture stays on, and the meter listens to it directly rather than to a clone of the track the gate switches.
    expect(captured.enabled).toBe(true)
    expect(captured.clone).not.toHaveBeenCalled()
    expect(FakeAudioContext.sources.some(source => source.stream === capture)).toBe(true)

    FakeAudioContext.sample = 150
    await vi.advanceTimersByTimeAsync(120)
    expect(sent.enabled).toBe(true)
    rtc.leaveVoiceChannel()
    expect(captured.stop).toHaveBeenCalled()
    expect(sent.stop).toHaveBeenCalled()
  })

  it('falls back to the captured mic when AudioContext construction fails' , async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.stubGlobal('AudioContext', class { constructor() { throw new Error('unavailable') } })
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await join
    expect(toRaw(voice.localAudioStream)).toBe(raw)
    expect(FakeAudioContext.instances).toHaveLength(0)
  })

  it('keeps sending captured audio when the analyser cannot be built', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(FakeAudioContext.prototype, 'createAnalyser').mockImplementation(() => { throw new Error('unavailable analyser') })
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await join
    // The capture is routed to the sent stream whether or not it can be metered.
    expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream)
    expect(present(raw.getAudioTracks()[0]).stop).not.toHaveBeenCalled()
  })

  it('applies input gain after AI suppression and releases both captured and processed tracks', async () => {
    suppressor.node = node()
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    voice.inputVolume = 75
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await join
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(0.75)
    expect(FakeAudioContext.destinations).toHaveLength(2)
    expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream)
    const sent = present(present(voice.localAudioStream).getAudioTracks()[0])
    rtc.leaveVoiceChannel()
    expect(present(raw.getTracks()[0]).stop).toHaveBeenCalled()
    expect(sent.stop).toHaveBeenCalled()
    expect(suppressor.node.destroy).toHaveBeenCalled()
  })

  it('retains the native capture when an input gain stage is unavailable', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(FakeAudioContext.prototype, 'createGain').mockImplementation(() => { throw new Error('gain unavailable') })
    const { rtc, voice } = setup()
    voice.inputVolume = 50
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await join
    expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream)
    expect(present(raw.getAudioTracks()[0]).stop).not.toHaveBeenCalled()
  })

  it('discards a microphone pipeline whose filter finishes after leaving', async () => {
    suppressor.supported = true
    let finish: ((value: SuppressorDouble) => void) | undefined
    suppressor.create = () => new Promise(resolve => { finish = resolve })
    const { rtc, voice, sent } = setup()
    voice.noiseMode = 'ai'
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(finish)(node())
    await join
    expect(present(raw.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.localAudioStream).toBeNull()
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
  })

  it('recovers a failed call filter once through browser suppression without changing the saved mode', async () => {
    suppressor.node = node()
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const options = present(suppressor.options.at(-1))
    present(options.onFailure)()
    present(options.onFailure)()
    const replacement = await grantMic()
    await vi.waitFor(() => expect(FakeAudioContext.sources.some(source => source.stream === replacement)).toBe(true))
    await vi.waitFor(() => expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream))
    expect(voice.noiseMode).toBe('ai')
    expect(suppressor.models).toHaveLength(1)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenLastCalledWith({ audio: expect.objectContaining({ noiseSuppression: true }) })
    expect(useToastStore().toasts.filter(toast => toast.text === t('audio.filterDegraded'))).toHaveLength(1)
    rtc.leaveVoiceChannel()
    present(options.onFailure)()
    expect(useToastStore().toasts.filter(toast => toast.text === t('audio.filterDegraded'))).toHaveLength(1)
  })

  it('skips a scheduled fallback after the call ends', async () => {
    suppressor.node = node()
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    vi.useFakeTimers()
    present(present(suppressor.options.at(-1)).onFailure)()
    rtc.leaveVoiceChannel()
    await vi.advanceTimersByTimeAsync(0)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
  })

  it('uses browser suppression when a supported model returns no filter', async () => {
    suppressor.supported = true
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai-lite'
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const replacement = await grantMic()
    await vi.waitFor(() => expect(FakeAudioContext.sources.some(source => source.stream === replacement)).toBe(true))
    await vi.waitFor(() => expect(toRaw(voice.localAudioStream)).toBe(present(FakeAudioContext.destinations.at(-1)).stream))
    expect(suppressor.models).toEqual(['gtcrn'])
  })

  it('starts a filtered standalone test and destroys the filter when closed', async () => {
    suppressor.node = node()
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const test = rtc.startMicTest()
    await grantMic()
    expect(await test).toBe(true)
    expect(suppressor.models).toEqual(['dfn3'])
    expect(suppressor.node.connect).toHaveBeenCalled()
    rtc.stopMicTest()
    expect(suppressor.node.destroy).toHaveBeenCalled()
    expect(present(suppressor.options[0]).signal?.aborted).toBe(true)
  })

  it('destroys a standalone filter returned after its test was closed', async () => {
    suppressor.supported = true
    let finish: ((value: SuppressorDouble) => void) | undefined
    suppressor.create = () => new Promise(resolve => { finish = resolve })
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const test = rtc.startMicTest()
    const raw = await grantMic()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.stopMicTest()
    const filter = node()
    present(finish)(filter)
    expect(await test).toBe(false)
    expect(filter.destroy).toHaveBeenCalled()
    expect(present(raw.getTracks()[0]).stop).toHaveBeenCalled()
  })

  it('restarts a failed filtered standalone loopback with native suppression', async () => {
    suppressor.node = node()
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const test = rtc.toggleMicTest()
    await grantMic()
    expect(await test).toBe(true)
    present(present(suppressor.options.at(-1)).onFailure)()
    await grantMic()
    await vi.waitFor(() => expect(FakeAudioContext.instances).toHaveLength(2))
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenLastCalledWith({ audio: expect.objectContaining({ noiseSuppression: true }) })
    expect(voice.isMicTesting).toBe(true)
  })

  it('refuses loopback after denied permission and cleans streams after graph creation errors', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { rtc, voice } = setup()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValueOnce(new Error('denied'))
    expect(await rtc.toggleMicTest()).toBe(false)
    expect(voice.isMicTesting).toBe(false)
    expect(voice.currentInputLevel).toBe(0)
    vi.spyOn(FakeAudioContext.prototype, 'createAnalyser').mockImplementation(() => { throw new Error('broken graph') })
    const test = rtc.toggleMicTest()
    const raw = await grantMic()
    expect(await test).toBe(false)
    expect(present(raw.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(FakeAudioContext.instances.at(-1)).closed).toBe(true)
  })

  it('reuses an existing call context for standalone meter requests and resumes it without blocking', async () => {
    const { rtc } = await joined()
    const ctx = present(FakeAudioContext.instances.at(-1))
    ctx.state = 'suspended'
    vi.spyOn(ctx, 'resume').mockRejectedValue(new Error('gesture'))
    expect(await rtc.startMicTest()).toBe(true)
    expect(ctx.resume).toHaveBeenCalled()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
  })
})

describe('remote rendering and audio output robustness', () => {
  it('drops remote media that names no sender', async () => {
    const { pc, voice } = await joined()
    const before = audioElements.length
    present(pc.ontrack)({ track: fakeTrack(), streams: [] })
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('screen:')] })
    present(pc.ontrack)({ track: fakeTrack(), streams: [] })
    present(pc.ontrack)({ track: fakeTrack('video'), streams: [] })
    present(pc.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('cam:')] })
    expect(audioElements).toHaveLength(before)
    expect(voice.userVideoStreams).toEqual({})
    expect(voice.remoteScreenStream).toBeNull()
    expect(useToastStore().toasts).toEqual([])
  })

  it('drops only the removed video and ignores unrelated removetrack callbacks', async () => {
    const { pc, voice } = await joined()
    voice.watchScreen('alice')
    const screen = fakeTrack('video')
    const stream = remoteStreamFor('alice')
    present(pc.ontrack)({ track: screen, streams: [stream] })
    stream.dispatchEvent(Object.assign(new Event('removetrack'), { track: fakeTrack('video') }))
    expect(voice.remoteScreenStream).not.toBeNull()
    stream.dispatchEvent(Object.assign(new Event('removetrack'), { track: screen }))
    expect(voice.remoteScreenStream).toBeNull()
    const camera = fakeTrack('video')
    const cameraStream = remoteStreamFor('cam:bob')
    present(pc.ontrack)({ track: camera, streams: [cameraStream] })
    cameraStream.dispatchEvent(Object.assign(new Event('removetrack'), { track: camera }))
    expect(voice.userVideoStreams).toEqual({})
  })

  it('retries playback when a reused audio sink is paused and does not drop it for an unrelated removed track', async () => {
    const { pc } = await joined()
    const track = fakeTrack()
    const stream = remoteStreamFor('alice')
    present(pc.ontrack)({ track, streams: [stream] })
    const el = present(audioElements.at(-1))
    el.play = vi.fn().mockRejectedValue(new Error('gesture'))
    present(pc.ontrack)({ track, streams: [stream] })
    await Promise.resolve()
    expect(el.play).toHaveBeenCalledTimes(1)
    stream.dispatchEvent(Object.assign(new Event('removetrack'), { track: fakeTrack() }))
    expect(el.srcObject).not.toBeNull()
    Object.defineProperty(el, 'paused', { configurable: true, value: false })
    present(pc.ontrack)({ track, streams: [stream] })
    expect(el.play).toHaveBeenCalledTimes(1)
  })

  it('changes all playback output targets and resumes an amplified suspended context', async () => {
    const { rtc, pc, voice } = await joined()
    voice.setUserVolume('alice', 150)
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    const playback = present(FakeAudioContext.instances.at(-1))
    playback.state = 'suspended'
    vi.spyOn(playback, 'resume').mockRejectedValue(new Error('gesture'))
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('bob')] })
    voice.setUserVolume('bob', 180)
    await vi.waitFor(() => expect(playback.resume).toHaveBeenCalled())
    await rtc.toggleMicTest()
    voice.selectedOutputDeviceId = 'headphones'
    await nextTick()
    expect(playback.sinkId).toBe('headphones')
    expect(present(FakeAudioContext.instances[0]).sinkId).toBe('headphones')
    rtc.resumeRemoteAudio()
    await Promise.resolve()
    expect(playback.resume).toHaveBeenCalledTimes(2)
  })

  it('caps requested boost when Web Audio is missing or cannot build a playback graph', async () => {
    const { pc, voice } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    voice.setUserVolume('alice', 150)
    vi.stubGlobal('AudioContext', undefined)
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    expect(present(audioElements.at(-1)).volume).toBe(1)
    vi.stubGlobal('AudioContext', class extends FakeAudioContext { createGain(): FakeGain { throw new Error('no output') } })
    voice.setUserVolume('bob', 150)
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('bob')] })
    expect(present(audioElements.at(-1)).volume).toBe(1)
  })

  it('cleans peer and detached sinks even when their native cleanup calls fail', async () => {
    const { rtc, pc, voice } = await joined()
    voice.setUserVolume('alice', 150)
    const track = fakeTrack()
    const stream = remoteStreamFor('alice')
    present(pc.ontrack)({ track, streams: [stream] })
    const source = present(FakeAudioContext.sources.at(-1))
    source.disconnect.mockImplementation(() => { throw new Error('detached') })
    present(audioElements.at(-1)).pause = vi.fn(() => { throw new Error('disposed') })
    stream.dispatchEvent(Object.assign(new Event('removetrack'), { track }))
    const secondTrack = fakeTrack()
    present(pc.ontrack)({ track: secondTrack, streams: [remoteStreamFor('bob')] })
    present(audioElements.at(-1)).pause = vi.fn(() => { throw new Error('disposed') })
    pc.close = () => { throw new Error('already closed') }
    const playback = present(FakeAudioContext.instances.at(-1))
    vi.spyOn(playback, 'close').mockRejectedValue(new Error('disposed'))
    rtc.leaveVoiceChannel()
    expect(voice.currentChannelId).toBeNull()
    present(track.onended).call(track, new Event('ended'))
  })

  it('logs non-permission autoplay failures without showing a gesture banner', async () => {
    const { pc, voice } = await joined()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const originalAudio = globalThis.Audio
    vi.stubGlobal('Audio', function () {
      const el = new originalAudio()
      el.play = vi.fn().mockRejectedValue(new Error('media unavailable'))
      return el
    })
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('bob')] })
    await vi.waitFor(() => expect(warn).toHaveBeenCalled())
    expect(voice.audioBlocked).toBe(false)
  })
})

describe('native microphone graph rejection handling', () => {
  it('joins when a suspended native audio context rejects resume until a user gesture', async () => {
    class SuspendedContext extends FakeAudioContext {
      constructor() { super(); this.state = 'suspended' }
      resume() { return Promise.reject(new DOMException('A user gesture is required', 'NotAllowedError')) }
    }
    vi.stubGlobal('AudioContext', SuspendedContext)
    const { rtc, voice, sent } = setup()
    const join = rtc.joinVoiceChannel('suspended-call')
    await grantMic()
    await join
    await Promise.resolve()
    expect(voice.currentChannelId).toBe('suspended-call')
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'suspended-call', tab: expect.any(String) } })
  })

  it('retains microphone transmission when corrupt persisted volume is rejected by the native gain parameter', async () => {
    localStorage.setItem('mnema_input_volume', 'invalid persisted volume')
    const original = FakeAudioContext.prototype.createGain
    vi.spyOn(FakeAudioContext.prototype, 'createGain').mockImplementation(function (this: FakeAudioContext) {
      const node = original.call(this)
      node.gain.setValueAtTime.mockImplementation((value: number) => {
        if (!Number.isFinite(value)) throw new TypeError('AudioParam requires a finite value')
        node.gain.value = value
      })
      return node
    })
    const { rtc, voice } = setup()
    expect(Number.isNaN(voice.inputVolume)).toBe(true)
    const join = rtc.joinVoiceChannel('corrupt-setting')
    await grantMic()
    await join
    expect(voice.localAudioStream).not.toBeNull()
    expect(present(FakeAudioContext.gains.at(-1)).gain.setValueAtTime).toHaveBeenCalledWith(NaN, 0)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(1)
  })
})

describe('capture delivery microtasks and microphone source removal', () => {
  it('cleans a completed microphone graph when leave runs before the join continuation', async () => {
    const { rtc, voice, sent } = setup()
    const join = rtc.joinVoiceChannel('cancelled-continuation')
    const capture = await grantMic()
    rtc.leaveVoiceChannel()
    await join
    expect(present(capture.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.localAudioStream).toBeNull()
    expect(sent.some(event => event.type === 'voice_join')).toBe(false)
  })

  it('does not install a replacement whose permission continuation runs just before leave', async () => {
    const { rtc, voice } = await joined()
    const swap = rtc.applyAudioSettings()
    const capture = await grantMic()
    rtc.leaveVoiceChannel()
    await swap
    expect(present(capture.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.localAudioStream).toBeNull()
  })

  it('joins for receiving when the granted microphone disappears before the browser creates its source node', async () => {
    const originalSource = FakeAudioContext.prototype.createMediaStreamSource
    vi.spyOn(FakeAudioContext.prototype, 'createMediaStreamSource').mockImplementation(function (this: FakeAudioContext, stream: MediaStream) {
      if (!stream.getAudioTracks().length) throw new DOMException('No audio track remains', 'InvalidStateError')
      return originalSource.call(this, stream)
    })
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { rtc, sent } = setup()
    const removedCapture = fakeStream()
    removedCapture.removeTrack(present(removedCapture.getAudioTracks()[0]))
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(removedCapture)
    await rtc.joinVoiceChannel('device-removed')
    const pc = await negotiate()
    expect(present(pc.senders[0]).track).toBeNull()
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'device-removed', tab: expect.any(String) } })
    await rtc.applyAudioSettings()
    expect(present(pc.senders[0]).track).toBeNull()
  })
})

describe('audio graph fallback without a mounted document body', () => {
  it('plays a remote voice after the page body was detached and releases the sink on leave', async () => {
    const { rtc, pc, voice } = await joined()
    const body = document.body
    const parent = present(body.parentNode)
    body.remove()
    try {
      present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('detached-page-participant')] })
      const sink = present(audioElements.at(-1))
      expect(sink.play).toHaveBeenCalled()
      expect(sink.parentNode).toBeNull()
      expect(voice.audioBlocked).toBe(false)
      rtc.leaveVoiceChannel()
      expect(sink.srcObject).toBeNull()
    } finally {
      parent.appendChild(body)
    }
  })

  it('keeps existing call PTT listeners while a native context retry builds a standalone meter', async () => {
    let attempts = 0
    vi.stubGlobal('AudioContext', function () {
      if (++attempts === 1) throw new DOMException('Audio context temporarily unavailable', 'NotSupportedError')
      return new FakeAudioContext()
    })
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { rtc, voice } = setup()
    voice.inputMode = 'ptt'
    const join = rtc.joinVoiceChannel('context-retry-call')
    await grantMic()
    await join
    expect(voice.localAudioStream).not.toBeNull()
    const meter = rtc.startMicTest()
    await grantMic()
    expect(await meter).toBe(true)
    rtc.stopMicTest()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: voice.pttKey }))
    expect(voice.isPttPressed).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keyup', { code: voice.pttKey }))
    expect(voice.isPttPressed).toBe(false)
  })
})

describe('serialized settings after native source construction fails', () => {
  it('accepts the next microphone setting after the previous AI graph lost its audio source', async () => {
    const { rtc, voice, audio } = await joined()
    const originalSource = FakeAudioContext.prototype.createMediaStreamSource
    vi.spyOn(FakeAudioContext.prototype, 'createMediaStreamSource').mockImplementation(function (this: FakeAudioContext, stream: MediaStream) {
      if (!stream.getAudioTracks().length) throw new DOMException('The microphone disconnected', 'InvalidStateError')
      return originalSource.call(this, stream)
    })
    suppressor.node = { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
    voice.noiseMode = 'ai'
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValueOnce(new TestStream())
    await expect(rtc.applyAudioSettings()).rejects.toMatchObject({ name: 'InvalidStateError' })
    voice.noiseMode = 'browser'
    const retry = rtc.applyAudioSettings()
    const replacement = await grantMic()
    await retry
    expect(audio.track).toBe(present(toRaw(voice.localAudioStream)?.getAudioTracks()[0]))
    expect(FakeAudioContext.sources.some(source => source.stream === replacement)).toBe(true)
    expect(voice.currentChannelId).toBe('ch-1')
  })
})
