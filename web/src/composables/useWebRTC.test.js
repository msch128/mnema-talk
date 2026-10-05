import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { toRaw } from 'vue'
import { publishMids, tuneScreenOffer, screenAudioConstraints, useWebRTC, SCREEN_MAX_BITRATE, CAMERA_MAX_BITRATE, SCREEN_START_KBPS, SCREEN_MIN_KBPS } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'

// The AI filter worklets can't run in jsdom; tests hand in a fake node instead.
const suppressor = vi.hoisted(() => ({ node: null, models: [] }))
vi.mock('../lib/noiseSuppressor', () => ({
  isNoiseSuppressionSupported: () => suppressor.node !== null,
  preloadNoiseSuppressor: () => {},
  createNoiseSuppressorNode: async (ctx, model) => {
    suppressor.models.push(model)
    return suppressor.node
  }
}))

// --- Browser API stand-ins -------------------------------------------------

function fakeTrack(kind = 'audio') {
  return { kind, enabled: true, readyState: 'live', stop: vi.fn(), clone() { return fakeTrack(kind) } }
}

function streamOf(tracks) {
  return {
    getTracks: () => tracks,
    getAudioTracks: () => tracks.filter(t => t.kind === 'audio'),
    getVideoTracks: () => tracks.filter(t => t.kind === 'video')
  }
}

function fakeStream(kinds = ['audio']) {
  return streamOf(kinds.map(k => fakeTrack(k)))
}

function fakeSender(track) {
  const s = {
    track,
    replaceTrack: vi.fn(async t => { s.track = t }),
    getParameters: () => ({ encodings: [{}] }),
    setParameters: vi.fn(async () => {})
  }
  return s
}

// The SFU's offer: receive lines for microphone, screen and camera first.
const SFU_OFFER = [
  'v=0',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:1', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:2', 'a=recvonly',
  ''
].join('\r\n')

class FakePC {
  static instances = []
  constructor() {
    this.transceivers = []
    // Lines added locally: like in a browser, never matched to a remote offer.
    this.unmatched = []
    this.remoteDescription = null
    this.candidates = []
    this.closed = false
    FakePC.instances.push(this)
  }
  addTrack(track) { const s = fakeSender(track); this.unmatched.push(s); return s }
  addTransceiver(trackOrKind) {
    const s = fakeSender(typeof trackOrKind === 'string' ? null : trackOrKind)
    this.unmatched.push(s)
    return { sender: s }
  }
  // Senders on the negotiated lines, in m-line order (mic, screen, camera).
  get senders() { return this.transceivers.map(t => t.sender) }
  getSenders() { return this.senders }
  getTransceivers() { return this.transceivers }
  async setRemoteDescription(d) {
    this.remoteDescription = d
    for (const m of String(d.sdp).matchAll(/a=mid:(\S+)/g)) {
      if (!this.transceivers.some(t => t.mid === m[1])) {
        this.transceivers.push({ mid: m[1], direction: 'recvonly', sender: fakeSender(null) })
      }
    }
  }
  async createAnswer() { return { type: 'answer', sdp: 'a' } }
  async setLocalDescription() {}
  async addIceCandidate(c) { this.candidates.push(c) }
  async getStats() { return new Map() }
  close() { this.closed = true }
}

class FakeAudioContext {
  static destinations = []
  static sources = []
  static gains = []
  constructor() { this.state = 'running'; this.currentTime = 0; this.destination = {} }
  createMediaStreamDestination() {
    const node = { stream: fakeStream(), connect() {}, disconnect: vi.fn() }
    FakeAudioContext.destinations.push(node)
    return node
  }
  createGain() {
    const node = {
      gain: {
        value: 1,
        setValueAtTime: vi.fn(function (v) { this.value = v })
      },
      connect: vi.fn(),
      disconnect: vi.fn()
    }
    FakeAudioContext.gains.push(node)
    return node
  }
  createAnalyser() { return { fftSize: 256, connect() {}, getByteTimeDomainData() {} } }
  createMediaStreamSource(stream) {
    const node = { stream, connect: vi.fn(), disconnect: vi.fn() }
    FakeAudioContext.sources.push(node)
    return node
  }
  createBiquadFilter() { return { type: '', frequency: { setValueAtTime() {} }, connect() {} } }
  resume() { return Promise.resolve() }
  close() { return Promise.resolve() }
}

