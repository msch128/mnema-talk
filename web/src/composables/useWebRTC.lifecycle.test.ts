import { describe, it, expect, vi } from 'vitest'
import { present } from '../media-test.fixture'
import { nextTick } from 'vue'
import * as voiceSession from '../lib/voiceSession'
import { useToastStore } from '../stores/toast'
import { installHarness, FakeAudioContext, FakePC, SFU_OFFER, audioElements, fakeStream, fakeTrack, grantMic, joined, negotiate, remoteStreamFor, setup, stubDisplayMedia } from './useWebRTC.harness.fixture'
import { suppressor } from './useWebRTC.noiseSuppressor.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('connection diagnostics and polling', () => {
  it('reports candidate types, bounds recent errors and ignores retired ICE callbacks', async () => {
    const { rtc, sent } = await joined()
    const pc = present(FakePC.instances.at(-1))
    present(pc.onicecandidate)({ candidate: null })
    present(pc.onicecandidate)({ candidate: { protocol: 'udp', type: 'host', toJSON: () => ({ candidate: 'candidate' }) } })
    present(pc.onicecandidate)({ candidate: { protocol: 'udp', type: 'host', toJSON: () => ({ candidate: 'candidate-2' }) } })
    present(pc.onicecandidate)({ candidate: { toJSON: () => ({ candidate: 'unknown' }) } })
    for (let i = 0; i < 7; i++) present(pc.onicecandidateerror)({ errorCode: i })
    present(pc.onicecandidateerror)({ errorCode: 701, errorText: 'unavailable', url: 'stun:example.invalid' })
    present(pc.onicegatheringstatechange)()
    expect(sent.at(-1)).toMatchObject({ type: 'webrtc_diag', payload: { event: 'gathering', candidates: { 'udp/host': 2, '?/?': 1 }, errors: ['3', '4', '5', '6', '701 unavailable stun:example.invalid'] } })
    const before = sent.length
    present(pc.oniceconnectionstatechange)()
    expect(sent).toHaveLength(before)
    for (const state of ['connected', 'failed', 'disconnected'] as const) {
      pc.iceConnectionState = state
      present(pc.oniceconnectionstatechange)()
      expect(sent.at(-1)).toMatchObject({ type: 'webrtc_diag', payload: { event: 'ice', ice: state } })
    }
    rtc.rejoinAfterReconnect()
    const count = sent.length
    present(pc.onicecandidateerror)({ errorCode: 1 })
    present(pc.onicegatheringstatechange)()
    expect(sent).toHaveLength(count)
  })

  it('samples browser statistics, logs active video every fifth tick and clears on leave', async () => {
    const { rtc, voice, sent } = await joined()
    const pc = present(FakePC.instances.at(-1))
    const stats = new Map<string, import('../lib/mediaStats').MediaStat>([
      ['o', { id: 'o', type: 'outbound-rtp', kind: 'video', bytesSent: 1000, timestamp: 1000 }]
    ])
    pc.getStats = vi.fn(async () => stats)
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    vi.useFakeTimers()
    // The polling timer predates fake timers, so reconnect creates one controlled here.
    rtc.rejoinAfterReconnect()
    const active = present(FakePC.instances.at(-1))
    active.getStats = pc.getStats
    await vi.advanceTimersByTimeAsync(10_000)
    expect(active.getStats).toHaveBeenCalledTimes(5)
    expect(voice.rtcStats).not.toBeNull()
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'stream')).toHaveLength(1)
    expect(sent.filter(event => event.type === 'webrtc_diag' && 'event' in event.payload && event.payload.event === 'stream').at(-1)).toMatchObject({ payload: { event: 'stream', sharing: true, screen_audio: false, video: [{ dir: 'out', kbps: null }] } })
    rtc.leaveVoiceChannel()
    expect(voice.rtcStats).toBeNull()
    await vi.advanceTimersByTimeAsync(2000)
    expect(active.getStats).toHaveBeenCalledTimes(5)
  })

  it('retries getStats failures and does not log empty or inactive video', async () => {
    const { rtc, voice, sent } = await joined()
    vi.useFakeTimers()
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    pc.getStats = vi.fn().mockRejectedValueOnce(new Error('closing')).mockResolvedValue(new Map())
    await vi.advanceTimersByTimeAsync(10_000)
    expect(pc.getStats).toHaveBeenCalledTimes(5)
    expect(voice.rtcStats).not.toBeNull()
    voice.setRemoteScreen('watcher', fakeStream(['video']))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'stream')).toEqual([])
  })
})

