import { describe, it, expect, vi } from 'vitest'
import { TestStream, present } from '../media-test.fixture'
import { toRaw, nextTick } from 'vue'
import { t } from '../i18n'
import { SCREEN_MAX_BITRATE, CAMERA_MAX_BITRATE } from './useWebRTC'
import { useToastStore } from '../stores/toast'
import { streamBitrate, DEFAULT_STREAM_QUALITY } from '../lib/streamQuality'
import { installHarness, FakeAudioContext, FakePC, audioElements, fakeStream, fakeTrack, grantMic, joined, micRequests, negotiate, remoteStreamFor, setup, stubDisplayMedia } from './useWebRTC.harness.fixture'
import { suppressor, type SuppressorDouble } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('webcam', () => {
  it('sends the camera on its own line, next to a screen share', async () => {
    const { rtc, voice, screen, camera, sent } = await joined()
    const cam = fakeStream(['video'])
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => cam)
    stubDisplayMedia(fakeStream(['video']))

    await rtc.startCamera()
    expect(present(present(vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.at(-1))[0]).video).toBeTruthy()
    expect(camera.track).toBe(present(cam.getVideoTracks()[0]))
    expect(voice.isCameraOn).toBe(true)

    await rtc.startScreenShare()
    expect(screen.track).not.toBe(camera.track)
    expect(camera.track).toBe(present(cam.getVideoTracks()[0]))

    rtc.stopCamera()
    expect(camera.track).toBeNull()
    expect(screen.track).not.toBeNull()
    expect(voice.isCameraOn).toBe(false)
    expect(present(cam.getVideoTracks()[0]).stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'webrtc_camera_stop')).toBe(true)
  })

  it('caps the bitrate and keeps the screen resolution under congestion', async () => {
    const { rtc, screen, camera } = await joined()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => fakeStream(['video']))
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    await rtc.startCamera()

    const screenParams = present(present(screen.setParameters.mock.calls[0])[0])
    expect(present(screenParams.encodings[0]).maxBitrate).toBe(streamBitrate(DEFAULT_STREAM_QUALITY))
    expect(present(screenParams.encodings[0]).maxBitrate).toBeLessThanOrEqual(SCREEN_MAX_BITRATE)
    expect(screenParams.degradationPreference).toBe('maintain-resolution')
    expect(present(present(present(camera.setParameters.mock.calls[0])[0]).encodings[0]).maxBitrate).toBe(CAMERA_MAX_BITRATE)
  })

  it('tunes the senders again after a reconnect', async () => {
    const { rtc, voice } = await joined()
    voice.qosHighPriority = true
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => fakeStream(['video']))
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    await rtc.startCamera()

    rtc.rejoinAfterReconnect()
    const senders = (await negotiate()).senders
    const audio = present(senders[0]), screen = present(senders[1]), camera = present(senders[2])
    await vi.waitFor(() => expect(camera.setParameters).toHaveBeenCalled())
    const screenParams = present(present(screen.setParameters.mock.calls.at(-1))[0])
    expect(present(screenParams.encodings[0]).maxBitrate).toBe(streamBitrate(DEFAULT_STREAM_QUALITY))
    expect(screenParams.degradationPreference).toBe('maintain-resolution')
    expect(present(present(present(camera.setParameters.mock.calls.at(-1))[0]).encodings[0]).maxBitrate).toBe(CAMERA_MAX_BITRATE)
    expect(present(present(present(audio.setParameters.mock.calls.at(-1))[0]).encodings[0]).priority).toBe('high')
  })

  it('still shares when the browser rejects the sender parameters', async () => {
    const { rtc, screen } = await joined()
    screen.setParameters.mockRejectedValue(new Error('unsupported'))
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    expect(screen.track).not.toBeNull()
  })

  it('names the actual reason when the camera fails', async () => {
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    const { rtc, voice } = await joined()
    const toasts = useToastStore()
    const spy = vi.spyOn(toasts, 'error')
    for (const [name, key] of [
      ['NotAllowedError', 'voice.cameraDenied'],
      ['NotFoundError', 'voice.cameraNotFound'],
      ['NotReadableError', 'voice.cameraBusy'],
      ['TypeError', 'voice.cameraFailed']
    ]) {
      vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => {
        throw Object.assign(new Error(name), { name })
      })
      await rtc.startCamera()
      expect(spy).toHaveBeenLastCalledWith(t(present(key)))
      expect(voice.isCameraOn).toBe(false)
    }
  })

  it('prefers the chosen camera without requiring it, and restarts a running camera on a new choice', async () => {
    const { rtc, voice, sent } = await joined()
    voice.selectedCameraDeviceId = 'cam-2'
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => fakeStream(['video']))
    await rtc.startCamera()
    expect(present(present(vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.at(-1))[0]).video).toMatchObject({ deviceId: { ideal: 'cam-2' } })

    voice.selectedCameraDeviceId = 'cam-3'
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => fakeStream(['video']))
    sent.length = 0
    await rtc.restartCamera()
    // Viewers need a fresh picture from the new device: the server must ask our own camera, not the videos we watch.
    expect(sent).toContainEqual({ type: 'webrtc_request_keyframe', payload: { source: 'camera' } })
    expect(sent).not.toContainEqual({ type: 'webrtc_request_keyframe', payload: {} })
    expect(present(present(vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.at(-1))[0]).video).toMatchObject({ deviceId: { ideal: 'cam-3' } })
    expect(voice.isCameraOn).toBe(true)

    // A stopped camera is not started by a changed choice; it uses it next time.
    rtc.stopCamera()
    const calls = vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.length
    await rtc.restartCamera()
    expect(vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.length).toBe(calls)
    expect(voice.isCameraOn).toBe(false)
  })

  it('does not start a camera outside a call', async () => {
    const { rtc, voice } = setup()
    await rtc.startCamera()
    expect(navigator.mediaDevices.getUserMedia).not.toHaveBeenCalled()
    expect(voice.isCameraOn).toBe(false)
  })

  it('stops the camera on leave', async () => {
    const { rtc, voice } = await joined()
    const cam = fakeStream(['video'])
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => cam)
    await rtc.startCamera()
    rtc.leaveVoiceChannel()
    expect(present(cam.getVideoTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.isCameraOn).toBe(false)
  })

  it('maps remote cameras and screen shares to their publishers', async () => {
    const { voice, pc } = await joined()
    const camTrack = fakeTrack('video')
    present(pc.ontrack)({ track: camTrack, streams: [remoteStreamFor('cam:alice')] })
    expect(Object.keys(voice.userVideoStreams)).toEqual(['alice'])
    expect(voice.remoteScreenStream).toBeNull()

    // A screen share nobody opted into is not shown.
    present(pc.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('bob')] })
    expect(voice.remoteScreenStream).toBeNull()
    voice.watchScreen('bob')
    present(pc.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('bob')] })
    expect(voice.remoteScreenUserId).toBe('bob')
    expect(voice.remoteScreenStream).not.toBeNull()

    present(camTrack.onended).call(camTrack, new Event('ended'))
    expect(voice.userVideoStreams.alice).toBeUndefined()
  })
})