let micRequests
let audioElements
beforeEach(() => {
  audioElements = []
  setActivePinia(createPinia())
  suppressor.node = null
  suppressor.models = []
  FakeAudioContext.destinations = []
  FakeAudioContext.sources = []
  FakeAudioContext.gains = []
  FakePC.instances = []
  micRequests = []
  vi.stubGlobal('RTCPeerConnection', FakePC)
  vi.stubGlobal('RTCSessionDescription', function (d) { return d })
  vi.stubGlobal('RTCIceCandidate', function (c) { return c })
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal('MediaStream', function (tracks) { return streamOf(tracks) })
  // Remote audio elements: jsdom cannot play media.
  vi.stubGlobal('Audio', function () {
    const el = document.createElement('audio')
    el.play = vi.fn(async () => {})
    el.pause = vi.fn()
    // The DOM would only accept a real MediaStream here.
    Object.defineProperty(el, 'srcObject', { value: null, writable: true })
    audioElements.push(el)
    return el
  })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ ice_servers: [] }) }))
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      enumerateDevices: vi.fn().mockResolvedValue([]),
      // Each request stays pending until the test resolves it.
      getUserMedia: vi.fn(() => new Promise(resolve => {
        const stream = fakeStream()
        micRequests.push(() => resolve(stream))
        micRequests.at(-1).stream = stream
      }))
    }
  })
})

afterEach(() => {
  const { leaveVoiceChannel } = useWebRTC()
  leaveVoiceChannel()
  vi.unstubAllGlobals()
})

function setup() {
  const chat = useChatStore()
  const sent = []
  chat.sendWSEvent = (type, payload) => sent.push({ type, payload })
  return { chat, voice: useVoiceStore(), rtc: useWebRTC(), sent }
}

// The SFU sends its offer once the join is in; the answer binds our senders.
async function negotiate() {
  const pc = FakePC.instances.at(-1)
  useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(3))
  await new Promise(r => setTimeout(r, 0))
  return pc
}

async function grantMic() {
  await vi.waitFor(() => expect(micRequests.length).toBeGreaterThan(0))
  const req = micRequests.shift()
  req()
  return req.stream
}

// --- Tests -----------------------------------------------------------------

