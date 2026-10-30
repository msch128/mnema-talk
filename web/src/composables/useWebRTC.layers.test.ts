import { describe, it, expect, vi } from 'vitest'
import { present } from '../media-test.fixture'
import { toRaw } from 'vue'
import { screenAudioConstraints, CAPTURE_POLL_MS, CAPTURE_SETTLE_MS } from './useWebRTC'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { streamBitrate, DEFAULT_STREAM_QUALITY } from '../lib/streamQuality'
import { installHarness, FakeAudioContext, FakePC, SFU_LAYER_OFFER, declared, declaredLayersOf, display, encodingOf, fakeSender, fakeStream, grantMic, installRecordingCall, joined, joinedWithLayers, negotiate, stubDisplayMedia } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('screen share with audio', () => {
  it('sends the screen audio on its own line, apart from the mic', async () => {
    const { rtc, audio, screen, screenAudio, sentMic, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)

    await rtc.startScreenShare()

    expect(screen.track).toBe(present(display.getVideoTracks()[0]))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    // The mic line keeps the bare microphone: nothing is mixed into the voice.
    expect(audio.track).toBe(sentMic)
    expect(FakeAudioContext.sources.map(n => present(n.stream.getAudioTracks()[0]))).not.toContain(present(display.getAudioTracks()[0]))
    expect(voice.hasScreenAudio).toBe(true)
  })

  it('drops a share picked after leaving the call', async () => {
    const { rtc, voice, sent } = await joined()
    const display = fakeStream(['video', 'audio'])
    let pick: (() => void) | undefined
    navigator.mediaDevices.getDisplayMedia = vi.fn(() => new Promise<MediaStream>(r => { pick = () => r(display) }))
    const share = rtc.startScreenShare()
    await vi.waitFor(() => expect(pick).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(pick)()
    await share
    expect(display.getTracks().every(tr => vi.mocked(tr.stop).mock.calls.length > 0)).toBe(true)
    expect(voice.isScreenSharing).toBe(false)
    expect(sent.some(e => e.type === 'webrtc_screenshare_start')).toBe(false)
  })

  it('asks the browser to leave out the voices this page plays', async () => {
    const { rtc } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()
    expect(present(present(vi.mocked(navigator.mediaDevices.getDisplayMedia).mock.calls[0])[0]).audio).toMatchObject({ echoCancellation: true })
  })

  it('leaves the page out of the capture where supported, else cancels its echo', () => {
    expect(screenAudioConstraints({ restrictOwnAudio: true })).toEqual({ autoGainControl: false, noiseSuppression: false, echoCancellation: false, restrictOwnAudio: true })
    expect(screenAudioConstraints({})).toEqual({ autoGainControl: false, noiseSuppression: false, echoCancellation: true })
  })

  it('checks actual captured audio and requests echo cancellation when exclusion was not applied', async () => {
    const { rtc } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = present(display.getAudioTracks()[0])
    let echoCancellation = false
    track.getSettings = () => ({ restrictOwnAudio: false, echoCancellation })
    track.getConstraints = () => ({ restrictOwnAudio: true, noiseSuppression: false })
    track.applyConstraints = vi.fn(async () => { echoCancellation = true })
    navigator.mediaDevices.getSupportedConstraints = () => ({ restrictOwnAudio: true })
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    expect(track.applyConstraints).toHaveBeenCalledWith({ restrictOwnAudio: true, noiseSuppression: false, echoCancellation: true })
    expect(useToastStore().toasts.filter(toast => toast.action)).toHaveLength(0)
  })

  it('leaves a confirmed own-audio restriction untouched', async () => {
    const { rtc } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = present(display.getAudioTracks()[0])
    track.getSettings = () => ({ restrictOwnAudio: true, echoCancellation: false })
    track.applyConstraints = vi.fn()
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    expect(track.applyConstraints).not.toHaveBeenCalled()
    expect(useToastStore().toasts.filter(toast => toast.action)).toHaveLength(0)
  })

  it('warns once if capture protection cannot be confirmed and offers stream-only mute', async () => {
    const { rtc, sentMic, screenAudio, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = present(display.getAudioTracks()[0])
    track.getSettings = () => ({ restrictOwnAudio: false, echoCancellation: false })
    track.applyConstraints = vi.fn(async () => { throw new DOMException('not supported', 'OverconstrainedError') })
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    const actionable = useToastStore().toasts.filter(toast => toast.action)
    expect(actionable).toHaveLength(1)
    expect(screenAudio.track).toBe(track)
    expect(track.enabled).toBe(true)
    present(present(actionable[0]).action).onClick()
    expect(voice.isScreenAudioMuted).toBe(true)
    expect(track.enabled).toBe(false)
    expect(sentMic.enabled).toBe(true)
    rtc.stopScreenShare()
    voice.isScreenAudioMuted = false
    present(present(actionable[0]).action).onClick()
    expect(voice.isScreenAudioMuted).toBe(false)
  })

  it('does not attach screen audio after leaving while its fallback constraints are pending', async () => {
    const { rtc, voice, screenAudio, sent } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = present(display.getAudioTracks()[0])
    let resolve: (() => void) | undefined
    track.getSettings = () => ({ restrictOwnAudio: false })
    track.applyConstraints = vi.fn(() => new Promise<void>(r => { resolve = r }))
    stubDisplayMedia(display)
    const starting = rtc.startScreenShare()
    await vi.waitFor(() => expect(resolve).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(resolve)()
    await starting
    expect(screenAudio.replaceTrack).not.toHaveBeenCalledWith(track)
    expect(voice.hasScreenAudio).toBe(false)
    expect(sent.some(event => event.type === 'webrtc_screenshare_start')).toBe(false)
  })

  it('mute and the gate only silence the mic, never the screen audio', async () => {
    const { rtc, voice, screenAudio, sentMic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleMute()
    expect(sentMic.enabled).toBe(false)
    expect(present(screenAudio.track).enabled).toBe(true)
  })

  it("the streamer's stream mute silences only the screen audio line", async () => {
    const { rtc, voice, audio, screenAudio, sentMic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(present(display.getAudioTracks()[0]).enabled).toBe(false))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    expect(audio.track).toBe(sentMic)
    expect(sentMic.enabled).toBe(true)
    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(present(display.getAudioTracks()[0]).enabled).toBe(true))
  })

  it('without screen audio the plain mic keeps being sent', async () => {
    const { rtc, audio, screenAudio, sentMic, voice } = await joined()
    const calls = audio.replaceTrack.mock.calls.length
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    expect(audio.replaceTrack.mock.calls.length).toBe(calls)
    expect(audio.track).toBe(sentMic)
    expect(screenAudio.track).toBeNull()
    expect(voice.hasScreenAudio).toBe(false)
  })

  it('empties the screen lines when sharing stops', async () => {
    const { rtc, audio, screen, screenAudio, sentMic, sent, voice } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()

    rtc.stopScreenShare()
    await vi.waitFor(() => expect(screenAudio.track).toBeNull())
    expect(screen.track).toBeNull()
    expect(audio.track).toBe(sentMic)
    expect(voice.hasScreenAudio).toBe(false)
    expect(sent.some(e => e.type === 'webrtc_screenshare_stop')).toBe(true)
  })

  it('silences the screen audio line when only the screen audio track ends', async () => {
    const { rtc, voice, screenAudio } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    present(present(display.getAudioTracks()[0]).onended).call(present(display.getAudioTracks()[0]), new Event('ended'))
    await vi.waitFor(() => expect(screenAudio.track).toBeNull())
    expect(voice.isScreenSharing).toBe(true)
    expect(voice.hasScreenAudio).toBe(false)
  })

  it('keeps the screen audio when audio settings are applied mid-share', async () => {
    const { rtc, audio, screenAudio, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    const first = rtc.applyAudioSettings()
    const second = rtc.applyAudioSettings()
    await grantMic()
    const lastMic = await grantMic()
    await Promise.all([first, second])

    expect(audio.track).toBe(present(toRaw(voice.localAudioStream)?.getAudioTracks()[0]))
    expect(audio.track).not.toBe(present(lastMic.getAudioTracks()[0]))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    expect(voice.hasScreenAudio).toBe(true)
  })

  it('a reconnect sends the screen audio on its line again', async () => {
    const { rtc, sentMic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    rtc.rejoinAfterReconnect()
    const fresh = await negotiate()
    expect(present(fresh.senders[0]).track).toBe(sentMic)
    expect(present(fresh.senders[1]).track).toBe(present(display.getVideoTracks()[0]))
    expect(present(fresh.senders[3]).track).toBe(present(display.getAudioTracks()[0]))
  })
})

describe('one screen share per person', () => {
  async function answer(result: boolean) {
    const { pendingConfirm } = await import('../lib/confirm')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    present(pendingConfirm.value).resolve(result)
  }

  it('starting another share asks first and then replaces the running one', async () => {
    const { rtc, voice, screen, screenAudio, sent } = await joined()
    const first = fakeStream(['video', 'audio'])
    stubDisplayMedia(first)
    await rtc.startScreenShare()
    const second = fakeStream(['video', 'audio'])
    stubDisplayMedia(second)

    const start = rtc.startScreenShare()
    await answer(true)
    await start

    expect(screen.track).toBe(present(second.getVideoTracks()[0]))
    expect(screenAudio.track).toBe(present(second.getAudioTracks()[0]))
    expect(first.getTracks().every(tr => vi.mocked(tr.stop).mock.calls.length > 0)).toBe(true)
    expect(toRaw(voice.localScreenStream)).toBe(second)
    expect(voice.isScreenSharing).toBe(true)
    // Still one share: it never stopped for the others.
    expect(sent.some(e => e.type === 'webrtc_screenshare_stop')).toBe(false)
  })

  it('keeps the running share when the question is declined', async () => {
    const { rtc, voice, screen } = await joined()
    const first = fakeStream(['video'])
    stubDisplayMedia(first)
    await rtc.startScreenShare()
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockClear()

    const start = rtc.startScreenShare()
    await answer(false)
    await start

    expect(navigator.mediaDevices.getDisplayMedia).not.toHaveBeenCalled()
    expect(screen.track).toBe(present(first.getVideoTracks()[0]))
    expect(present(first.getVideoTracks()[0]).stop).not.toHaveBeenCalled()
    expect(toRaw(voice.localScreenStream)).toBe(first)
  })
})

describe('stream quality of my own share', () => {
  // A 4K screen whose capture the browser cannot scale (settings stay 4K),
  // or one it scales to the constrained height.
  function display4k({ scales = false } = {}) {
    const stream = fakeStream(['video', 'audio'])
    const track = present(stream.getVideoTracks()[0])
    let settings = { width: 3840, height: 2160, frameRate: 60 }
    track.contentHint = ''
    track.getSettings = () => settings
    track.applyConstraints = vi.fn(async (c) => {
      if (!scales) return
      const h = c.height?.max ?? 2160
      settings = { width: Math.round(3840 * h / 2160), height: h, frameRate: c.frameRate?.max ?? 60 }
    })
    return { stream, track }
  }
  const lastParams = (sender: ReturnType<typeof fakeSender>) => present(present(sender.setParameters.mock.calls.at(-1))[0])

  it('captures and sends a new share at 1080p and 30 fps', async () => {
    const { rtc, screen, voice } = await joined()
    const { stream, track } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    // The very first share starts at the default.
    expect(voice.screenQuality).toEqual({ resolution: 1080, fps: 30 })
    expect(present(present(vi.mocked(navigator.mediaDevices.getDisplayMedia).mock.calls[0])[0]).video).toEqual({ frameRate: { ideal: 30, max: 30 }, height: { max: 1080 } })
    const enc = present(lastParams(screen).encodings[0])
    expect(enc.maxFramerate).toBe(30)
    // The browser did not scale the capture: the encoder does.
    expect(enc.scaleResolutionDownBy).toBe(2)
    expect(enc.maxBitrate).toBe(streamBitrate(DEFAULT_STREAM_QUALITY, { width: 3840, height: 2160 }))
    expect(track.contentHint).toBe('detail')
  })

  it('applies a new resolution and frame rate live, without a new capture', async () => {
    const { rtc, screen, voice } = await joined()
    const { stream, track } = display4k({ scales: true })
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    voice.setScreenQuality({ resolution: 720, fps: 15 })
    await vi.waitFor(() => expect(present(lastParams(screen).encodings[0]).maxFramerate).toBe(15))
    expect(track.applyConstraints).toHaveBeenLastCalledWith({ frameRate: { ideal: 15, max: 15 }, height: { max: 720 } })
    const enc = present(lastParams(screen).encodings[0])
    // The capture is 720p now: no extra scaling in the encoder.
    expect(enc.scaleResolutionDownBy).toBe(1)
    expect(enc.maxBitrate).toBe(streamBitrate({ resolution: 720, fps: 15 }, { width: 1280, height: 720 }))
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(1)
    expect(screen.track).toBe(track)
  })

  it('Gaming is smooth motion, Source lifts the size cap again', async () => {
    const { rtc, screen, voice } = await joined()
    const { stream, track } = display4k({ scales: true })
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    voice.setScreenQuality({ resolution: 1440, fps: 60 })
    await vi.waitFor(() => expect(present(lastParams(screen).encodings[0]).maxFramerate).toBe(60))
    expect(track.contentHint).toBe('motion')
    expect(lastParams(screen).degradationPreference).toBe('balanced')

    voice.setScreenQuality({ resolution: 'source', fps: 15 })
    await vi.waitFor(() => expect(present(lastParams(screen).encodings[0]).maxFramerate).toBe(15))
    expect(track.applyConstraints).toHaveBeenLastCalledWith({ frameRate: { ideal: 15, max: 15 } })
    expect(track.contentHint).toBe('detail')
    expect(lastParams(screen).degradationPreference).toBe('maintain-resolution')
    expect(present(lastParams(screen).encodings[0]).scaleResolutionDownBy).toBe(1)
  })

  it('a reconnect sends in the chosen quality again', async () => {
    const { rtc, voice } = await joined()
    const { stream } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    await new Promise(r => setTimeout(r, 0))

    rtc.rejoinAfterReconnect()
    const screen = present((await negotiate()).senders[1])
    await vi.waitFor(() => expect(screen.setParameters).toHaveBeenCalled())
    const enc = present(lastParams(screen).encodings[0])
    expect(enc.maxFramerate).toBe(15)
    expect(enc.scaleResolutionDownBy).toBe(3)
    expect(enc.maxBitrate).toBe(streamBitrate({ resolution: 720, fps: 15 }, { width: 3840, height: 2160 }))
  })

  it('stopping keeps the last choice as the start of the next share', async () => {
    const { rtc, voice } = await joined()
    stubDisplayMedia(display4k().stream)
    await rtc.startScreenShare()
    voice.setScreenQuality({ resolution: 1440, fps: 60 })
    rtc.stopScreenShare()
    expect(voice.screenQuality).toEqual({ resolution: 1440, fps: 60 })

    const next = display4k()
    stubDisplayMedia(next.stream)
    await rtc.startScreenShare()
    expect(present(present(vi.mocked(navigator.mediaDevices.getDisplayMedia).mock.calls.at(-1))[0]).video).toEqual({ frameRate: { ideal: 60, max: 60 }, height: { max: 1440 } })
  })

  it('changes quality and frame rate in a running share without picking the screen again', async () => {
    const { rtc, screen, voice, sent } = await joined()
    const { stream, track } = display4k({ scales: true })
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    const picks = vi.mocked(navigator.mediaDevices.getDisplayMedia).mock.calls.length
    const before = screen.setParameters.mock.calls.length
    sent.length = 0

    voice.setScreenQuality({ resolution: 720, fps: 60 })
    await vi.waitFor(() => expect(track.applyConstraints).toHaveBeenLastCalledWith({ frameRate: { ideal: 60, max: 60 }, height: { max: 720 } }))
    await vi.waitFor(() => expect(screen.setParameters.mock.calls.length).toBeGreaterThan(before))
    const enc = present(lastParams(screen).encodings[0])
    expect(enc.maxFramerate).toBe(60)
    expect(enc.maxBitrate).toBe(streamBitrate({ resolution: 720, fps: 60 }, { width: 1280, height: 720 }))
    expect(vi.mocked(navigator.mediaDevices.getDisplayMedia).mock.calls.length).toBe(picks)
    expect(screen.track).toBe(track)
    expect(track.stop).not.toHaveBeenCalled()
    expect(voice.isScreenSharing).toBe(true)
    // The viewers get a fresh picture at the new size (the server asks the sharer's own screen on screenshare_start).
    expect(sent.some(e => e.type === 'webrtc_screenshare_start')).toBe(true)
    expect(voice.screenQualityNeedsRestart).toBe(false)
  })

  it('switches between motion and clarity live and says so when the capture cannot be constrained', async () => {
    const { rtc, voice } = await joined()
    const { stream, track } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(track.contentHint).toBe('detail')

    voice.setScreenQuality({ resolution: 1080, fps: 30, optimize: 'motion' })
    await vi.waitFor(() => expect(track.contentHint).toBe('motion'))
    expect(voice.screenQualityNeedsRestart).toBe(false)

    track.applyConstraints = vi.fn(async () => { throw new Error('OverconstrainedError') })
    voice.setScreenQuality({ resolution: 720, fps: 30, optimize: 'motion' })
    await vi.waitFor(() => expect(voice.screenQualityNeedsRestart).toBe(true))
    // The share itself keeps running; a restart is offered, never done silently.
    expect(voice.isScreenSharing).toBe(true)
    rtc.stopScreenShare()
    expect(voice.screenQualityNeedsRestart).toBe(false)

    // A capture picked anew was made at the chosen quality: nothing is left to restart.
    voice.screenQualityNeedsRestart = true
    stubDisplayMedia(display4k().stream)
    await rtc.startScreenShare()
    expect(voice.screenQualityNeedsRestart).toBe(false)
  })

  it('reports what the screen sender sends', async () => {
    const { rtc, screen } = await joined()
    expect(await rtc.getScreenSendStats()).toBeNull()
    stubDisplayMedia(display4k().stream)
    await rtc.startScreenShare()
    const stat = { id: 'o1', type: 'outbound-rtp', kind: 'video', bytesSent: 10 }
    screen.getStats = vi.fn(async () => new Map([['o1', stat]]))
    expect(await rtc.getScreenSendStats()).toEqual([stat])
  })
})

describe('layers of my screen share', () => {
  it('binds the lower layers on the lines the server offers, with their layer in the seal', async () => {
    const call = installRecordingCall()
    const { pc } = await joinedWithLayers()
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'sendrecv'])
    // Mic, screen (layer 0), camera, screen sound, then layers 1 and 2.
    // Each line also says what it carries: 0 microphone, 1 camera, 2 screen, 3 screen sound.
    expect(call.protectSender.mock.calls.map(c => [(c as unknown[])[1], (c as unknown[])[2]])).toEqual([[0, undefined], [2, 0], [1, undefined], [3, undefined], [2, 1], [2, 2]])
  })

  it('leaves the lines alone when the server does not take layers', async () => {
    const call = installRecordingCall()
    const { pc, rtc, sent } = await joinedWithLayers({ announce: 0 })
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'recvonly', 'recvonly'])
    expect(call.protectSender).toHaveBeenCalledTimes(4)
    const { stream } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(present(pc.senders[4]).track).toBeNull()
  })

  it('sends a 4K share as three layers and declares them, only the best sent until viewers need more', async () => {
    const { rtc, sent, low1, low2, screen } = await joinedWithLayers()
    const { stream, track } = display(3840, 2160)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    expect(low1.track).toBe(track)
    expect(low2.track).toBe(track)
    expect(declared(sent)).toEqual([{ layers: [{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }, { width: 852, height: 480, fps: 30 }] }])
    // The best layer is the share as chosen (1080p30 of a 4K capture, scaled by the encoder).
    expect(encodingOf(screen)).toMatchObject({ active: true, scaleResolutionDownBy: 2, maxFramerate: 30 })
    // The lower layers scale the same capture further and wait until they are wanted.
    expect(encodingOf(low1)).toMatchObject({ active: false, scaleResolutionDownBy: 3, maxFramerate: 30 })
    expect(encodingOf(low2)).toMatchObject({ active: false, scaleResolutionDownBy: 4.5, maxFramerate: 30 })
    expect(present(encodingOf(low1).maxBitrate)).toBeLessThan(present(encodingOf(screen).maxBitrate))
    expect(present(encodingOf(low2).maxBitrate)).toBeLessThan(present(encodingOf(low1).maxBitrate))
  })

  it('sends only the layers some viewer needs', async () => {
    const { rtc, voice, low1, low2, screen } = await joinedWithLayers()
    stubDisplayMedia(display(3840, 2160).stream)
    await rtc.startScreenShare()

    voice.layersWanted = [0, 2]
    await vi.waitFor(() => expect(encodingOf(low2).active).toBe(true))
    expect(encodingOf(screen).active).toBe(true)
    expect(encodingOf(low1).active).toBe(false)

    // Everybody moved to the lower layers: the best one is not encoded at all.
    voice.layersWanted = [1, 2]
    await vi.waitFor(() => expect(encodingOf(screen).active).toBe(false))
    expect(encodingOf(low1).active).toBe(true)
    expect(encodingOf(low2).active).toBe(true)
  })

  it('stopping the share empties the layers, forgets what was wanted and declares again next time', async () => {
    const { rtc, voice, sent, low1, low2, screen } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    await rtc.startScreenShare()
    voice.layersWanted = [1]
    await vi.waitFor(() => expect(encodingOf(low1).active).toBe(true))
    expect(encodingOf(screen).active).toBe(false)

    rtc.stopScreenShare()
    await vi.waitFor(() => expect(low1.track).toBeNull())
    expect(low2.track).toBeNull()
    expect(voice.layersWanted).toBeNull()
    // The next share starts on its best layer.
    await vi.waitFor(() => expect(encodingOf(screen).active).toBe(true))

    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(low1.track).toBe(track)
    expect(declared(sent)).toHaveLength(2)
    expect(encodingOf(low1).active).toBe(false)
  })

  it('re-declares and re-tunes when the quality changes, and drops layers that are no longer worth sending', async () => {
    const { rtc, voice, sent, low1, low2 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    track.applyConstraints = vi.fn(async () => {})
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toHaveLength(1)
    expect(low2.track).toBe(track)

    // The capture is 360p now: nothing smaller to send.
    track.getSettings = () => ({ width: 640, height: 360, frameRate: 30 })
    voice.setScreenQuality({ resolution: 720, fps: 30 })
    await vi.waitFor(() => expect(declared(sent)).toHaveLength(2))
    expect(declared(sent)[1]).toEqual({ layers: [{ width: 640, height: 360, fps: 30 }] })
    expect(low1.track).toBeNull()
    expect(low2.track).toBeNull()

    // Unchanged layers are not declared twice.
    voice.setScreenQuality({ resolution: 720, fps: 30, custom: true })
    await new Promise(r => setTimeout(r, 10))
    expect(declared(sent)).toHaveLength(2)
  })

  it('does not declare layers while the capture size is unknown', async () => {
    const { rtc, sent, low1 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    track.getSettings = () => ({})
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(low1.track).toBeNull()
  })

  describe('when the captured window changes size', () => {
    async function sharing() {
      const ctx = await joinedWithLayers()
      vi.useFakeTimers()
      const shown = display(1920, 1080)
      stubDisplayMedia(shown.stream)
      await ctx.rtc.startScreenShare()
      expect(declared(ctx.sent)).toHaveLength(1)
      const resize = (width: number, height: number, frameRate = 30) => {
        shown.track.getSettings = () => ({ width, height, frameRate })
      }
      return { ...ctx, resize }
    }

    it('declares the layers again, and sizes the best layer for the new picture', async () => {
      const { sent, screen, resize } = await sharing()
      expect(encodingOf(screen)).toMatchObject({ scaleResolutionDownBy: 1 })
      resize(1280, 720)
      // Nothing happens before the change has held.
      await vi.advanceTimersByTimeAsync(CAPTURE_SETTLE_MS - 100)
      expect(declared(sent)).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(2)
      expect(declaredLayersOf(sent, 1).map(l => l.height)).toEqual([720, 480, 360])
      // A bigger picture is scaled down to the chosen quality again.
      resize(3840, 2160)
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(3)
      expect(encodingOf(screen)).toMatchObject({ scaleResolutionDownBy: 2 })
    })

    it('waits until the size stops changing', async () => {
      const { sent, resize } = await sharing()
      for (const [w, h] of [[1700, 1000], [1500, 900], [1280, 720]] as const) {
        resize(w, h)
        await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + 100)
      }
      expect(declared(sent)).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(CAPTURE_SETTLE_MS + CAPTURE_POLL_MS)
      expect(declared(sent)).toHaveLength(2)
      expect(declaredLayersOf(sent, 1)[0]).toMatchObject({ width: 1280, height: 720 })
    })

    it('re-tunes for a change of the frame rate and ignores noise below a whole frame', async () => {
      const { sent, screen, resize } = await sharing()
      const tuned = screen.setParameters.mock.calls.length
      resize(1920, 1080, 30.4)
      await vi.advanceTimersByTimeAsync(3 * CAPTURE_SETTLE_MS)
      expect(screen.setParameters.mock.calls.length).toBe(tuned)
      resize(1920, 1080, 24)
      await vi.advanceTimersByTimeAsync(3 * CAPTURE_SETTLE_MS)
      expect(screen.setParameters.mock.calls.length).toBeGreaterThan(tuned)
      // The declared layers are the chosen quality's, which did not change.
      expect(declared(sent)).toHaveLength(1)
    })

    it('stops watching when the share stops', async () => {
      const { rtc, sent, resize } = await sharing()
      rtc.stopScreenShare()
      resize(1280, 720)
      await vi.advanceTimersByTimeAsync(5 * CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(1)
      // A change that was waiting to settle is dropped with the share.
      const again = display(1920, 1080)
      stubDisplayMedia(again.stream)
      await rtc.startScreenShare()
      again.track.getSettings = () => ({ width: 800, height: 600, frameRate: 30 })
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + 100)
      rtc.stopScreenShare()
      await vi.advanceTimersByTimeAsync(5 * CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(2)
    })
  })

  it('a reconnect restores the layers on the new connection', async () => {
    const { rtc, voice, sent } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    await rtc.startScreenShare()
    voice.serverScreenLayers = 3
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_LAYER_OFFER } })
    await vi.waitFor(() => expect(pc.transceivers.length).toBe(6))
    const low1 = present(pc.senders[4])
    await vi.waitFor(() => expect(low1.track).not.toBeNull())
    expect(encodingOf(low1)).toMatchObject({ active: false })
    expect(declared(sent)).toHaveLength(2)
  })

  it('sends only the layer lines the server announced', async () => {
    const { rtc, low1, low2, sent } = await joinedWithLayers({ announce: 2 })
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    // One layer line: the second layer of the plan has no sender.
    expect(low1.track).toBe(track)
    expect(low2.track).toBeNull()
    expect(declared(sent)).toHaveLength(1)
  })

  it('a share that is stopped while its layers are being set up is not resumed', async () => {
    const { rtc, low1, low2 } = await joinedWithLayers()
    const { stream } = display(1920, 1080)
    stubDisplayMedia(stream)
    low1.setParameters.mockImplementation(async () => { rtc.stopScreenShare() })
    await rtc.startScreenShare()
    await vi.waitFor(() => expect(low1.track).toBeNull())
    expect(low2.track).toBeNull()
  })

  it('does not plan layers for a track that cannot tell its size', async () => {
    const { rtc, low1, sent } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    Reflect.deleteProperty(track, 'getSettings')
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(low1.track).toBeNull()
  })

  it('leaving the call during a layer update leaves the lower senders alone', async () => {
    const { rtc, low1 } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    const sharing = rtc.startScreenShare()
    rtc.leaveVoiceChannel()
    await sharing
    expect(low1.track).toBeNull()
  })
})
