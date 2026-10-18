import { describe, it, expect, vi } from 'vitest'
import { TestStream, present } from '../media-test.fixture'
import { toRaw, nextTick } from 'vue'
import { t } from '../i18n'
import { setGamingPttLease } from './useWebRTC'
import { useToastStore } from '../stores/toast'
import { installHarness, FakeAudioContext, type FakeGain, FakePC, attachedStream, audioElements, fakeStream, fakeTrack, grantMic, joined, negotiate, remoteStreamFor, setup, streamOf } from './useWebRTC.harness.fixture'
import { suppressor, type SuppressorDouble } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('push-to-talk', () => {
  async function joinedWithPTT() {
    const ctx = setup()
    ctx.voice.inputMode = 'ptt'
    ctx.voice.pttKey = 'Space'
    const join = ctx.rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    return ctx
  }

  it('does not open the mic while typing in a text field', async () => {
    const { voice } = await joinedWithPTT()
    const input = document.createElement('textarea')
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space', bubbles: true }))
    expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(true)
    input.remove()
  })

  it('releases when the window loses focus and after leaving', async () => {
    const { voice, rtc } = await joinedWithPTT()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    window.dispatchEvent(new Event('blur'))
    expect(voice.isPttPressed).toBe(false)

    rtc.leaveVoiceChannel()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(false)
  })
})