describe('useWebRTC join and leave', () => {
  it('joins with the microphone attached', async () => {
    const { rtc, sent, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(sent.filter(e => e.type === 'voice_join')).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1' } }])
    expect(voice.currentChannelId).toBe('ch-1')
    // We send on the SFU's own mic, screen and camera lines (never on lines
    // added locally, which a browser would not match to the offer); only
    // the mic carries a track yet.
    const pc = await negotiate()
    expect(pc.unmatched).toHaveLength(0)
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv'])
    const senders = pc.senders
    expect(senders).toHaveLength(3)
    expect(senders.filter(sd => sd.track)).toHaveLength(1)
  })

  it('leaving while the mic prompt is open leaves no ghost and stops the mic', async () => {
    const { rtc, sent, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    rtc.leaveVoiceChannel()
    const stream = await grantMic()
    await join
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
    expect(stream.getTracks()[0].stop).toHaveBeenCalled()
    expect(voice.currentChannelId).toBeNull()
  })

  it('a second join supersedes a pending first one', async () => {
    const { rtc, sent } = setup()
    const first = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    const second = rtc.joinVoiceChannel('ch-2')
    const firstStream = await grantMic()
    await grantMic()
    await Promise.all([first, second])
    const joins = sent.filter(e => e.type === 'voice_join').map(e => e.payload.channel_id)
    expect(joins).toEqual(['ch-2'])
    expect(firstStream.getTracks()[0].stop).toHaveBeenCalled()
  })
})

describe('useWebRTC signaling', () => {
  it('ignores offers that arrive after leaving', async () => {
    const { rtc, chat } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    rtc.leaveVoiceChannel()
    const before = FakePC.instances.length
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await new Promise(r => setTimeout(r, 10))
    expect(FakePC.instances.length).toBe(before)
  })

  it('buffers ICE candidates until the offer is applied, then answers', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'c1' } })
    await new Promise(r => setTimeout(r, 10))
    const pc = FakePC.instances.at(-1)
    expect(pc.candidates).toEqual([])
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(sent.some(e => e.type === 'webrtc_answer')).toBe(true))
    expect(pc.candidates).toEqual([{ candidate: 'c1' }])
  })

  it('applies the offer with H.264 first on the screen line when the browser can send it', async () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => ({ codecs: [{ mimeType: 'video/VP8' }, { mimeType: 'video/H264' }] }) })
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = FakePC.instances.at(-1)
    const offer = SFU_OFFER.replace(/(m=video 9 UDP\/TLS\/RTP\/SAVPF) 96(\r\na=mid:(\d))/g, (_, m, rest) =>
      `${m} 96 102${rest}\r\na=rtpmap:96 VP8/90000\r\na=rtpmap:102 H264/90000\r\na=fmtp:102 packetization-mode=1;profile-level-id=42e01f`)
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: offer } })
    await vi.waitFor(() => expect(pc.remoteDescription).not.toBeNull())
    const applied = pc.remoteDescription.sdp
    expect(pc.remoteDescription.type).toBe('offer')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 102 96\r\na=mid:1')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 96 102\r\na=mid:2')
    expect(applied).toContain('x-google-start-bitrate')
  })

  it('reconnect starts a fresh connection and announces the join again', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const oldPC = FakePC.instances.at(-1)
    rtc.rejoinAfterReconnect()
    expect(oldPC.closed).toBe(true)
    expect(FakePC.instances.at(-1)).not.toBe(oldPC)
    expect(sent.filter(e => e.type === 'voice_join')).toHaveLength(2)
  })
})

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
    const sender = (await negotiate()).senders[0]

    const apply = rtc.applyAudioSettings()
    const newStream = await grantMic()
    await apply
    expect(sender.replaceTrack).toHaveBeenCalledWith(newStream.getAudioTracks()[0])
    expect(oldStream.getTracks()[0].stop).toHaveBeenCalled()
    expect(toRaw(rtc.localAudioStream.value)).toBe(newStream)
  })
})

describe('AI noise suppression', () => {
  function fakeSuppressor() {
    return { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() }
  }
  const lastConstraints = () => navigator.mediaDevices.getUserMedia.mock.calls.at(-1)[0].audio

  async function joinWith(mode) {
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
    const processed = FakeAudioContext.destinations.at(-1).stream.getAudioTracks()[0]
    expect(FakePC.instances.at(-1).senders[0].track).toBe(processed)
    expect(suppressor.node.connect).toHaveBeenCalled()

    // Mute switches the sent track; the raw capture keeps feeding the model and meter.
    voice.toggleMute()
    expect(processed.enabled).toBe(false)
    expect(raw.getAudioTracks()[0].enabled).toBe(true)
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
    expect(raw.getAudioTracks()[0].stop).toHaveBeenCalled()
    expect(node.destroy).toHaveBeenCalled()
  })

  it('falls back to the browser filter when the worklet is unavailable', async () => {
    const { raw } = await joinWith('ai')

    expect(lastConstraints().noiseSuppression).toBe(true)
    expect(FakePC.instances.at(-1).senders[0].track).toBe(raw.getAudioTracks()[0])
  })

  it('off disables every filter', async () => {
    suppressor.node = fakeSuppressor()
    const { raw } = await joinWith('off')

    expect(suppressor.models).toEqual([])
    expect(lastConstraints().noiseSuppression).toBe(false)
    expect(FakePC.instances.at(-1).senders[0].track).toBe(raw.getAudioTracks()[0])
  })
})

// --- Screen share audio, per-user volume, camera ----------------------------