describe('mic test and loopback', () => {
  it('mutes outgoing mic in channel during mic test and restores on stop', async () => {
    const { rtc, voice } = await joined()
    const track = present(present(voice.localAudioStream).getAudioTracks()[0])
    expect(track.enabled).toBe(true)

    await rtc.toggleMicTest()
    expect(voice.isMicTesting).toBe(true)
    expect(track.enabled).toBe(false)

    rtc.toggleMicTest()
    expect(voice.isMicTesting).toBe(false)
    expect(track.enabled).toBe(true)
  })

  it('toggles mic test loopback state', async () => {
    const { rtc, voice } = await joined()
    expect(voice.isMicTesting).toBe(false)

    await rtc.toggleMicTest()
    expect(voice.isMicTesting).toBe(true)

    rtc.toggleMicTest()
    expect(voice.isMicTesting).toBe(false)
  })
})

describe('standalone mic test', () => {
  it('closing the settings while the mic prompt is open releases the mic', async () => {
    const { rtc, voice } = setup()
    const start = rtc.startMicTest()
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    rtc.stopMicTest()
    const stream = await grantMic()
    await start
    expect(present(stream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(FakeAudioContext.instances).toHaveLength(0)
    expect(voice.currentInputLevel).toBe(0)
  })

  it('a second start supersedes a pending first one', async () => {
    const { rtc } = setup()
    const first = rtc.startMicTest()
    const second = rtc.startMicTest()
    const firstStream = await grantMic()
    const secondStream = await grantMic()
    await Promise.all([first, second])
    expect(present(firstStream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(secondStream.getTracks()[0]).stop).not.toHaveBeenCalled()
    expect(FakeAudioContext.instances).toHaveLength(1)

    rtc.stopMicTest()
    expect(present(secondStream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(FakeAudioContext.instances[0]).closed).toBe(true)
  })

  it('builds the loopback once, also when it starts the test itself', async () => {
    const { rtc, voice } = setup()
    const toggle = rtc.toggleMicTest()
    await grantMic()
    await toggle
    expect(voice.isMicTesting).toBe(true)
    expect(FakeAudioContext.gains).toHaveLength(1)
  })

  it('plays the loopback on the chosen output device', async () => {
    const { rtc, voice } = setup()
    voice.selectedOutputDeviceId = 'headset'
    const start = rtc.startMicTest()
    await grantMic()
    await start
    await rtc.toggleMicTest()
    expect(FakeAudioContext.gains).toHaveLength(1)
    expect(present(FakeAudioContext.instances[0]).sinkId).toBe('headset')
  })

  it('push-to-talk works in the test outside a call, and stops with it', async () => {
    const { rtc, voice } = setup()
    voice.inputMode = 'ptt'
    voice.pttKey = 'Space'
    const start = rtc.startMicTest()
    await grantMic()
    await start
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'Space' }))

    rtc.stopMicTest()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(false)
  })
})

describe('permission and microphone switching outcomes', () => {
  it('refreshes only audio device lists and survives unavailable enumeration', async () => {
    const { rtc, voice } = setup()
    const device = (kind: MediaDeviceKind, deviceId: string): MediaDeviceInfo => ({ kind, deviceId, groupId: '', label: deviceId, toJSON: () => ({ kind, deviceId }) })
    const mic = device('audioinput', 'microphone')
    const speaker = device('audiooutput', 'speaker')
    vi.mocked(navigator.mediaDevices.enumerateDevices).mockResolvedValue([mic, speaker, device('videoinput', 'camera')])
    await rtc.refreshAudioDevices()
    expect(voice.availableInputDevices).toEqual([mic])
    expect(voice.availableOutputDevices).toEqual([speaker])
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.enumerateDevices).mockRejectedValue(new Error('permission'))
    await rtc.refreshAudioDevices()
    expect(voice.availableInputDevices).toEqual([mic])
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: undefined })
    await rtc.refreshAudioDevices()
  })

  it('joins without a microphone when permission is denied and can negotiate receive-only media', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new DOMException('denied', 'NotAllowedError'))
    const { rtc, voice, sent } = setup()
    await rtc.joinVoiceChannel('silent')
    expect(voice.localAudioStream).toBeNull()
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'silent', tab: expect.any(String) } })
    const pc = await negotiate()
    expect(pc.senders.every(sender => sender.track === null)).toBe(true)
  })

  it('avoids reacquiring an already joined microphone and sends exact selected device constraints', async () => {
    const { rtc, voice } = setup()
    voice.selectedInputDeviceId = 'selected-mic'
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledWith({ audio: expect.objectContaining({ deviceId: { exact: 'selected-mic' } }) })
    const before = FakeAudioContext.instances.length
    await rtc.joinVoiceChannel('ch-1')
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
    expect(FakeAudioContext.instances).toHaveLength(before)
  })

  it('keeps the current microphone when a settings replacement is denied', async () => {
    const { rtc, voice, mic } = await joined()
    const old = voice.localAudioStream
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue(new Error('busy'))
    await rtc.applyAudioSettings()
    expect(voice.localAudioStream).toBe(old)
    expect(present(mic.getTracks()[0]).stop).not.toHaveBeenCalled()
  })

  it('discards a settings capture after leaving and drains queued settings without another capture', async () => {
    const { rtc } = await joined()
    const first = rtc.applyAudioSettings()
    const queued = rtc.applyAudioSettings()
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    rtc.leaveVoiceChannel()
    const stale = await grantMic()
    await Promise.all([first, queued])
    expect(present(stale.getTracks()[0]).stop).toHaveBeenCalled()
    expect(micRequests).toEqual([])
  })

  it('swaps a muted microphone on its negotiated line without another offer', async () => {
    const { rtc, voice, mic, sent } = await joined()
    voice.isMuted = true
    const pc = present(FakePC.instances.at(-1))
    const swap = rtc.applyAudioSettings()
    const replacement = await grantMic()
    await swap
    expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
    const sentTrack = present(toRaw(voice.localAudioStream)?.getAudioTracks()[0])
    expect(present(pc.senders[0]).track).toBe(sentTrack)
    // The muted state is on the sent track; the capture behind it keeps feeding the meter.
    expect(sentTrack.enabled).toBe(false)
    expect(present(replacement.getAudioTracks()[0]).enabled).toBe(true)
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
  })

  it('starts and stops a standalone test through audio settings while applying senderless QoS', async () => {
    const { rtc, voice } = setup()
    const update = rtc.applyAudioSettings()
    const stream = await grantMic()
    await update
    expect(FakeAudioContext.instances).toHaveLength(1)
    expect(voice.currentChannelId).toBeNull()
    rtc.stopMicTest()
    expect(present(stream.getTracks()[0]).stop).toHaveBeenCalled()
  })
})

