import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { toRaw } from 'vue'
import { publishMids, tuneScreenOffer, screenAudioConstraints, useWebRTC, SCREEN_MAX_BITRATE, CAMERA_MAX_BITRATE, SCREEN_START_KBPS, SCREEN_MIN_KBPS } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { streamBitrate, DEFAULT_STREAM_QUALITY } from '../lib/streamQuality'

// The AI filter worklets can't run in jsdom; tests hand in a fake node instead.
const suppressor = vi.hoisted(() => ({ node: null, models: [], options: [] }))
vi.mock('../lib/noiseSuppressor', () => ({
  isNoiseSuppressionSupported: () => suppressor.node !== null,
  preloadNoiseSuppressor: () => {},
  createNoiseSuppressorNode: async (ctx, model, options) => {
    suppressor.models.push(model)
    suppressor.options.push(options)
    return suppressor.node
  }
}))

// --- Browser API stand-ins -------------------------------------------------

let trackSeq = 0
function fakeTrack(kind = 'audio') {
  return { id: `track-${++trackSeq}`, kind, enabled: true, readyState: 'live', stop: vi.fn(), clone() { return fakeTrack(kind) } }
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

// The SFU's offer: receive lines for microphone, screen, camera and the
// screen share's sound first.
const SFU_OFFER = [
  'v=0',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:1', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:2', 'a=recvonly',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:3', 'a=recvonly',
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
  static instances = []
  constructor() {
    this.state = 'running'
    this.currentTime = 0
    this.destination = {}
    this.sinkId = ''
    this.closed = false
    FakeAudioContext.instances.push(this)
  }
  setSinkId(id) { this.sinkId = id; return Promise.resolve() }
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
  close() { this.closed = true; return Promise.resolve() }
}

let micRequests
let audioElements
beforeEach(() => {
  audioElements = []
  setActivePinia(createPinia())
  suppressor.node = null
  suppressor.models = []
  suppressor.options = []
  FakeAudioContext.destinations = []
  FakeAudioContext.sources = []
  FakeAudioContext.gains = []
  FakeAudioContext.instances = []
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
  const { leaveVoiceChannel, stopMicTest } = useWebRTC()
  stopMicTest()
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
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(4))
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
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv'])
    const senders = pc.senders
    expect(senders).toHaveLength(4)
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

  it('does not hang on an audio context that waits for a user gesture', async () => {
    class GestureLockedContext extends FakeAudioContext {
      constructor() { super(); this.state = 'suspended' }
      resume() { return new Promise(() => {}) }
    }
    vi.stubGlobal('AudioContext', GestureLockedContext)
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(sent.some(e => e.type === 'voice_join')).toBe(true)
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

describe('shared watchers', () => {
  it('reacts once to a setting however many components use it', async () => {
    const { rtc, sent, voice } = setup()
    useWebRTC()
    useWebRTC()
    useWebRTC()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const before = sent.filter(e => e.type === 'voice_mute_state').length
    voice.isMuted = true
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'voice_mute_state').length).toBe(before + 1))
    await new Promise(r => setTimeout(r, 0))
    expect(sent.filter(e => e.type === 'voice_mute_state').length).toBe(before + 1)
  })
})

describe('kicked from voice', () => {
  it('ends the call locally without a leave and does not rejoin', async () => {
    const { rtc, chat, voice, sent } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    const { recent } = await import('../lib/voiceSession')
    const join = rtc.joinVoiceChannel('ch-1')
    const mic = await grantMic()
    await join
    const pc = FakePC.instances.at(-1)

    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(voice.currentChannelId).toBeNull()
    expect(pc.closed).toBe(true)
    expect(mic.getTracks()[0].stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'voice_leave')).toBe(false)
    expect(recent()).toBeNull()
    expect(useToastStore().toasts.map(x => x.text)).toContain(t('voice.kicked'))

    const joins = sent.filter(e => e.type === 'voice_join').length
    rtc.rejoinAfterReconnect()
    expect(sent.filter(e => e.type === 'voice_join').length).toBe(joins)
  })

  it('ignores a kick from a channel I already left', async () => {
    const { rtc, chat, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(voice.currentChannelId).toBe('ch-2')
  })
})

describe('useWebRTC signaling', () => {
  it('discards an offer suspended on a connection replaced by reconnect', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const old = FakePC.instances.at(-1)
    let release
    const original = old.setRemoteDescription.bind(old)
    old.setRemoteDescription = vi.fn(async description => {
      await new Promise(resolve => { release = resolve })
      await original(description)
    })
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.rejoinAfterReconnect()
    const replacement = FakePC.instances.at(-1)
    release()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(replacement.transceivers).toEqual([])
    expect(sent.filter(event => event.type === 'webrtc_answer')).toEqual([])
    await negotiate()
    expect(sent.filter(event => event.type === 'webrtc_answer')).toHaveLength(1)
  })

  it('does not publish an old answer after leaving during answer creation', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const old = FakePC.instances.at(-1)
    let release
    old.createAnswer = vi.fn(() => new Promise(resolve => { release = resolve }))
    old.setLocalDescription = vi.fn(async () => {})
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    release({ type: 'answer', sdp: 'old-session' })
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(old.setLocalDescription).not.toHaveBeenCalled()
    expect(sent.filter(event => event.type === 'webrtc_answer')).toEqual([])
  })

  it('ignores ICE and media callbacks from a retired connection', async () => {
    const { rtc, voice, sent } = await joined()
    const old = FakePC.instances.at(-1)
    rtc.rejoinAfterReconnect()
    const before = sent.length
    old.onicecandidate({ candidate: { toJSON: () => ({ candidate: 'retired' }) } })
    old.ontrack({ track: fakeTrack('video'), streams: [{ id: 'cam:retired' }] })
    expect(sent).toHaveLength(before)
    expect(voice.userVideoStreams).toEqual({})
  })

  it('configures screen limits before restoring a share on a negotiated new sender', async () => {
    const { rtc } = await joined()
    const stream = fakeStream(['video'])
    stream.getVideoTracks()[0].getSettings = () => ({ width: 1920, height: 1080 })
    navigator.mediaDevices.getDisplayMedia = vi.fn().mockResolvedValue(stream)
    await rtc.startScreenShare()
    rtc.rejoinAfterReconnect()
    const replacement = await negotiate()
    const screen = replacement.senders[1]
    expect(screen.track).toBe(stream.getVideoTracks()[0])
    expect(screen.setParameters.mock.invocationCallOrder[0]).toBeLessThan(screen.replaceTrack.mock.invocationCallOrder[0])
  })

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
    expect(suppressor.options[0].signal.aborted).toBe(true)
  })

  it('discloses native fallback after worker failure, preserves the AI choice and never unmutes', async () => {
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    suppressor.node = fakeSuppressor()
    const { voice } = await joinWith('ai')
    voice.isMuted = true
    const previous = voice.localAudioStream
    suppressor.options[0].onFailure()
    suppressor.options[0].onFailure()
    const nativeStream = await grantMic()
    await vi.waitFor(() => expect(voice.localAudioStream).not.toBe(previous))
    expect(lastConstraints().noiseSuppression).toBe(true)
    expect(voice.noiseMode).toBe('ai')
    expect(nativeStream.getAudioTracks()[0].enabled).toBe(false)
    expect(useToastStore().toasts.filter(x => x.text === t('audio.filterDegraded'))).toHaveLength(1)
    expect(suppressor.models).toEqual(['dfn3'])
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
  const [audio, screen, camera, screenAudio] = pc.senders
  return { ...ctx, mic, pc, audio, screen, camera, screenAudio }
}

function stubDisplayMedia(stream) {
  navigator.mediaDevices.getDisplayMedia = vi.fn(async () => stream)
}

describe('screen share with audio', () => {
  it('sends the screen audio on its own line, apart from the mic', async () => {
    const { rtc, audio, screen, screenAudio, mic, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)

    await rtc.startScreenShare()

    expect(screen.track).toBe(display.getVideoTracks()[0])
    expect(screenAudio.track).toBe(display.getAudioTracks()[0])
    // The mic line keeps the bare microphone: nothing is mixed into the voice.
    expect(audio.track).toBe(mic.getAudioTracks()[0])
    expect(FakeAudioContext.sources.map(n => n.stream.getAudioTracks()[0])).not.toContain(display.getAudioTracks()[0])
    expect(voice.hasScreenAudio).toBe(true)
  })

  it('drops a share picked after leaving the call', async () => {
    const { rtc, voice, sent } = await joined()
    const display = fakeStream(['video', 'audio'])
    let pick
    navigator.mediaDevices.getDisplayMedia = vi.fn(() => new Promise(r => { pick = () => r(display) }))
    const share = rtc.startScreenShare()
    await vi.waitFor(() => expect(pick).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    pick()
    await share
    expect(display.getTracks().every(tr => tr.stop.mock.calls.length > 0)).toBe(true)
    expect(voice.isScreenSharing).toBe(false)
    expect(sent.some(e => e.type === 'webrtc_screenshare_start')).toBe(false)
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

  it('checks actual captured audio and requests echo cancellation when exclusion was not applied', async () => {
    const { rtc } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = display.getAudioTracks()[0]
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
    const track = display.getAudioTracks()[0]
    track.getSettings = () => ({ restrictOwnAudio: true, echoCancellation: false })
    track.applyConstraints = vi.fn()
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    expect(track.applyConstraints).not.toHaveBeenCalled()
    expect(useToastStore().toasts.filter(toast => toast.action)).toHaveLength(0)
  })

  it('warns once if capture protection cannot be confirmed and offers stream-only mute', async () => {
    const { rtc, mic, screenAudio, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = display.getAudioTracks()[0]
    track.getSettings = () => ({ restrictOwnAudio: false, echoCancellation: false })
    track.applyConstraints = vi.fn(async () => { throw new DOMException('not supported', 'OverconstrainedError') })
    stubDisplayMedia(display)
    await rtc.startScreenShare()
    const actionable = useToastStore().toasts.filter(toast => toast.action)
    expect(actionable).toHaveLength(1)
    expect(screenAudio.track).toBe(track)
    expect(track.enabled).toBe(true)
    actionable[0].action.onClick()
    expect(voice.isScreenAudioMuted).toBe(true)
    expect(track.enabled).toBe(false)
    expect(mic.getAudioTracks()[0].enabled).toBe(true)
    rtc.stopScreenShare()
    voice.isScreenAudioMuted = false
    actionable[0].action.onClick()
    expect(voice.isScreenAudioMuted).toBe(false)
  })

  it('does not attach screen audio after leaving while its fallback constraints are pending', async () => {
    const { rtc, voice, screenAudio, sent } = await joined()
    const display = fakeStream(['video', 'audio'])
    const track = display.getAudioTracks()[0]
    let resolve
    track.getSettings = () => ({ restrictOwnAudio: false })
    track.applyConstraints = vi.fn(() => new Promise(r => { resolve = r }))
    stubDisplayMedia(display)
    const starting = rtc.startScreenShare()
    await vi.waitFor(() => expect(resolve).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    resolve()
    await starting
    expect(screenAudio.replaceTrack).not.toHaveBeenCalledWith(track)
    expect(voice.hasScreenAudio).toBe(false)
    expect(sent.some(event => event.type === 'webrtc_screenshare_start')).toBe(false)
  })

  it('mute and the gate only silence the mic, never the screen audio', async () => {
    const { rtc, voice, screenAudio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleMute()
    expect(mic.getAudioTracks()[0].enabled).toBe(false)
    expect(screenAudio.track.enabled).toBe(true)
  })

  it("the streamer's stream mute silences only the screen audio line", async () => {
    const { rtc, voice, audio, screenAudio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(display.getAudioTracks()[0].enabled).toBe(false))
    expect(screenAudio.track).toBe(display.getAudioTracks()[0])
    expect(audio.track).toBe(mic.getAudioTracks()[0])
    expect(mic.getAudioTracks()[0].enabled).toBe(true)
    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(display.getAudioTracks()[0].enabled).toBe(true))
  })

  it('without screen audio the plain mic keeps being sent', async () => {
    const { rtc, audio, screenAudio, mic, voice } = await joined()
    const calls = audio.replaceTrack.mock.calls.length
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    expect(audio.replaceTrack.mock.calls.length).toBe(calls)
    expect(audio.track).toBe(mic.getAudioTracks()[0])
    expect(screenAudio.track).toBeNull()
    expect(voice.hasScreenAudio).toBe(false)
  })

  it('empties the screen lines when sharing stops', async () => {
    const { rtc, audio, screen, screenAudio, mic, sent, voice } = await joined()
    stubDisplayMedia(fakeStream(['video', 'audio']))
    await rtc.startScreenShare()

    rtc.stopScreenShare()
    await vi.waitFor(() => expect(screenAudio.track).toBeNull())
    expect(screen.track).toBeNull()
    expect(audio.track).toBe(mic.getAudioTracks()[0])
    expect(voice.hasScreenAudio).toBe(false)
    expect(sent.some(e => e.type === 'webrtc_screenshare_stop')).toBe(true)
  })

  it('silences the screen audio line when only the screen audio track ends', async () => {
    const { rtc, voice, screenAudio } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    display.getAudioTracks()[0].onended()
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

    expect(audio.track).toBe(lastMic.getAudioTracks()[0])
    expect(screenAudio.track).toBe(display.getAudioTracks()[0])
    expect(voice.hasScreenAudio).toBe(true)
  })

  it('a reconnect sends the screen audio on its line again', async () => {
    const { rtc, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    rtc.rejoinAfterReconnect()
    const fresh = await negotiate()
    expect(fresh.senders[0].track).toBe(mic.getAudioTracks()[0])
    expect(fresh.senders[1].track).toBe(display.getVideoTracks()[0])
    expect(fresh.senders[3].track).toBe(display.getAudioTracks()[0])
  })
})

describe('one screen share per person', () => {
  async function answer(result) {
    const { pendingConfirm } = await import('../lib/confirm')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    pendingConfirm.value.resolve(result)
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

    expect(screen.track).toBe(second.getVideoTracks()[0])
    expect(screenAudio.track).toBe(second.getAudioTracks()[0])
    expect(first.getTracks().every(tr => tr.stop.mock.calls.length > 0)).toBe(true)
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
    navigator.mediaDevices.getDisplayMedia.mockClear()

    const start = rtc.startScreenShare()
    await answer(false)
    await start

    expect(navigator.mediaDevices.getDisplayMedia).not.toHaveBeenCalled()
    expect(screen.track).toBe(first.getVideoTracks()[0])
    expect(first.getVideoTracks()[0].stop).not.toHaveBeenCalled()
    expect(toRaw(voice.localScreenStream)).toBe(first)
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

  function remoteStream(id) {
    const listeners = []
    return {
      id,
      addEventListener: (type, fn) => { if (type === 'removetrack') listeners.push(fn) },
      removeTrack: track => listeners.forEach(fn => fn({ track }))
    }
  }

  it('reuses the element when the SFU hands a track to another speaker', async () => {
    const { voice, pc } = await joined()
    const track = { ...fakeTrack('audio'), id: 't1' }
    const alice = remoteStream('alice')
    const bob = remoteStream('bob')
    pc.ontrack({ track, streams: [alice] })
    pc.ontrack({ track, streams: [bob] })
    expect(audioElements).toHaveLength(1)
    const el = audioElements[0]
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
    pc.ontrack({ track: previousTrack, streams: [previousStream] })
    const previousElement = audioElements.at(-1)
    voice.setStreamVolume('alice', 80)
    await vi.waitFor(() => expect(previousElement.volume).toBe(0.8))

    const replacementTrack = fakeTrack('audio')
    const replacementStream = remoteStream('screen:alice')
    pc.ontrack({ track: replacementTrack, streams: [replacementStream] })
    const replacementElement = audioElements.at(-1)
    expect(previousElement.srcObject).toBeNull()
    expect(previousElement.pause).toHaveBeenCalledOnce()
    expect(replacementElement.volume).toBe(0.8)
    expect(microphone.srcObject.getAudioTracks()).toHaveLength(1)
    expect(replacementElement.srcObject.getAudioTracks()).toEqual([replacementTrack])
    expect(audioElements.filter(el => el.srcObject).map(el => el.dataset.streamId)).toEqual(['alice', 'screen:alice'])

    previousTrack.onended()
    previousStream.removeTrack(previousTrack)
    expect(replacementElement.srcObject.getAudioTracks()).toEqual([replacementTrack])
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
    const previousSource = FakeAudioContext.sources.at(-1)
    const previousGain = FakeAudioContext.gains.at(-1)
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
    const mixedIncoming = { ...streamOf([video, mic, audio]), id: 'alice' }
    voice.watchScreen('alice')
    pc.ontrack({ track: mic, streams: [mixedIncoming] })
    pc.ontrack({ track: audio, streams: [{ id: 'screen:alice' }] })
    pc.ontrack({ track: audio, streams: [{ id: 'screen:alice' }] })
    pc.ontrack({ track: video, streams: [mixedIncoming] })
    expect(audioElements).toHaveLength(2)
    expect(audioElements[0].srcObject.getAudioTracks()).toEqual([mic])
    expect(audioElements[1].srcObject.getAudioTracks()).toEqual([audio])
    expect(voice.remoteScreenStream.getVideoTracks()).toEqual([video])
    expect(voice.remoteScreenStream.getAudioTracks()).toEqual([])
  })

  it('does not let callbacks from a replaced track object remove its replacement with the same ID', async () => {
    const { pc } = await joined()
    const previous = { ...fakeTrack('audio'), id: 'reused' }
    const next = { ...fakeTrack('audio'), id: 'reused' }
    const stream = remoteStream('screen:alice')
    pc.ontrack({ track: previous, streams: [stream] })
    const old = audioElements.at(-1)
    pc.ontrack({ track: next, streams: [stream] })
    const current = audioElements.at(-1)
    expect(old.srcObject).toBeNull()
    previous.onended()
    stream.removeTrack(previous)
    expect(current.srcObject.getAudioTracks()).toEqual([next])
    next.onended()
    expect(current.srcObject).toBeNull()
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
    expect(screenParams.encodings[0].maxBitrate).toBe(streamBitrate(DEFAULT_STREAM_QUALITY))
    expect(screenParams.encodings[0].maxBitrate).toBeLessThanOrEqual(SCREEN_MAX_BITRATE)
    expect(screenParams.degradationPreference).toBe('maintain-resolution')
    expect(camera.setParameters.mock.calls[0][0].encodings[0].maxBitrate).toBe(CAMERA_MAX_BITRATE)
  })

  it('tunes the senders again after a reconnect', async () => {
    const { rtc, voice } = await joined()
    voice.qosHighPriority = true
    navigator.mediaDevices.getUserMedia.mockImplementationOnce(async () => fakeStream(['video']))
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    await rtc.startCamera()

    rtc.rejoinAfterReconnect()
    const [audio, screen, camera] = (await negotiate()).senders
    await vi.waitFor(() => expect(camera.setParameters).toHaveBeenCalled())
    const screenParams = screen.setParameters.mock.calls.at(-1)[0]
    expect(screenParams.encodings[0].maxBitrate).toBe(streamBitrate(DEFAULT_STREAM_QUALITY))
    expect(screenParams.degradationPreference).toBe('maintain-resolution')
    expect(camera.setParameters.mock.calls.at(-1)[0].encodings[0].maxBitrate).toBe(CAMERA_MAX_BITRATE)
    expect(audio.setParameters.mock.calls.at(-1)[0].encodings[0].priority).toBe('high')
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

describe('stream quality of my own share', () => {
  // A 4K screen whose capture the browser cannot scale (settings stay 4K),
  // or one it scales to the constrained height.
  function display4k({ scales = false } = {}) {
    const stream = fakeStream(['video', 'audio'])
    const track = stream.getVideoTracks()[0]
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
  const lastParams = sender => sender.setParameters.mock.calls.at(-1)[0]

  it('captures and sends a new share at 1080p and 30 fps', async () => {
    const { rtc, screen, voice } = await joined()
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    const { stream, track } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    // Every new share starts at the default, whatever the last one used.
    expect(voice.screenQuality).toEqual({ resolution: 1080, fps: 30 })
    expect(navigator.mediaDevices.getDisplayMedia.mock.calls[0][0].video).toEqual({ frameRate: { ideal: 30, max: 30 }, height: { max: 1080 } })
    const enc = lastParams(screen).encodings[0]
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
    await vi.waitFor(() => expect(lastParams(screen).encodings[0].maxFramerate).toBe(15))
    expect(track.applyConstraints).toHaveBeenLastCalledWith({ frameRate: { ideal: 15, max: 15 }, height: { max: 720 } })
    const enc = lastParams(screen).encodings[0]
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
    await vi.waitFor(() => expect(lastParams(screen).encodings[0].maxFramerate).toBe(60))
    expect(track.contentHint).toBe('motion')
    expect(lastParams(screen).degradationPreference).toBe('balanced')

    voice.setScreenQuality({ resolution: 'source', fps: 15 })
    await vi.waitFor(() => expect(lastParams(screen).encodings[0].maxFramerate).toBe(15))
    expect(track.applyConstraints).toHaveBeenLastCalledWith({ frameRate: { ideal: 15, max: 15 } })
    expect(track.contentHint).toBe('detail')
    expect(lastParams(screen).degradationPreference).toBe('maintain-resolution')
    expect(lastParams(screen).encodings[0].scaleResolutionDownBy).toBe(1)
  })

  it('a reconnect sends in the chosen quality again', async () => {
    const { rtc, voice } = await joined()
    const { stream } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    await new Promise(r => setTimeout(r, 0))

    rtc.rejoinAfterReconnect()
    const [, screen] = (await negotiate()).senders
    await vi.waitFor(() => expect(screen.setParameters).toHaveBeenCalled())
    const enc = lastParams(screen).encodings[0]
    expect(enc.maxFramerate).toBe(15)
    expect(enc.scaleResolutionDownBy).toBe(3)
    expect(enc.maxBitrate).toBe(streamBitrate({ resolution: 720, fps: 15 }, { width: 3840, height: 2160 }))
  })

  it('stopping resets the quality for the next share', async () => {
    const { rtc, voice } = await joined()
    stubDisplayMedia(display4k().stream)
    await rtc.startScreenShare()
    voice.setScreenQuality({ resolution: 1440, fps: 60 })
    rtc.stopScreenShare()
    expect(voice.screenQuality).toEqual({ resolution: 1080, fps: 30 })
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
    expect(stream.getTracks()[0].stop).toHaveBeenCalled()
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
    expect(firstStream.getTracks()[0].stop).toHaveBeenCalled()
    expect(secondStream.getTracks()[0].stop).not.toHaveBeenCalled()
    expect(FakeAudioContext.instances).toHaveLength(1)

    rtc.stopMicTest()
    expect(secondStream.getTracks()[0].stop).toHaveBeenCalled()
    expect(FakeAudioContext.instances[0].closed).toBe(true)
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
    expect(FakeAudioContext.instances[0].sinkId).toBe('headset')
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


describe('publishMids', () => {
  it('finds the SFU receive lines for mic, screen, camera and screen audio', () => {
    const offer = [
      'v=0',
      'm=audio 9 UDP 111', 'a=mid:0', 'a=recvonly',
      'm=video 9 UDP 96', 'a=mid:1', 'a=recvonly',
      'm=video 9 UDP 96', 'a=mid:2', 'a=recvonly',
      'm=audio 9 UDP 111', 'a=mid:3', 'a=recvonly',
      // forwarded tracks of someone else: not ours to send on
      'm=audio 9 UDP 111', 'a=mid:4', 'a=sendonly',
      'm=video 9 UDP 96', 'a=mid:5', 'a=sendonly',
      ''
    ].join('\r\n')
    const ours = { audio: '0', screenAudio: '3', video: ['1', '2'] }
    expect(publishMids(offer)).toEqual(ours)
    // Someone was already talking: the SFU forwards their voice on our mic
    // line, which then reads sendrecv. It is still our microphone line.
    expect(publishMids(offer.replace('a=mid:0\r\na=recvonly', 'a=mid:0\r\na=sendrecv'))).toEqual(ours)
    expect(publishMids('')).toEqual({ audio: null, screenAudio: null, video: [] })
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
