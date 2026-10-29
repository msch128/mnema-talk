import { describe, it, expect, vi } from 'vitest'
import { TestStream, present } from '../media-test.fixture'
import { nextTick } from 'vue'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { installHarness, FakeAudioContext, FakePC, attachedStream, audioElements, fakeStream, fakeTrack, grantMic, joined, negotiate, remoteStreamFor, setup, streamOf } from './useWebRTC.harness.fixture'
import { suppressor } from './useWebRTC.noiseSuppressor.fixture'

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
    // The new capture is routed through the audio context; its output is what the line sends.
    const sent = present(present(rtc.localAudioStream.value).getAudioTracks()[0])
    expect(sender.replaceTrack).toHaveBeenCalledWith(sent)
    expect(sent).not.toBe(present(newStream.getAudioTracks()[0]))
    expect(present(oldStream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(newStream.getAudioTracks()[0]).stop).not.toHaveBeenCalled()
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
    // Mute switches the sent track; the native capture behind it stays on for the meter.
    expect(present(present(voice.localAudioStream).getAudioTracks()[0]).enabled).toBe(false)
    expect(present(nativeStream.getAudioTracks()[0]).enabled).toBe(true)
    expect(useToastStore().toasts.filter(x => x.text === t('audio.filterDegraded'))).toHaveLength(1)
    expect(suppressor.models).toEqual(['dfn3'])
  })

  it('falls back to the browser filter when the worklet is unavailable', async () => {
    const { voice } = await joinWith('ai')

    expect(lastConstraints().noiseSuppression).toBe(true)
    expect(present(present(FakePC.instances.at(-1)).senders[0]).track).toBe(present(present(voice.localAudioStream).getAudioTracks()[0]))
  })

  it('off disables every filter', async () => {
    suppressor.node = fakeSuppressor()
    const { voice } = await joinWith('off')

    expect(suppressor.models).toEqual([])
    expect(lastConstraints().noiseSuppression).toBe(false)
    expect(present(present(FakePC.instances.at(-1)).senders[0]).track).toBe(present(present(voice.localAudioStream).getAudioTracks()[0]))
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
    const captured = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(captured)
    await ctx.rtc.joinVoiceChannel('timed')
    return { ...ctx, captured }
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

  describe('no-sound warning reads the raw capture', () => {
    async function watched() {
      const ctx = await timedCall()
      ctx.voice.warnNoAudioDetected = true
      ctx.voice.inputMode = 'activity'
      const toasts = useToastStore()
      vi.spyOn(toasts, 'info')
      return { ...ctx, toasts }
    }

    it('stays quiet in a quiet room: a faint raw signal is not silence, whatever the processed meter reads', async () => {
      const { toasts } = await watched()
      FakeAudioContext.sample = 128
      FakeAudioContext.rawSample = 0.0005
      await vi.advanceTimersByTimeAsync(40_000)
      expect(toasts.info).not.toHaveBeenCalled()
    })

    it('warns once per cooldown on digital silence and resets after any signal', async () => {
      const { toasts } = await watched()
      FakeAudioContext.rawSample = 0
      await vi.advanceTimersByTimeAsync(16_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
      expect(toasts.info).toHaveBeenCalledWith(t('audio.noAudioDetectedToast'), expect.anything())
      await vi.advanceTimersByTimeAsync(30_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
      FakeAudioContext.rawSample = 0.001
      await vi.advanceTimersByTimeAsync(60)
      FakeAudioContext.rawSample = 0
      await vi.advanceTimersByTimeAsync(130_000)
      expect(toasts.info).toHaveBeenCalledTimes(2)
    })

    it('does not warn while muted in Mnema, in the mic test or with an idle push-to-talk key', async () => {
      const { voice, toasts } = await watched()
      FakeAudioContext.rawSample = 0
      voice.isMuted = true
      await vi.advanceTimersByTimeAsync(20_000)
      voice.isMuted = false
      voice.inputMode = 'ptt'
      await vi.advanceTimersByTimeAsync(20_000)
      expect(toasts.info).not.toHaveBeenCalled()
    })

    it('gives a hardware mute one calm hint per call', async () => {
      const { captured, toasts } = await watched()
      captured.getAudioTracks().forEach(track => { (track as unknown as { muted: boolean }).muted = true })
      FakeAudioContext.rawSample = 0.05
      await vi.advanceTimersByTimeAsync(16_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
      expect(toasts.info).toHaveBeenCalledWith(t('audio.micHardwareMutedToast'), expect.anything())
      await vi.advanceTimersByTimeAsync(400_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
    })

    it('stays off for the rest of the call once dismissed, and with the setting off', async () => {
      const { voice, toasts } = await watched()
      FakeAudioContext.rawSample = 0
      await vi.advanceTimersByTimeAsync(16_000)
      const options = vi.mocked(toasts.info).mock.calls[0]?.[1]
      await present(options?.action).onClick()
      await vi.advanceTimersByTimeAsync(400_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
      voice.warnNoAudioDetected = false
      await vi.advanceTimersByTimeAsync(400_000)
      expect(toasts.info).toHaveBeenCalledTimes(1)
    })
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

  it('reports a failed loopback in a call without starting a standalone test', async () => {
    const { rtc, voice } = await timedCall()
    const callCtx = present(FakeAudioContext.instances.at(-1))
    vi.spyOn(callCtx, 'createGain').mockImplementation(() => { throw new Error('graph broken') })
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const mediaCalls = vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.length
    const contexts = FakeAudioContext.instances.length
    expect(await rtc.toggleMicTest()).toBe(false)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(mediaCalls)
    expect(FakeAudioContext.instances).toHaveLength(contexts)
    expect(voice.isMicTesting).toBe(true)
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