async function joined() {
  const ctx = setup()
  const join = ctx.rtc.joinVoiceChannel('ch-1')
  const mic = await grantMic()
  await join
  const pc = await negotiate()
  const [audio, screen, camera] = pc.senders
  return { ...ctx, mic, pc, audio, screen, camera }
}

function stubDisplayMedia(stream) {
  navigator.mediaDevices.getDisplayMedia = vi.fn(async () => stream)
}

describe('screen share with audio', () => {
  it('mixes screen audio with the mic into the one sent track', async () => {
    const { rtc, audio, screen, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)

    await rtc.startScreenShare()

    const mixTrack = FakeAudioContext.destinations.at(-1).stream.getAudioTracks()[0]
    expect(screen.track).toBe(display.getVideoTracks()[0])
    expect(audio.replaceTrack).toHaveBeenCalledWith(mixTrack)
    expect(audio.track).toBe(mixTrack)
    const inputs = FakeAudioContext.sources.map(n => n.stream.getAudioTracks()[0])
    expect(inputs).toContain(display.getAudioTracks()[0])
    expect(inputs).toContain(mic.getAudioTracks()[0])
  })

  it('asks the browser to leave out the voices this page plays', async () => {
    const { rtc } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia.mock.calls[0][0].audio).toMatchObject({ echoCancellation: true })
  })

  it('leaves the page out of the capture where supported, else cancels its echo', () => {
    expect(screenAudioConstraints({ restrictOwnAudio: true })).toEqual({ autoGainControl: false, noiseSuppression: false, echoCancellation: false, restrictOwnAudio: true })
    expect(screenAudioConstraints({})).toEqual({ autoGainControl: false, noiseSuppression: false, echoCancellation: true })
  })

  it('mute and the gate only silence the mic, never the screen audio', async () => {
    const { rtc, voice, audio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleMute()
    expect(mic.getAudioTracks()[0].enabled).toBe(false)
    // The sent mix track and the screen track stay enabled: only the mic input is silent.
    expect(audio.track.enabled).toBe(true)
    expect(display.getAudioTracks()[0].enabled).toBe(true)
  })

  it('without screen audio the plain mic keeps being sent', async () => {
    const { rtc, audio, mic } = await joined()
    const calls = audio.replaceTrack.mock.calls.length
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    expect(audio.replaceTrack.mock.calls.length).toBe(calls)
    expect(audio.track).toBe(mic.getAudioTracks()[0])
  })

  it('restores the mic and tears the mix down when sharing stops', async () => {
    const { rtc, audio, screen, mic, sent } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()
    const dest = FakeAudioContext.destinations.at(-1)

    rtc.stopScreenShare()
    await vi.waitFor(() => expect(audio.track).toBe(mic.getAudioTracks()[0]))
    expect(screen.track).toBeNull()
    expect(dest.disconnect).toHaveBeenCalled()
    expect(dest.stream.getAudioTracks()[0].stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'webrtc_screenshare_stop')).toBe(true)
  })

  it('falls back to the mic when only the screen audio track ends', async () => {
    const { rtc, voice, audio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    display.getAudioTracks()[0].onended()
    await vi.waitFor(() => expect(audio.track).toBe(mic.getAudioTracks()[0]))
    expect(voice.isScreenSharing).toBe(true)
  })

  it('rebuilds the mix when audio settings are applied mid-share', async () => {
    const { rtc, audio } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    const firstMix = audio.track

    const apply = rtc.applyAudioSettings()
    const newMic = await grantMic()
    await apply

    const newMix = FakeAudioContext.destinations.at(-1).stream.getAudioTracks()[0]
    expect(newMix).not.toBe(firstMix)
    expect(audio.track).toBe(newMix)
    const inputs = FakeAudioContext.sources.map(n => n.stream.getAudioTracks()[0])
    expect(inputs.at(-1)).toBe(newMic.getAudioTracks()[0])
    expect(inputs.at(-2)).toBe(display.getAudioTracks()[0])
  })

  it('a reconnect sends the mix, not the bare mic', async () => {
    const { rtc, mic } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()
    const mixTrack = FakeAudioContext.destinations.at(-1).stream.getAudioTracks()[0]

    rtc.rejoinAfterReconnect()
    const fresh = await negotiate()
    expect(fresh.senders[0].track).toBe(mixTrack)
    expect(fresh.senders[0].track).not.toBe(mic.getAudioTracks()[0])
    expect(fresh.senders[1].track.kind).toBe('video')
  })
})