describe('audio settings in a call', () => {
  it('swaps the new microphone into the running connection', async () => {
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    const oldStream = await grantMic()
    await join
    const sender = present((await negotiate()).senders[0])

    const apply = rtc.applyAudioSettings()
    const newStream = await grantMic()
    await apply
    expect(sender.replaceTrack).toHaveBeenCalledWith(present(newStream.getAudioTracks()[0]))
    expect(present(oldStream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(toRaw(rtc.localAudioStream.value)).toBe(newStream)
  })
})

describe('AI noise suppression', () => {
  function fakeSuppressor() {
    return { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
  }
  const lastConstraints = () => {
    const audio = present(present(vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.at(-1))[0]).audio
    if (typeof audio !== 'object' || audio === null) throw new Error('Expected audio constraints')
    return audio
  }

  async function joinWith(mode: 'ai'|'ai-lite'|'browser'|'off') {
    const { rtc, voice } = setup()
    voice.setNoiseMode(mode)
    const join = rtc.joinVoiceChannel('ch-1')
    const raw = await grantMic()
    await join
    await negotiate()
    return { rtc, voice, raw }
  }

  it('sends the filtered track and asks the browser for the unprocessed mic', async () => {
    suppressor.node = fakeSuppressor()
    const { voice, raw } = await joinWith('ai')

    expect(suppressor.models).toEqual(['dfn3'])
    expect(lastConstraints().noiseSuppression).toBe(false)
    const processed = present(present(FakeAudioContext.destinations.at(-1)).stream.getAudioTracks()[0])
    expect(present(present(FakePC.instances.at(-1)).senders[0]).track).toBe(processed)
    expect(suppressor.node.connect).toHaveBeenCalled()

    // Mute switches the sent track; the raw capture keeps feeding the model and meter.
    voice.toggleMute()
    expect(processed.enabled).toBe(false)
    expect(present(raw.getAudioTracks()[0]).enabled).toBe(true)
  })

  it('uses the light model in ai-lite mode', async () => {
    suppressor.node = fakeSuppressor()
    await joinWith('ai-lite')
    expect(suppressor.models).toEqual(['gtcrn'])
  })

  it('stops the raw mic and destroys the filter on leave', async () => {
    const node = suppressor.node = fakeSuppressor()
    const { rtc, raw } = await joinWith('ai')

    rtc.leaveVoiceChannel()
    expect(present(raw.getAudioTracks()[0]).stop).toHaveBeenCalled()
    expect(node.destroy).toHaveBeenCalled()
    expect(present(present(suppressor.options[0]).signal).aborted).toBe(true)
  })

  it('discloses native fallback after worker failure, preserves the AI choice and never unmutes', async () => {
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    suppressor.node = fakeSuppressor()
    const { voice } = await joinWith('ai')
    voice.isMuted = true
    const previous = voice.localAudioStream
    present(present(suppressor.options[0]).onFailure)()
    present(present(suppressor.options[0]).onFailure)()
    const nativeStream = await grantMic()
    await vi.waitFor(() => expect(voice.localAudioStream).not.toBe(previous))
    expect(lastConstraints().noiseSuppression).toBe(true)
    expect(voice.noiseMode).toBe('ai')
    expect(present(nativeStream.getAudioTracks()[0]).enabled).toBe(false)
    expect(useToastStore().toasts.filter(x => x.text === t('audio.filterDegraded'))).toHaveLength(1)
    expect(suppressor.models).toEqual(['dfn3'])
  })

  it('falls back to the browser filter when the worklet is unavailable', async () => {
    const { raw } = await joinWith('ai')

    expect(lastConstraints().noiseSuppression).toBe(true)
    expect(present(present(FakePC.instances.at(-1)).senders[0]).track).toBe(present(raw.getAudioTracks()[0]))
  })

  it('off disables every filter', async () => {
    suppressor.node = fakeSuppressor()
    const { raw } = await joinWith('off')

    expect(suppressor.models).toEqual([])
    expect(lastConstraints().noiseSuppression).toBe(false)
    expect(present(present(FakePC.instances.at(-1)).senders[0]).track).toBe(present(raw.getAudioTracks()[0]))
  })
})

describe('per-user playback', () => {
  function remoteAudio(pc: FakePC, userId: string) {
    present(pc.ontrack)({ track: fakeTrack('audio'), streams: [remoteStreamFor(userId)] })
    return present(audioElements.at(-1))
  }

  it('applies a user volume and local mute to that user only', async () => {
    const { voice, pc } = await joined()
    const alice = remoteAudio(pc, 'alice')
    const bob = remoteAudio(pc, 'bob')

    voice.setUserVolume('alice', 50)
    await vi.waitFor(() => expect(alice.volume).toBe(0.5))
    expect(bob.volume).toBe(1)

    voice.toggleLocalMute('alice')
    await vi.waitFor(() => expect(alice.volume).toBe(0))
    voice.toggleLocalMute('alice')
    await vi.waitFor(() => expect(alice.volume).toBe(0.5))
  })

  it("the stream volume sets only the gain of the screen share's sound, the voice volume only the voice", async () => {
    const { voice, pc } = await joined()
    const doraVoice = remoteAudio(pc, 'dora')
    const doraStream = remoteAudio(pc, 'screen:dora')
    expect(doraStream.dataset.userId).toBe('dora')
    expect(doraStream.dataset.source).toBe('screen')
    expect(doraVoice.dataset.source).toBeUndefined()
    // Someone's stream starts at half volume.
    expect(doraStream.volume).toBe(0.5)
    expect(doraVoice.volume).toBe(1)

    voice.setStreamVolume('dora', 20)
    await vi.waitFor(() => expect(doraStream.volume).toBe(0.2))
    expect(doraVoice.volume).toBe(1)

    voice.toggleStreamMute('dora')
    await vi.waitFor(() => expect(doraStream.volume).toBe(0))
    expect(doraVoice.volume).toBe(1)
    voice.toggleStreamMute('dora')
    await vi.waitFor(() => expect(doraStream.volume).toBe(0.2))

    // The member menu's volume and mute stay with the voice.
    voice.setUserVolume('dora', 50)
    await vi.waitFor(() => expect(doraVoice.volume).toBe(0.5))
    expect(doraStream.volume).toBe(0.2)
    voice.toggleLocalMute('dora')
    await vi.waitFor(() => expect(doraVoice.volume).toBe(0))
    expect(doraStream.volume).toBe(0.2)
  })

  it('multiplies with the master volume and deafen wins', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 50
    const alice = remoteAudio(pc, 'alice')
    voice.setUserVolume('alice', 50)
    await vi.waitFor(() => expect(alice.volume).toBe(0.25))
    voice.isDeafened = true
    await vi.waitFor(() => expect(alice.volume).toBe(0))
  })

  function remoteStream(id: string) {
    const stream = new TestStream()
    stream.id = id
    stream.removeTrack = (track: MediaStreamTrack) => {
      stream.dispatchEvent(Object.assign(new Event('removetrack'), { track }))
    }
    return stream
  }

  it('reuses the element when the SFU hands a track to another speaker', async () => {
    const { voice, pc } = await joined()
    const track = Object.assign(fakeTrack('audio'), { id: 't1' })
    const alice = remoteStream('alice')
    const bob = remoteStream('bob')
    present(pc.ontrack)({ track, streams: [alice] })
    present(pc.ontrack)({ track, streams: [bob] })
    expect(audioElements).toHaveLength(1)
    const el = present(audioElements[0])
    expect(el.dataset.userId).toBe('bob')

    // Bob's volume applies to it now, not Alice's.
    voice.setUserVolume('alice', 0)
    voice.setUserVolume('bob', 50)
    await vi.waitFor(() => expect(el.volume).toBe(0.5))

    // The old stream letting go of the track must not silence Bob.
    alice.removeTrack(track)
    expect(el.srcObject).not.toBeNull()
    bob.removeTrack(track)
    expect(el.srcObject).toBeNull()
  })

  it('replaces one screen-audio sink without duplicating it or removing the microphone', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 100
    const microphone = remoteAudio(pc, 'alice')
    const previousTrack = fakeTrack('audio')
    const previousStream = remoteStream('screen:alice')
    present(pc.ontrack)({ track: previousTrack, streams: [previousStream] })
    const previousElement = present(audioElements.at(-1))
    voice.setStreamVolume('alice', 80)
    await vi.waitFor(() => expect(previousElement.volume).toBe(0.8))

    const replacementTrack = fakeTrack('audio')
    const replacementStream = remoteStream('screen:alice')
    present(pc.ontrack)({ track: replacementTrack, streams: [replacementStream] })
    const replacementElement = present(audioElements.at(-1))
    expect(previousElement.srcObject).toBeNull()
    expect(previousElement.pause).toHaveBeenCalledOnce()
    expect(replacementElement.volume).toBe(0.8)
    expect(attachedStream(microphone).getAudioTracks()).toHaveLength(1)
    expect(attachedStream(replacementElement).getAudioTracks()).toEqual([replacementTrack])
    expect(audioElements.filter(el => el.srcObject).map(el => el.dataset.streamId)).toEqual(['alice', 'screen:alice'])

    present(previousTrack.onended).call(previousTrack, new Event('ended'))
    previousStream.removeTrack(previousTrack)
    expect(attachedStream(replacementElement).getAudioTracks()).toEqual([replacementTrack])
    replacementStream.removeTrack(replacementTrack)
    expect(replacementElement.srcObject).toBeNull()
    expect(microphone.srcObject).not.toBeNull()
  })

  it('disconnects a replaced amplified voice graph before its new track starts playing', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 100
    const old = remoteAudio(pc, 'alice')
    voice.setUserVolume('alice', 150)
    await vi.waitFor(() => expect(old.muted).toBe(true))
    const previousSource = present(FakeAudioContext.sources.at(-1))
    const previousGain = present(FakeAudioContext.gains.at(-1))
    const next = remoteAudio(pc, 'alice')
    expect(old.srcObject).toBeNull()
    expect(previousSource.disconnect).toHaveBeenCalledOnce()
    expect(previousGain.disconnect).toHaveBeenCalledOnce()
    expect(next.muted).toBe(true)
    expect(audioElements.filter(el => el.srcObject)).toEqual([next])
  })

  it('keeps microphone and screen sound out of the video stream and ignores repeated delivery', async () => {
    const { voice, pc } = await joined()
    const mic = fakeTrack('audio')
    const audio = fakeTrack('audio')
    const video = fakeTrack('video')
    const mixedIncoming = Object.assign(streamOf([video, mic, audio]), { id: 'alice' })
    voice.watchScreen('alice')
    present(pc.ontrack)({ track: mic, streams: [mixedIncoming] })
    present(pc.ontrack)({ track: audio, streams: [remoteStreamFor('screen:alice')] })
    present(pc.ontrack)({ track: audio, streams: [remoteStreamFor('screen:alice')] })
    present(pc.ontrack)({ track: video, streams: [mixedIncoming] })
    expect(audioElements).toHaveLength(2)
    expect(attachedStream(present(audioElements[0])).getAudioTracks()).toEqual([mic])
    expect(attachedStream(present(audioElements[1])).getAudioTracks()).toEqual([audio])
    expect(present(voice.remoteScreenStream).getVideoTracks()).toEqual([video])
    expect(present(voice.remoteScreenStream).getAudioTracks()).toEqual([])
  })

  it('does not let callbacks from a replaced track object remove its replacement with the same ID', async () => {
    const { pc } = await joined()
    const previous = Object.assign(fakeTrack('audio'), { id: 'reused' })
    const next = Object.assign(fakeTrack('audio'), { id: 'reused' })
    const stream = remoteStream('screen:alice')
    present(pc.ontrack)({ track: previous, streams: [stream] })
    const old = present(audioElements.at(-1))
    present(pc.ontrack)({ track: next, streams: [stream] })
    const current = present(audioElements.at(-1))
    expect(old.srcObject).toBeNull()
    present(previous.onended).call(previous, new Event('ended'))
    stream.removeTrack(previous)
    expect(attachedStream(current).getAudioTracks()).toEqual([next])
    present(next.onended).call(next, new Event('ended'))
    expect(current.srcObject).toBeNull()
  })

  it('amplifies above 100 % through a gain node', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 100
    const alice = remoteAudio(pc, 'alice')
    voice.setUserVolume('alice', 150)
    await vi.waitFor(() => expect(present(FakeAudioContext.gains.at(-1))?.gain.value).toBe(1.5))
    // The element only keeps the stream flowing; the graph does the playing.
    expect(alice.muted).toBe(true)

    voice.setUserVolume('alice', 100)
    await vi.waitFor(() => expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(1))
  })
})