describe('session restoration and playback permission', () => {
  it('resumes only a recent channel that still exists and never joins twice', async () => {
    const { rtc, chat, voice } = setup()
    expect(await rtc.resumeVoiceSession()).toBe(false)
    voiceSession.save('deleted')
    expect(await rtc.resumeVoiceSession()).toBe(false)
    expect(voiceSession.recent()).toBeNull()
    vi.spyOn(chat, 'isVoiceChannel').mockImplementation(id => id === 'resume')
    voiceSession.save('resume')
    const resume = rtc.resumeVoiceSession()
    await grantMic()
    expect(await resume).toBe(true)
    expect(voice.currentChannelId).toBe('resume')
    expect(await rtc.resumeVoiceSession()).toBe(false)
  })

  it('flags blocked autoplay and retries all remote audio after a click', async () => {
    const { rtc, voice, pc } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const originalAudio = globalThis.Audio
    vi.stubGlobal('Audio', function () {
      const el = new originalAudio()
      el.play = vi.fn().mockRejectedValue(new DOMException('gesture', 'NotAllowedError'))
      return el
    })
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    await Promise.resolve()
    await Promise.resolve()
    expect(voice.audioBlocked).toBe(true)
    rtc.resumeRemoteAudio()
    await Promise.resolve()
    expect(voice.audioBlocked).toBe(true)
    for (const el of audioElements) el.play = vi.fn().mockResolvedValue(undefined)
    rtc.resumeRemoteAudio()
    await Promise.resolve()
    expect(voice.audioBlocked).toBe(false)
  })
})