describe('per-user playback', () => {
  function remoteAudio(pc, userId) {
    pc.ontrack({ track: fakeTrack('audio'), streams: [{ id: userId }] })
    return audioElements.at(-1)
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

  it('multiplies with the master volume and deafen wins', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 50
    const alice = remoteAudio(pc, 'alice')
    voice.setUserVolume('alice', 50)
    await vi.waitFor(() => expect(alice.volume).toBe(0.25))
    voice.isDeafened = true
    await vi.waitFor(() => expect(alice.volume).toBe(0))
  })

  it('amplifies above 100 % through a gain node', async () => {
    const { voice, pc } = await joined()
    voice.outputVolume = 100
    const alice = remoteAudio(pc, 'alice')
    voice.setUserVolume('alice', 150)
    await vi.waitFor(() => expect(FakeAudioContext.gains.at(-1)?.gain.value).toBe(1.5))
    // The element only keeps the stream flowing; the graph does the playing.
    expect(alice.muted).toBe(true)

    voice.setUserVolume('alice', 100)
    await vi.waitFor(() => expect(FakeAudioContext.gains.at(-1).gain.value).toBe(1))
  })
})

describe('webcam', () => {
  it('sends the camera on its own line, next to a screen share', async () => {
    const { rtc, voice, screen, camera, sent } = await joined()
    const cam = fakeStream(['video'])
    navigator.mediaDevices.getUserMedia.mockImplementationOnce(async () => cam)
    stubDisplayMedia(fakeStream(['video']))

    await rtc.startCamera()
    expect(navigator.mediaDevices.getUserMedia.mock.calls.at(-1)[0].video).toBeTruthy()
    expect(camera.track).toBe(cam.getVideoTracks()[0])
    expect(voice.isCameraOn).toBe(true)

    await rtc.startScreenShare()
    expect(screen.track).not.toBe(camera.track)
    expect(camera.track).toBe(cam.getVideoTracks()[0])

    rtc.stopCamera()
    expect(camera.track).toBeNull()
    expect(screen.track).not.toBeNull()
    expect(voice.isCameraOn).toBe(false)
    expect(cam.getVideoTracks()[0].stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'webrtc_camera_stop')).toBe(true)
  })

  it('caps the bitrate and keeps the screen resolution under congestion', async () => {
    const { rtc, screen, camera } = await joined()
    navigator.mediaDevices.getUserMedia.mockImplementationOnce(async () => fakeStream(['video']))
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    await rtc.startCamera()

    const screenParams = screen.setParameters.mock.calls[0][0]
    expect(screenParams.encodings[0].maxBitrate).toBe(SCREEN_MAX_BITRATE)
    expect(screenParams.degradationPreference).toBe('maintain-resolution')
    expect(camera.setParameters.mock.calls[0][0].encodings[0].maxBitrate).toBe(CAMERA_MAX_BITRATE)
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
      navigator.mediaDevices.getUserMedia.mockImplementationOnce(async () => {
        throw Object.assign(new Error(name), { name })
      })
      await rtc.startCamera()
      expect(spy).toHaveBeenLastCalledWith(t(key))
      expect(voice.isCameraOn).toBe(false)
    }
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
    navigator.mediaDevices.getUserMedia.mockImplementationOnce(async () => cam)
    await rtc.startCamera()
    rtc.leaveVoiceChannel()
    expect(cam.getVideoTracks()[0].stop).toHaveBeenCalled()
    expect(voice.isCameraOn).toBe(false)
  })

  it('maps remote cameras and screen shares to their publishers', async () => {
    const { voice, pc } = await joined()
    const camTrack = fakeTrack('video')
    pc.ontrack({ track: camTrack, streams: [{ id: 'cam:alice' }] })
    expect(Object.keys(voice.userVideoStreams)).toEqual(['alice'])
    expect(voice.remoteScreenStream).toBeNull()

    // A screen share nobody opted into is not shown.
    pc.ontrack({ track: fakeTrack('video'), streams: [{ id: 'bob' }] })
    expect(voice.remoteScreenStream).toBeNull()
    voice.watchScreen('bob')
    pc.ontrack({ track: fakeTrack('video'), streams: [{ id: 'bob' }] })
    expect(voice.remoteScreenUserId).toBe('bob')
    expect(voice.remoteScreenStream).not.toBeNull()

    camTrack.onended()
    expect(voice.userVideoStreams.alice).toBeUndefined()
  })
})