describe('capture lifecycle wrappers and statistics', () => {
  it('starts sharing via replacement when there is no current capture and toggles it off', async () => {
    const { rtc, voice } = await joined()
    const stream = fakeStream(['video'])
    const track = present(stream.getVideoTracks()[0])
    track.getSettings = () => ({ width: 2560, height: 1440, frameRate: 60, deviceId: 'capture-private-device' })
    expect(rtc.getScreenCaptureSettings()).toBeNull()
    stubDisplayMedia(stream)
    await rtc.replaceScreenShare()
    expect(rtc.getScreenCaptureSettings()).toEqual({ width: 2560, height: 1440, frameRate: 60 })
    rtc.toggleScreenShare()
    expect(voice.isScreenSharing).toBe(false)
    await rtc.toggleScreenShare()
    expect(voice.isScreenSharing).toBe(true)
    track.getSettings = () => ({})
    expect(rtc.getScreenCaptureSettings()).toEqual({})
  })

  it('returns null after sender statistics fail and continues capture after constraint failure', async () => {
    const { rtc, voice, screen } = await joined()
    const stream = fakeStream(['video'])
    const track = present(stream.getVideoTracks()[0])
    vi.spyOn(track, 'applyConstraints').mockRejectedValue(new Error('unsupported size'))
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    screen.getStats.mockRejectedValue(new Error('closing'))
    expect(await rtc.getScreenSendStats()).toBeNull()
    const debug = vi.spyOn(console, 'debug').mockImplementation(() => {})
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    await vi.waitFor(() => expect(debug).toHaveBeenCalled())
    await vi.waitFor(() => expect(screen.setParameters).toHaveBeenLastCalledWith(expect.objectContaining({ encodings: [expect.objectContaining({ maxFramerate: 15 })] })))
    expect(screen.track).toBe(track)
  })

  it('preserves the running share if the replacement picker is cancelled and discards a late replacement', async () => {
    const { rtc, voice } = await joined()
    const first = fakeStream(['video', 'audio'])
    stubDisplayMedia(first)
    await rtc.startScreenShare()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockRejectedValueOnce(new Error('cancel'))
    await rtc.replaceScreenShare()
    expect(toRaw(voice.localScreenStream)).toBe(first)
    expect(warn).toHaveBeenCalled()
    let finish: ((stream: MediaStream) => void) | undefined
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const pending = rtc.replaceScreenShare()
    rtc.stopScreenShare()
    const late = fakeStream(['video'])
    present(finish)(late)
    await pending
    expect(present(late.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.isScreenSharing).toBe(false)
  })

  it('does not resurrect an ended screen when its old video ended callback fires', async () => {
    const { rtc, voice } = await joined()
    const old = fakeStream(['video'])
    stubDisplayMedia(old)
    await rtc.startScreenShare()
    const track = present(old.getVideoTracks()[0])
    const ended = present(track.onended)
    rtc.stopScreenShare()
    ended.call(track, new Event('ended'))
    expect(voice.isScreenSharing).toBe(false)
    const replacement = fakeStream(['video'])
    stubDisplayMedia(replacement)
    await rtc.startScreenShare()
    ended.call(track, new Event('ended'))
    expect(voice.isScreenSharing).toBe(true)
    const current = present(replacement.getVideoTracks()[0])
    present(current.onended).call(current, new Event('ended'))
    expect(voice.isScreenSharing).toBe(false)
  })

  it('toggles camera capture and stops it when the source ends', async () => {
    const { rtc, voice } = await joined()
    const stream = fakeStream(['video'])
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(stream)
    await rtc.toggleCamera()
    expect(voice.isCameraOn).toBe(true)
    const track = present(stream.getVideoTracks()[0])
    present(track.onended).call(track, new Event('ended'))
    expect(voice.isCameraOn).toBe(false)
    await rtc.toggleCamera()
    rtc.toggleCamera()
    expect(voice.isCameraOn).toBe(false)
  })

  it('discards duplicate camera permission results and accepts an empty browser capture', async () => {
    const { rtc, voice } = await joined()
    const pending = rtc.startCamera()
    const duplicate = rtc.startCamera()
    const first = await grantMic()
    const second = await grantMic()
    await Promise.all([pending, duplicate])
    expect(present(second.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(first.getTracks()[0]).stop).not.toHaveBeenCalled()
    rtc.stopCamera()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(new TestStream())
    await rtc.startCamera()
    expect(voice.isCameraOn).toBe(true)
  })
})

describe('available media after device removal', () => {
  it('keeps gate and loopback controls safe after the browser removes its microphone track', async () => {
    vi.useFakeTimers()
    const { rtc, voice, sent } = setup()
    const raw = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(raw)
    await rtc.joinVoiceChannel('removed-device')
    const track = present(raw.getAudioTracks()[0])
    raw.removeTrack(track)
    voice.isMuted = false
    await vi.advanceTimersByTimeAsync(60)
    expect(sent).toContainEqual({ type: 'voice_speaking', payload: { active: true } })
    voice.isMicTesting = true
    await vi.advanceTimersByTimeAsync(60)
    expect(sent.at(-1)).toEqual({ type: 'voice_speaking', payload: { active: false } })
    voice.isMicTesting = false
    voice.isMuted = true
    await vi.advanceTimersByTimeAsync(60)
    voice.isMuted = false
    await rtc.toggleMicTest()
    await vi.advanceTimersByTimeAsync(60)
    rtc.stopMicTest()
    expect(voice.isMicTesting).toBe(false)
  })

  it('continues an empty display stream until the user stops it', async () => {
    const { rtc, voice } = await joined()
    stubDisplayMedia(new TestStream())
    await rtc.startScreenShare()
    expect(voice.isScreenSharing).toBe(true)
    expect(voice.hasScreenAudio).toBe(false)
    expect(rtc.getScreenCaptureSettings()).toBeNull()
    rtc.stopScreenShare()
  })

  it('maps primitive camera failures to the generic error', async () => {
    const { rtc, voice } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.mocked(navigator.mediaDevices.getUserMedia).mockRejectedValue('capture unavailable')
    await rtc.startCamera()
    expect(voice.isCameraOn).toBe(false)
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('voice.cameraFailed'))
  })

  it('ignores contenteditable push-to-talk and releases with a matching key while running a meter without loopback', async () => {
    const { rtc, voice } = setup()
    voice.inputMode = 'ptt'
    voice.pttKey = 'Space'
    const start = rtc.startMicTest()
    await grantMic()
    await start
    const editable = document.createElement('div')
    Object.defineProperty(editable, 'isContentEditable', { configurable: true, value: true })
    document.body.append(editable)
    editable.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space', bubbles: true }))
    expect(voice.isPttPressed).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(false)
    editable.remove()
  })

  it('resumes paused remote audio on visibility return while a running microphone needs no resume', async () => {
    const { pc } = await joined()
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    const el = present(audioElements.at(-1))
    Object.defineProperty(el, 'paused', { configurable: true, value: false })
    el.play = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(el.play).not.toHaveBeenCalled()
  })

  it('rebuilds a closed playback context for a newly amplified speaker', async () => {
    const { pc, voice } = await joined()
    voice.setUserVolume('alice', 150)
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    const previous = present(FakeAudioContext.instances.at(-1))
    previous.state = 'closed'
    voice.setUserVolume('bob', 160)
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('bob')] })
    expect(FakeAudioContext.instances.at(-1)).not.toBe(previous)
    expect(present(FakeAudioContext.gains.at(-1)).gain.value).toBe(1.6)
  })

  it('lets a current running call reuse its meter context and tolerate absent loopback gain', async () => {
    const { rtc, voice } = await joined()
    expect(await rtc.startMicTest()).toBe(true)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(present(FakeAudioContext.instances.at(-1)), 'createGain').mockImplementation(() => { throw new Error('monitor unavailable') })
    expect(await rtc.toggleMicTest()).toBe(false)
    expect(warn).toHaveBeenCalled()
    rtc.stopMicTest()
    expect(voice.isMicTesting).toBe(false)
  })

  it('does not replace a missing microphone interval after a graph failure', async () => {
    const { rtc, voice } = setup()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const analyser = vi.spyOn(FakeAudioContext.prototype, 'createAnalyser').mockImplementation(() => { throw new Error('no meter') })
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    analyser.mockRestore()
    const settings = rtc.applyAudioSettings()
    await grantMic()
    await settings
    expect(voice.localAudioStream).not.toBeNull()
  })

  it.each([null, {}, { codecs: [] }])('negotiates when capability lookup yields %j', async caps => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => caps })
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = await negotiate()
    expect(pc.remoteDescription?.sdp).toContain('m=video')
  })
})