describe('sender failure cleanup', () => {
  it('keeps media controls responsive when negotiated sender replacement fails', async () => {
    const { rtc, voice, pc } = await joined()
    for (const sender of pc.senders) sender.replaceTrack.mockRejectedValue(new Error('closed sender'))
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    expect(voice.isScreenSharing).toBe(true)
    const track = present(display.getAudioTracks()[0])
    present(track.onended).call(track, new Event('ended'))
    await Promise.resolve()
    expect(voice.hasScreenAudio).toBe(false)
    await rtc.replaceScreenShare()
    rtc.stopScreenShare()
    const camera = fakeStream(['video'])
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(camera)
    await rtc.startCamera()
    expect(voice.isCameraOn).toBe(true)
    rtc.stopCamera()
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(fakeStream())
    await rtc.applyAudioSettings()
    rtc.leaveVoiceChannel()
    expect(voice.currentChannelId).toBeNull()
  })

  it('negotiates even when restoring microphone and screen-audio tracks is rejected', async () => {
    const { rtc, voice, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()
    const pc = present(FakePC.instances.at(-1))
    const original = pc.setRemoteDescription.bind(pc)
    pc.setRemoteDescription = async description => {
      await original(description)
      present(pc.senders[0]).replaceTrack.mockRejectedValue(new Error('mic temporarily unavailable'))
      present(pc.senders[3]).replaceTrack.mockRejectedValue(new Error('sound temporarily unavailable'))
    }
    await negotiate()
    expect(voice.isScreenSharing).toBe(true)
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
  })

  it('leaves a filter and input gain safely when graph disconnection or close rejects', async () => {
    suppressor.node = { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    voice.inputVolume = 80
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const gain = present(FakeAudioContext.gains.at(-1))
    gain.disconnect.mockImplementation(() => { throw new Error('already detached') })
    suppressor.node.disconnect.mockImplementation(() => { throw new Error('already detached') })
    vi.spyOn(present(FakeAudioContext.instances.at(-1)), 'close').mockRejectedValue(new Error('already closed'))
    rtc.leaveVoiceChannel()
    expect(voice.localAudioStream).toBeNull()
    expect(suppressor.node.disconnect).toHaveBeenCalled()
  })

  it('closes standalone loopback safely when monitor or filter disconnection fails', async () => {
    suppressor.node = { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const test = rtc.toggleMicTest()
    await grantMic()
    await test
    present(FakeAudioContext.gains.at(-1)).disconnect.mockImplementation(() => { throw new Error('detached') })
    suppressor.node.disconnect.mockImplementation(() => { throw new Error('detached') })
    vi.spyOn(present(FakeAudioContext.instances.at(-1)), 'close').mockRejectedValue(new Error('closed'))
    rtc.stopMicTest()
    expect(voice.isMicTesting).toBe(false)
    expect(voice.currentInputLevel).toBe(0)
  })
})

describe('startup interruption and optional capture tracks', () => {
  it('leaves before cached ICE configuration resumes a join', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    rtc.leaveVoiceChannel()
    await join
    expect(navigator.mediaDevices.getUserMedia).not.toHaveBeenCalled()
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
  })

  it('updates mute and volume controls outside a call without starting capture', async () => {
    const { voice, sent } = setup()
    voice.isMuted = true
    voice.inputVolume = 70
    voice.isScreenAudioMuted = true
    await nextTick()
    expect(sent).toEqual([])
    expect(navigator.mediaDevices.getUserMedia).not.toHaveBeenCalled()
  })

  it('accepts a late kick after explicit leave without another notification', async () => {
    const { rtc, chat } = await joined()
    rtc.leaveVoiceChannel()
    const toasts = useToastStore()
    const count = toasts.toasts.length
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(toasts.toasts).toHaveLength(count)
  })

  it('negotiates missing publish slots and binds them when they become available', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'v=0\r\n' } })
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1))
    expect(pc.senders).toEqual([])
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'v=0\r\nm=audio 9 UDP 111\r\na=mid:0\r\n' } })
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(2))
    expect(pc.senders).toHaveLength(1)
    await negotiate()
    expect(pc.senders).toHaveLength(4)
  })

  it('starts camera before negotiation and restores it on the offered camera line', async () => {
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const camera = fakeStream(['video'])
    vi.mocked(navigator.mediaDevices.getUserMedia).mockResolvedValue(camera)
    await rtc.startCamera()
    expect(voice.isCameraOn).toBe(true)
    const pc = await negotiate()
    expect(present(pc.senders[2]).track).toBe(camera.getVideoTracks()[0])
  })

  it('does not advertise a restored screen that was stopped while sender replacement awaited', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    const pc = present(FakePC.instances.at(-1))
    let finish: (() => void) | undefined
    const original = pc.setRemoteDescription.bind(pc)
    pc.setRemoteDescription = async description => {
      await original(description)
      present(pc.senders[1]).replaceTrack = vi.fn(() => new Promise<void>(resolve => { finish = resolve }))
    }
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    const pending = present(finish)
    rtc.stopScreenShare()
    pending()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(sent.filter(e => e.type === 'webrtc_screenshare_start')).toEqual([])
  })

  it('does not finish attaching a screen stopped during replaceTrack', async () => {
    const { rtc, screen, sent } = await joined()
    stubDisplayMedia(fakeStream(['video']))
    let finish: (() => void) | undefined
    screen.replaceTrack = vi.fn(() => new Promise<void>(resolve => { finish = resolve }))
    const start = rtc.startScreenShare()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    const pending = present(finish)
    rtc.stopScreenShare()
    pending()
    await start
    expect(sent.filter(e => e.type === 'webrtc_screenshare_start')).toEqual([])
  })

  it('discards a replacement stopped during its active encoder tuning', async () => {
    const { rtc, screen, sent, voice } = await joined()
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    stubDisplayMedia(fakeStream(['video']))
    const starts = sent.filter(e => e.type === 'webrtc_screenshare_start').length
    let finish: (() => void) | undefined
    screen.setParameters = vi.fn(() => new Promise<void>(resolve => { finish = resolve }))
    const replacement = rtc.replaceScreenShare()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.stopScreenShare()
    present(finish)()
    await replacement
    expect(voice.isScreenSharing).toBe(false)
    expect(sent.filter(e => e.type === 'webrtc_screenshare_start')).toHaveLength(starts)
  })

  it('skips a pending quality update after the screen track is replaced or stopped', async () => {
    const { rtc, voice } = await joined()
    const stream = fakeStream(['video'])
    const track = present(stream.getVideoTracks()[0])
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    let finish: (() => void) | undefined
    vi.spyOn(track, 'applyConstraints').mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    voice.setScreenQuality({ resolution: 1440, fps: 60 })
    await nextTick()
    rtc.stopScreenShare()
    present(finish)()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(track.applyConstraints).toHaveBeenCalledTimes(1)
    expect(voice.isScreenSharing).toBe(false)
  })

  it('uses the WebKit audio-context constructor when the standard constructor is absent', async () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', FakeAudioContext)
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(FakeAudioContext.instances).toHaveLength(1)
    expect(voice.localAudioStream).not.toBeNull()
  })

  it('supports a standalone meter using suspended contexts, keyboard feedback without a call, and native suppression after model initialization failure', async () => {
    class SuspendedContext extends FakeAudioContext { constructor() { super(); this.state = 'suspended' } }
    vi.stubGlobal('AudioContext', SuspendedContext)
    vi.spyOn(FakeAudioContext.prototype, 'resume').mockRejectedValue(new Error('gesture pending'))
    suppressor.supported = true
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    voice.inputMode = 'ptt'
    voice.pttKey = 'Space'
    const test = rtc.startMicTest()
    await grantMic()
    expect(await test).toBe(true)
    // Failure without loopback still falls back only when a monitor is active.
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
    window.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keyup', { code: 'Space' }))
    expect(voice.isPttPressed).toBe(false)
    expect(voice.isConnected).toBe(false)
    expect(voice.isMicTesting).toBe(false)
    expect(present(FakeAudioContext.instances.at(-1)).resume).toHaveBeenCalled()
  })

  it('does not recreate a stopped standalone pipeline when its filter initialization rejects', async () => {
    suppressor.supported = true
    let fail: ((reason: Error) => void) | undefined
    suppressor.create = () => new Promise((_resolve, reject) => { fail = reject })
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const test = rtc.startMicTest()
    await grantMic()
    await vi.waitFor(() => expect(fail).toBeTypeOf('function'))
    rtc.stopMicTest()
    present(fail)(new Error('cancelled startup'))
    expect(await test).toBe(false)
    expect(voice.isMicTesting).toBe(false)
    expect(voice.currentInputLevel).toBe(0)
  })

  it('recovers an initialized filter without unhandled errors when the fallback is stopped or rejected', async () => {
    suppressor.node = { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
    const { rtc, voice } = setup()
    voice.noiseMode = 'ai'
    const start = rtc.toggleMicTest()
    await grantMic()
    await start
    vi.useFakeTimers()
    const onFailure = present(present(suppressor.options.at(-1)).onFailure)
    onFailure()
    rtc.stopMicTest()
    await vi.advanceTimersByTimeAsync(0)
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
    onFailure()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(1)
  })
})