describe('video subscriptions', () => {
  const subs = sent => sent.filter(e => e.type === 'webrtc_subscribe').map(e => e.payload)

  beforeEach(() => localStorage.clear())

  it('sends camera opt-outs right after joining, nothing by default', async () => {
    const first = await joined()
    expect(subs(first.sent)).toEqual([])
    first.rtc.leaveVoiceChannel()

    localStorage.setItem('mnema_hidden_cameras', JSON.stringify({ alice: true }))
    setActivePinia(createPinia())
    const { sent } = await joined()
    const types = sent.map(e => e.type)
    expect(types.indexOf('webrtc_subscribe')).toBeGreaterThan(types.indexOf('voice_join'))
    expect(subs(sent)).toEqual([{ kind: 'camera', user_id: 'alice', on: false }])
  })

  it('forwards later changes: hide camera, opt into and out of a screen share', async () => {
    const { voice, sent } = await joined()
    voice.setCameraHidden('alice', true)
    voice.watchScreen('bob')
    voice.unwatchScreen('bob')
    voice.setAllCamerasOff(true)
    expect(subs(sent)).toEqual([
      { kind: 'camera', user_id: 'alice', on: false },
      { kind: 'screen', user_id: 'bob', on: true },
      { kind: 'screen', user_id: 'bob', on: false },
      { kind: 'camera', all: true, on: false }
    ])
  })

  it('resends opt-outs and still-running screen opt-ins after a reconnect', async () => {
    const { rtc, voice, sent } = await joined()
    voice.setCameraHidden('alice', true)
    voice.handleMediaState({ user_id: 'bob', screen: true })
    voice.handleMediaState({ user_id: 'carol', screen: true })
    voice.watchScreen('bob')
    voice.watchScreen('carol')
    // carol's share ended while the connection was down
    voice.mediaState = { bob: { screen: true } }
    sent.length = 0
    rtc.rejoinAfterReconnect()
    expect(subs(sent)).toEqual([
      { kind: 'camera', user_id: 'alice', on: false },
      { kind: 'screen', user_id: 'bob', on: true }
    ])
    expect(voice.watchedScreens).toEqual({ bob: true })
  })

  it('stops sending after leaving', async () => {
    const { rtc, voice, sent } = await joined()
    rtc.leaveVoiceChannel()
    sent.length = 0
    voice.setCameraHidden('alice', true)
    expect(subs(sent)).toEqual([])
  })

  it('applies webrtc_media_state events', async () => {
    const { chat, voice } = await joined()
    chat.handleWSEvent({ type: 'webrtc_media_state', payload: { user_id: 'bob', screen: true, camera: false } })
    expect(voice.mediaState.bob).toEqual({ screen: true, camera: false })
  })
})

