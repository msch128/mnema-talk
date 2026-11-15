import { describe, it, expect, vi } from 'vitest'
import { present } from '../../media-test.fixture'
import { toRaw } from 'vue'
import { screenAudioConstraints } from './useWebRTC'
import { useToastStore } from '../../stores/toast'
import { streamBitrate, DEFAULT_STREAM_QUALITY } from '../lib/streamQuality'
import { installHarness, FakeAudioContext, fakeSender, fakeStream, grantMic, joined, negotiate, stubDisplayMedia } from './useWebRTC.harness.fixture'

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
    const { pendingConfirm } = await import('../../shared/lib/confirm')
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