describe('late peer statistics after teardown', () => {
  it.each(['leave', 'reconnect'] as const)('does not publish a retired peer report after %s', async action => {
    const { rtc, voice, sent } = await joined()
    vi.useFakeTimers()
    rtc.rejoinAfterReconnect()
    const retired = present(FakePC.instances.at(-1))
    let finish: ((report: Map<string, import('../lib/mediaStats').MediaStat>) => void) | undefined
    retired.getStats = vi.fn(() => new Promise<Map<string, import('../lib/mediaStats').MediaStat>>(resolve => { finish = resolve }))
    await vi.advanceTimersByTimeAsync(2000)
    expect(retired.getStats).toHaveBeenCalledTimes(1)
    if (action === 'leave') rtc.leaveVoiceChannel()
    else rtc.rejoinAfterReconnect()
    const diagnostics = sent.filter(event => event.type === 'webrtc_diag').length
    present(finish)(new Map([['old', { id: 'old', type: 'outbound-rtp', kind: 'video', bytesSent: 1_000_000, timestamp: 1_000 }]]))
    await Promise.resolve()
    await Promise.resolve()
    expect(voice.rtcStats).toBeNull()
    expect(sent.filter(event => event.type === 'webrtc_diag')).toHaveLength(diagnostics)
  })
})