describe('mic test and loopback', () => {
  it('mutes outgoing mic in channel during mic test and restores on stop', async () => {
    const { rtc, voice } = await joined()
    const track = voice.localAudioStream.getAudioTracks()[0]
    expect(track.enabled).toBe(true)

    await rtc.startMicLoopback()
    expect(voice.isMicTesting).toBe(true)
    expect(track.enabled).toBe(false)

    rtc.stopMicLoopback()
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


describe('publishMids', () => {
  it('finds the SFU receive lines for mic, screen and camera', () => {
    const offer = [
      'v=0',
      'm=audio 9 UDP 111', 'a=mid:0', 'a=recvonly',
      'm=video 9 UDP 96', 'a=mid:1', 'a=recvonly',
      'm=video 9 UDP 96', 'a=mid:2', 'a=recvonly',
      // a forwarded track of someone else: not ours to send on
      'm=audio 9 UDP 111', 'a=mid:3', 'a=sendonly',
      'm=video 9 UDP 96', 'a=mid:4', 'a=sendonly',
      ''
    ].join('\r\n')
    expect(publishMids(offer)).toEqual({ audio: '0', video: ['1', '2'] })
    // Someone was already talking: the SFU forwards their voice on our mic
    // line, which then reads sendrecv. It is still our microphone line.
    expect(publishMids(offer.replace('a=mid:0\r\na=recvonly', 'a=mid:0\r\na=sendrecv'))).toEqual({ audio: '0', video: ['1', '2'] })
    expect(publishMids('')).toEqual({ audio: null, video: [] })
  })
})

describe('tuneScreenOffer', () => {
  // The video part of a Pion offer with its default codecs.
  const video = (mid) => [
    'm=video 9 UDP/TLS/RTP/SAVPF 96 97 102 103 104 105 106 107 98',
    'a=mid:' + mid,
    'a=rtpmap:96 VP8/90000',
    'a=rtcp-fb:96 nack',
    'a=rtpmap:97 rtx/90000',
    'a=fmtp:97 apt=96',
    'a=rtpmap:102 H264/90000',
    'a=fmtp:102 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f',
    'a=rtpmap:103 rtx/90000',
    'a=fmtp:103 apt=102',
    'a=rtpmap:104 H264/90000',
    'a=fmtp:104 level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f',
    'a=rtpmap:105 rtx/90000',
    'a=fmtp:105 apt=104',
    'a=rtpmap:106 H264/90000',
    'a=fmtp:106 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f',
    'a=rtpmap:107 rtx/90000',
    'a=fmtp:107 apt=106',
    'a=rtpmap:98 VP9/90000',
    'a=fmtp:98 profile-id=0'
  ]
  const offer = ['v=0', 'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=rtpmap:111 opus/48000/2',
    ...video('1'), ...video('2'), ''].join('\r\n')
  const section = (sdp, mid) => {
    const parts = sdp.split('\r\nm=')
    return parts.find(p => p.includes('a=mid:' + mid + '\r\n')).split('\r\n')
  }
  const extra = `x-google-start-bitrate=${SCREEN_START_KBPS};x-google-min-bitrate=${SCREEN_MIN_KBPS}`

  it('puts H.264 first on the screen line only', () => {
    const out = tuneScreenOffer(offer, '1', { h264: true })
    expect(section(out, '1')[0]).toBe('video 9 UDP/TLS/RTP/SAVPF 106 102 104 96 98 97 103 105 107')
    expect(section(out, '2')[0]).toBe('video 9 UDP/TLS/RTP/SAVPF 96 97 102 103 104 105 106 107 98')
    expect(out.endsWith('\r\n')).toBe(true)
  })

  it('raises the start and floor bitrate of every video codec on the screen line', () => {
    const screen = section(tuneScreenOffer(offer, '1', { h264: false }), '1')
    expect(screen[0]).toBe('video 9 UDP/TLS/RTP/SAVPF 96 97 102 103 104 105 106 107 98')
    expect(screen).toContain(`a=fmtp:96 ${extra}`)
    expect(screen.indexOf(`a=fmtp:96 ${extra}`)).toBe(screen.indexOf('a=rtpmap:96 VP8/90000') + 1)
    expect(screen).toContain(`a=fmtp:106 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f;${extra}`)
    expect(screen).toContain(`a=fmtp:98 profile-id=0;${extra}`)
    expect(screen).toContain('a=fmtp:97 apt=96')
    expect(section(tuneScreenOffer(offer, '1'), '2').join('\n')).not.toContain('x-google')
  })

  it('leaves the offer alone without a screen line', () => {
    expect(tuneScreenOffer(offer, null, { h264: true })).toBe(offer)
    expect(tuneScreenOffer(offer, '9', { h264: true })).toBe(offer)
    expect(tuneScreenOffer('', '1')).toBe('')
  })
})