describe('loopback microphone changes and cancellation errors', () => {
  it('rebuilds a call microphone during monitoring and retains a single output monitor', async () => {
    const { rtc, voice } = await joined()
    await rtc.toggleMicTest()
    const oldGain = present(FakeAudioContext.gains.at(-1))
    const oldContext = present(FakeAudioContext.instances.at(-1))
    const settings = rtc.applyAudioSettings()
    await grantMic()
    await settings
    expect(oldGain.disconnect).toHaveBeenCalled()
    expect(oldContext.closed).toBe(true)
    expect(voice.isMicTesting).toBe(true)
    expect(FakeAudioContext.gains.at(-1)).not.toBe(oldGain)
    rtc.stopMicTest()
    expect(voice.isMicTesting).toBe(false)
  })

  it('stops a microphone replacement while its AI model is initializing', async () => {
    const { rtc, voice } = await joined()
    suppressor.supported = true
    let finish: ((node: SuppressorDouble) => void) | undefined
    suppressor.create = () => new Promise(resolve => { finish = resolve })
    voice.noiseMode = 'ai'
    const settings = rtc.applyAudioSettings()
    const replacement = await grantMic()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(finish)({ connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() })
    await settings
    expect(present(replacement.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.localAudioStream).toBeNull()
  })

  it('ignores a standalone microphone rejection after the settings were closed', async () => {
    const { rtc, voice } = setup()
    let reject: ((error: Error) => void) | undefined
    vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementation(() => new Promise((_resolve, fail) => { reject = fail }))
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const start = rtc.startMicTest()
    rtc.stopMicTest()
    voice.currentInputLevel = 33
    present(reject)(new Error('picker was closed'))
    expect(await start).toBe(false)
    expect(voice.currentInputLevel).toBe(33)
  })

  it('keeps call PTT listeners if a standalone meter finishes while a channel join is awaiting permission', async () => {
    const { rtc, voice } = setup()
    voice.inputMode = 'ptt'
    const standalone = rtc.startMicTest()
    await grantMic()
    await standalone
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    rtc.stopMicTest()
    window.dispatchEvent(new KeyboardEvent('keydown', { code: voice.pttKey }))
    expect(voice.isPttPressed).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keyup', { code: voice.pttKey }))
    expect(voice.isPttPressed).toBe(false)
  })

  it('releases a pressed PTT key even when keyup arrives after the channel disconnects', async () => {
    const { voice } = await joined()
    voice.inputMode = 'ptt'
    voice.isPttPressed = true
    voice.isConnected = false
    window.dispatchEvent(new KeyboardEvent('keyup', { code: voice.pttKey }))
    expect(voice.isPttPressed).toBe(false)
  })

  it('suspends channel transmission while mic testing is activated before its loopback is created', async () => {
    vi.useFakeTimers()
    const { rtc, voice } = setup()
    const stream = fakeStream()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(stream)
    await rtc.joinVoiceChannel('mic-test-transition')
    const track = present(toRaw(voice.localAudioStream)?.getAudioTracks()[0])
    voice.isMicTesting = true
    expect(track.enabled).toBe(true)
    await vi.advanceTimersByTimeAsync(60)
    expect(track.enabled).toBe(false)
  })

  it('changes input volume outside a call without rebuilding the pipeline', async () => {
    const { voice } = setup()
    voice.inputVolume = 75
    await nextTick()
    expect(navigator.mediaDevices.getUserMedia).not.toHaveBeenCalled()
  })

  it('reports watched stream diagnostics without the own-share audio guard', async () => {
    const { rtc, voice, sent } = await joined()
    vi.useFakeTimers()
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    voice.handleMediaState({ channel_id: 'v1', user_id: 'alice', screen: true })
    voice.watchScreen('alice')
    voice.setRemoteScreen('alice', fakeStream(['video']))
    pc.getStats = vi.fn(async () => new Map([['i', { id: 'i', type: 'inbound-rtp', kind: 'video', bytesReceived: 1000 }]]))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(sent.filter(event => event.type === 'webrtc_diag' && 'event' in event.payload && event.payload.event === 'stream').at(-1)).toMatchObject({ type: 'webrtc_diag', payload: { event: 'stream', sharing: false, own_audio: undefined } })
    pc.getStats = vi.fn(async () => new Map())
    const count = sent.filter(event => event.type === 'webrtc_diag').length
    await vi.advanceTimersByTimeAsync(10_000)
    expect(sent.filter(event => event.type === 'webrtc_diag')).toHaveLength(count)
  })

  it('ignores an old screen-audio ended callback after the screen has been replaced', async () => {
    const { rtc, voice, screenAudio } = await joined()
    const previous = fakeStream(['video', 'audio'])
    stubDisplayMedia(previous)
    await rtc.startScreenShare()
    const previousAudio = present(previous.getAudioTracks()[0])
    const ended = present(previousAudio.onended)
    const replacement = fakeStream(['video', 'audio'])
    stubDisplayMedia(replacement)
    await rtc.replaceScreenShare()
    ended.call(previousAudio, new Event('ended'))
    expect(voice.hasScreenAudio).toBe(true)
    expect(screenAudio.track).toBe(replacement.getAudioTracks()[0])
  })
})

describe('standalone meter without audio monitoring', () => {
  it('updates only the input meter and handles visibility return without a channel', async () => {
    vi.useFakeTimers()
    const { rtc, voice } = setup()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    expect(await rtc.startMicTest()).toBe(true)
    await vi.advanceTimersByTimeAsync(50)
    expect(voice.currentInputLevel).toBeGreaterThan(0)
    expect(voice.isMicTesting).toBe(false)
    expect(FakeAudioContext.gains).toEqual([])
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(voice.isConnected).toBe(false)
    rtc.stopMicTest()
    expect(voice.currentInputLevel).toBe(0)
  })
})

describe('standalone meter on the WebKit constructor fallback', () => {
  it('builds and tears down metering when only webkitAudioContext is available', async () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', FakeAudioContext)
    const { rtc, voice } = setup()
    const starting = rtc.startMicTest()
    const capture = await grantMic()
    expect(await starting).toBe(true)
    expect(FakeAudioContext.instances).toHaveLength(1)
    rtc.stopMicTest()
    expect(present(capture.getTracks()[0]).stop).toHaveBeenCalled()
    expect(present(FakeAudioContext.instances.at(-1)).closed).toBe(true)
    expect(voice.currentInputLevel).toBe(0)
  })
})