describe('voice activity, loopback and window lifecycle', () => {
  async function timedCall() {
    vi.useFakeTimers()
    const ctx = setup()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    await ctx.rtc.joinVoiceChannel('timed')
    return ctx
  }

  it('gates transmission, reports speaking transitions and mutes the active channel during loopback', async () => {
    const { rtc, voice, sent } = await timedCall()
    voice.showAudioSettings = true
    voice.warnNoAudioDetected = false
    voice.hangoverMs = 0
    voice.autoSensitivity = false
    voice.sensitivityThreshold = 20
    const track = present(present(voice.localAudioStream).getAudioTracks()[0])
    FakeAudioContext.sample = 128
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(false)
    expect(voice.currentInputLevel).toBe(0)
    FakeAudioContext.sample = 150
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(true)
    expect(voice.currentInputLevel).toBeGreaterThan(20)
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: true } })
    voice.isMuted = true
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(false)
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: false } })
    voice.isMuted = false
    await vi.advanceTimersByTimeAsync(60)
    await rtc.toggleMicTest()
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(false)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(voice.outputVolume / 100)
    FakeAudioContext.sample = 128
    await vi.advanceTimersByTimeAsync(60)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(0)
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: false } })
    rtc.stopMicTest()
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(false)
  })

  it('warns after prolonged silence only once per cooldown and resets after speech or disabling warnings', async () => {
    const { voice } = await timedCall()
    vi.setSystemTime(new Date('2026-10-07T12:00:00Z'))
    FakeAudioContext.sample = 128
    voice.warnNoAudioDetected = true
    const toasts = useToastStore()
    vi.spyOn(toasts, 'info')
    await vi.advanceTimersByTimeAsync(16_000)
    expect(toasts.info).toHaveBeenCalledWith(t('audio.noAudioDetectedToast'))
    const count = vi.mocked(toasts.info).mock.calls.length
    await vi.advanceTimersByTimeAsync(16_000)
    expect(toasts.info).toHaveBeenCalledTimes(count)
    FakeAudioContext.sample = 140
    await vi.advanceTimersByTimeAsync(60)
    FakeAudioContext.sample = 128
    voice.warnNoAudioDetected = false
    await vi.advanceTimersByTimeAsync(60)
    voice.warnNoAudioDetected = true
    await vi.advanceTimersByTimeAsync(130_000)
    expect(toasts.info).toHaveBeenCalledTimes(count + 1)
  })

  it('gates a standalone monitor from the measured input and closes its resources', async () => {
    vi.useFakeTimers()
    const { rtc, voice } = setup()
    voice.hangoverMs = 0
    voice.outputVolume = 65
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    expect(await rtc.toggleMicTest()).toBe(true)
    FakeAudioContext.sample = 180
    await vi.advanceTimersByTimeAsync(50)
    expect(voice.currentInputLevel).toBe(100)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(0.65)
    FakeAudioContext.sample = 128
    await vi.advanceTimersByTimeAsync(50)
    expect(voice.currentInputLevel).toBe(0)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(0)
    rtc.stopMicTest()
    expect(present(FakeAudioContext.instances.at(-1)).closed).toBe(true)
  })

  it('uses key fallback, ignores repeats, unrelated keys and typing, and releases on visibility loss', async () => {
    const { voice } = await timedCall()
    voice.inputMode = 'ptt'
    voice.pttKey = 'x'
    const input = document.createElement('select')
    document.body.append(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'x', bubbles: true }))
    expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'x', repeat: true }))
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'other' }))
    expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'other' }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'x' }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'x' }))
    expect(voice.isPttPressed).toBe(true)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'x' }))
    voice.inputMode = 'activity'
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'x' }))
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'x' }))
    input.remove()
  })

  it('resumes suspended microphone and paused remote audio on visibility return', async () => {
    const { rtc } = await timedCall()
    const pc = present(FakePC.instances.at(-1))
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    const el = present(audioElements.at(-1))
    el.play = vi.fn().mockRejectedValue(new Error('gesture pending'))
    const audio = present(FakeAudioContext.instances.at(-1))
    audio.state = 'suspended'
    const resume = vi.spyOn(audio, 'resume').mockRejectedValue(new Error('gesture pending'))
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    await Promise.resolve()
    expect(resume).toHaveBeenCalled()
    expect(el.play).toHaveBeenCalled()
    rtc.leaveVoiceChannel()
    document.dispatchEvent(new Event('visibilitychange'))
    expect(resume).toHaveBeenCalledTimes(1)
  })

  it('inserts input gain once after a debounced slider change then changes gain live', async () => {
    const { rtc, voice } = await timedCall()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    voice.inputVolume = 50
    await nextTick()
    voice.inputVolume = 40
    await nextTick()
    await vi.advanceTimersByTimeAsync(250)
    const gain = present(FakeAudioContext.gains.at(-1))
    expect(gain.gain.value).toBe(0.4)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(2)
    voice.inputVolume = 70
    await nextTick()
    expect(gain.gain.value).toBe(0.7)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(2)
    rtc.leaveVoiceChannel()
    voice.inputVolume = 100
    await nextTick()
  })
})

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
    expect(toRaw(voice.localAudioStream)).toBe(raw)
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
    expect(toRaw(voice.localAudioStream)).toBe(raw)
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
    await vi.waitFor(() => expect(toRaw(voice.localAudioStream)).toBe(replacement))
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
    await vi.waitFor(() => expect(toRaw(voice.localAudioStream)).toBe(replacement))
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
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'suspended-call' } })
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
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'device-removed' } })
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
    expect(audio.track).toBe(replacement.getAudioTracks()[0])
    expect(voice.currentChannelId).toBe('ch-1')
  })
})
