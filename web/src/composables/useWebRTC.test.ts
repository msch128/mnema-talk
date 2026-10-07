import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { TestTrack, TestStream, present } from '../media-test.fixture'
import type { NoiseModel, FilterOptions } from '../lib/dfnTypes'
type ClientSentEvent = { type: keyof ClientEventPayloads; payload: ClientEventPayloads[keyof ClientEventPayloads] }
import type { ClientEventPayloads } from '../types/events'
import { decodeServerEvent } from '../types/events'
import { fixtureId } from '../test-fixtures.fixture'
import { toRaw, nextTick } from 'vue'
import * as voiceSession from '../lib/voiceSession'
import { t } from '../i18n'
import { publishMids, tuneScreenOffer, screenAudioConstraints, useWebRTC, SCREEN_MAX_BITRATE, CAMERA_MAX_BITRATE, SCREEN_START_KBPS, SCREEN_MIN_KBPS } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { streamBitrate, DEFAULT_STREAM_QUALITY } from '../lib/streamQuality'

// The AI filter worklets can't run in jsdom; tests hand in a fake node instead.
interface SuppressorDouble { connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn>; destroy: ReturnType<typeof vi.fn> }
const suppressor = vi.hoisted((): { node: SuppressorDouble | null; models: NoiseModel[]; options: FilterOptions[]; create: (() => Promise<SuppressorDouble | null>) | null; supported: boolean } => ({ node: null, models: [], options: [], create: null, supported: false }))
vi.mock('../lib/noiseSuppressor', () => ({
  isNoiseSuppressionSupported: () => suppressor.supported || suppressor.node !== null,
  preloadNoiseSuppressor: () => {},
  createNoiseSuppressorNode: async (_ctx: AudioContext, model: NoiseModel, options: FilterOptions) => {
    suppressor.models.push(model)
    suppressor.options.push(options)
    return suppressor.create ? suppressor.create() : suppressor.node
  }
}))

vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

// --- Browser API stand-ins -------------------------------------------------

function fakeTrack(kind = 'audio') { return new TestTrack(kind) }
function streamOf(tracks: MediaStreamTrack[]) { return new TestStream(tracks) }
function attachedStream(element: HTMLMediaElement): MediaStream {
  const stream = element.srcObject
  if (!(stream instanceof TestStream)) throw new Error('Expected synthetic playback stream')
  return stream
}
function remoteStreamFor(id: string) { const stream = new TestStream(); stream.id = id; return stream }
function fakeStream(kinds = ['audio']) { return streamOf(kinds.map(kind => fakeTrack(kind))) }

function fakeSender(track: MediaStreamTrack | null) {
  const s = {
    track,
    replaceTrack: vi.fn(async (t: MediaStreamTrack | null) => { s.track = t }),
    getStats: vi.fn(async (): Promise<Map<string, import('../lib/mediaStats').MediaStat>> => new Map()),
    getParameters: (): RTCRtpSendParameters => ({ transactionId: 'test', codecs: [], headerExtensions: [], rtcp: {}, encodings: [{}] }),
    setParameters: vi.fn(async (_params?: RTCRtpSendParameters) => {})
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

interface FakeTransceiver { mid: string; direction: RTCRtpTransceiverDirection; sender: ReturnType<typeof fakeSender> }
class FakePC {
  transceivers: FakeTransceiver[] = []
  unmatched: ReturnType<typeof fakeSender>[] = []
  remoteDescription: RTCSessionDescriptionInit | null = null
  candidates: RTCIceCandidateInit[] = []
  closed = false
  onicecandidate: ((event: { candidate: { toJSON: () => RTCIceCandidateInit; protocol?: string; type?: string } | null }) => void) | null = null
  ontrack: ((event: { track: MediaStreamTrack; streams: MediaStream[] }) => void) | null = null
  onconnectionstatechange: (() => void) | null = null
  connectionState: RTCPeerConnectionState = 'new'
  iceConnectionState: RTCIceConnectionState = 'new'
  iceGatheringState: RTCIceGatheringState = 'new'
  signalingState: RTCSignalingState = 'stable'
  onicecandidateerror: ((event: { errorCode: number; errorText?: string; url?: string }) => void) | null = null
  onicegatheringstatechange: (() => void) | null = null
  oniceconnectionstatechange: (() => void) | null = null

  static instances: FakePC[] = []
  constructor(public configuration: RTCConfiguration = {}) {
    this.transceivers = []
    // Lines added locally: like in a browser, never matched to a remote offer.
    this.unmatched = []
    this.remoteDescription = null
    this.candidates = []
    this.closed = false
    FakePC.instances.push(this)
  }
  addTrack(track: MediaStreamTrack) { const s = fakeSender(track); this.unmatched.push(s); return s }
  addTransceiver(trackOrKind: MediaStreamTrack | string) {
    const s = fakeSender(typeof trackOrKind === 'string' ? null : trackOrKind)
    this.unmatched.push(s)
    return { sender: s }
  }
  // Senders on the negotiated lines, in m-line order (mic, screen, camera).
  get senders() { return this.transceivers.map(t => t.sender) }
  getSenders() { return this.senders }
  getTransceivers() { return this.transceivers }
  async setRemoteDescription(d: RTCSessionDescriptionInit) {
    this.remoteDescription = d
    for (const m of String(d.sdp).matchAll(/a=mid:(\S+)/g)) {
      if (!this.transceivers.some(t => t.mid === m[1])) {
        this.transceivers.push({ mid: present(m[1]), direction: 'recvonly', sender: fakeSender(null) })
      }
    }
  }
  async createAnswer(): Promise<RTCSessionDescriptionInit> { return { type: 'answer', sdp: 'a' } }
  async setLocalDescription() {}
  async addIceCandidate(c: RTCIceCandidateInit) { this.candidates.push(c) }
  async getStats(): Promise<Map<string, import('../lib/mediaStats').MediaStat>> { return new Map() }
  close() { this.closed = true }
}

interface FakeNode { connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }
interface FakeDestination extends FakeNode { stream: TestStream }
interface FakeSource extends FakeNode { stream: MediaStream }
interface FakeGain extends FakeNode { gain: { value: number; setValueAtTime: ReturnType<typeof vi.fn> } }
class FakeAudioContext {
  state: AudioContextState = 'running'
  currentTime = 0
  destination = {}
  sinkId = ''
  closed = false

  static destinations: FakeDestination[] = []
  static sources: FakeSource[] = []
  static gains: FakeGain[] = []
  static instances: FakeAudioContext[] = []
  static sample = 150
  static analysers: Array<{ fftSize: number; connect: () => void; getByteTimeDomainData: ReturnType<typeof vi.fn> }> = []
  constructor() {
    this.state = 'running'
    this.currentTime = 0
    this.destination = {}
    this.sinkId = ''
    this.closed = false
    FakeAudioContext.instances.push(this)
  }
  setSinkId(id: string) { this.sinkId = id; return Promise.resolve() }
  createMediaStreamDestination(): FakeDestination {
    const node = { stream: fakeStream(), connect: vi.fn(), disconnect: vi.fn() }
    FakeAudioContext.destinations.push(node)
    return node
  }
  createGain(): FakeGain {
    const node = {
      gain: {
        value: 1,
        setValueAtTime: vi.fn(function (this: { value: number }, v: number) { this.value = v })
      },
      connect: vi.fn(),
      disconnect: vi.fn()
    }
    FakeAudioContext.gains.push(node)
    return node
  }
  createAnalyser() {
    const analyser = { fftSize: 256, connect() {}, getByteTimeDomainData: vi.fn((buffer: Uint8Array) => buffer.fill(FakeAudioContext.sample)) }
    FakeAudioContext.analysers.push(analyser)
    return analyser
  }
  createMediaStreamSource(stream: MediaStream): FakeSource {
    const node = { stream, connect: vi.fn(), disconnect: vi.fn() }
    FakeAudioContext.sources.push(node)
    return node
  }
  createBiquadFilter() { return { type: '', frequency: { setValueAtTime() {} }, connect() {} } }
  resume() { return Promise.resolve() }
  close() { this.closed = true; return Promise.resolve() }
}

type MicRequest = (() => void) & { stream: TestStream }
let micRequests: MicRequest[]
let audioElements: HTMLAudioElement[]
beforeEach(() => {
  audioElements = []
  localStorage.clear()
  setActivePinia(createPinia())
  suppressor.node = null
  suppressor.models = []
  suppressor.options = []
  suppressor.create = null
  suppressor.supported = false
  FakeAudioContext.destinations = []
  FakeAudioContext.sources = []
  FakeAudioContext.gains = []
  FakeAudioContext.instances = []
  FakeAudioContext.sample = 150
  FakeAudioContext.analysers = []
  FakePC.instances = []
  micRequests = []
  vi.stubGlobal('RTCPeerConnection', FakePC)
  vi.stubGlobal('RTCSessionDescription', function (d: RTCSessionDescriptionInit) { return d })
  vi.stubGlobal('RTCIceCandidate', function (c: RTCIceCandidateInit) { return c })
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal('MediaStream', function (tracks: MediaStreamTrack[]) { return streamOf(tracks) })
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
      getUserMedia: vi.fn(() => new Promise<MediaStream>(resolve => {
        const stream = fakeStream()
        micRequests.push(Object.assign(() => resolve(stream), { stream }))
      }))
    }
  })
})

afterEach(() => {
  const { leaveVoiceChannel, stopMicTest } = useWebRTC()
  stopMicTest()
  leaveVoiceChannel()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function setup() {
  const chat = useChatStore()
  const sent: ClientSentEvent[] = []
  chat.sendWSEvent = (type, payload) => { sent.push({ type, payload }) }
  return { chat, voice: useVoiceStore(), rtc: useWebRTC(), sent }
}

// The SFU sends its offer once the join is in; the answer binds our senders.
async function negotiate() {
  const pc = present(FakePC.instances.at(-1))
  useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(4))
  await new Promise(r => setTimeout(r, 0))
  return pc
}

async function grantMic() {
  await vi.waitFor(() => expect(micRequests.length).toBeGreaterThan(0))
  const req = present(micRequests.shift())
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
    expect(present(stream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.currentChannelId).toBeNull()
  })

  it('does not hang on an audio context that waits for a user gesture', async () => {
    class GestureLockedContext extends FakeAudioContext {
      constructor() { super(); this.state = 'suspended' }
      resume() { return new Promise<void>(() => {}) }
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
    const joins = sent.filter(e => e.type === 'voice_join').map(e => ('channel_id' in e.payload ? e.payload.channel_id : undefined))
    expect(joins).toEqual(['ch-2'])
    expect(present(firstStream.getTracks()[0]).stop).toHaveBeenCalled()
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
    const pc = present(FakePC.instances.at(-1))

    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(voice.currentChannelId).toBeNull()
    expect(pc.closed).toBe(true)
    expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'voice_leave')).toBe(false)
    expect(recent()).toBeNull()
    expect(useToastStore().toasts.map(x => x.text)).toContain(t('voice.kicked'))

    const joins = sent.filter(e => e.type === 'voice_join').length
    rtc.rejoinAfterReconnect()
    expect(sent.filter(e => e.type === 'voice_join').length).toBe(joins)
  })

  it.each(['de', 'en'] as const)('ends a refused full-room join with the %s capacity message and no retry', async language => {
    const { rtc, chat, voice, sent } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { setLocale, t } = await import('../i18n')
    const { recent } = await import('../lib/voiceSession')
    setLocale(language)
    try {
      const channelId = fixtureId(301)
      const join = rtc.joinVoiceChannel(channelId)
      const mic = await grantMic()
      await join
      const pc = present(FakePC.instances.at(-1))
      const joins = sent.filter(event => event.type === 'voice_join').length
      const refusal = decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: channelId, reason: 'room_full' } })
      expect(refusal).toEqual({ type: 'voice_kicked', payload: { channel_id: channelId, reason: 'room_full' } })
      chat.handleWSEvent(present(refusal))
      expect(voice.currentChannelId).toBeNull()
      expect(voice.isConnected).toBe(false)
      expect(pc.closed).toBe(true)
      expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
      expect(recent()).toBeNull()
      expect(sent.some(event => event.type === 'voice_leave')).toBe(false)
      expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('voice.roomFull'))
      expect(useToastStore().toasts.map(toast => toast.text)).not.toContain(t('voice.kicked'))
      expect(t('voice.roomFull')).toBe(language === 'de'
        ? 'Dieser Talk ist voll. Versuche es erneut, sobald jemand den Talk verlässt.'
        : 'This voice channel is full. Try again when someone leaves.')
      rtc.rejoinAfterReconnect()
      expect(sent.filter(event => event.type === 'voice_join')).toHaveLength(joins)
    } finally { setLocale('de') }
  })

  it('ignores a stale full-room refusal for a different channel', async () => {
    const { rtc, chat, voice } = setup()
    const { useToastStore } = await import('../stores/toast')
    const join = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1', reason: 'room_full' } })
    expect(voice.currentChannelId).toBe('ch-2')
    expect(present(FakePC.instances.at(-1)).closed).toBe(false)
    expect(useToastStore().toasts).toEqual([])
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
    const old = present(FakePC.instances.at(-1))
    let release: (() => void) | undefined
    const original = old.setRemoteDescription.bind(old)
    old.setRemoteDescription = vi.fn(async description => {
      await new Promise<void>(resolve => { release = resolve })
      await original(description)
    })
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.rejoinAfterReconnect()
    const replacement = present(FakePC.instances.at(-1))
    present(release)()
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
    const old = present(FakePC.instances.at(-1))
    let release: ((value: RTCSessionDescriptionInit) => void) | undefined
    old.createAnswer = vi.fn(() => new Promise<RTCSessionDescriptionInit>(resolve => { release = resolve }))
    old.setLocalDescription = vi.fn(async () => {})
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(release)({ type: 'answer', sdp: 'old-session' })
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(old.setLocalDescription).not.toHaveBeenCalled()
    expect(sent.filter(event => event.type === 'webrtc_answer')).toEqual([])
  })

  it('ignores ICE and media callbacks from a retired connection', async () => {
    const { rtc, voice, sent } = await joined()
    const old = present(FakePC.instances.at(-1))
    rtc.rejoinAfterReconnect()
    const before = sent.length
    present(old.onicecandidate)({ candidate: { toJSON: () => ({ candidate: 'retired' }) } })
    present(old.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('cam:retired')] })
    expect(sent).toHaveLength(before)
    expect(voice.userVideoStreams).toEqual({})
  })

  it('configures screen limits before restoring a share on a negotiated new sender', async () => {
    const { rtc } = await joined()
    const stream = fakeStream(['video'])
    present(stream.getVideoTracks()[0]).getSettings = () => ({ width: 1920, height: 1080 })
    navigator.mediaDevices.getDisplayMedia = vi.fn().mockResolvedValue(stream)
    await rtc.startScreenShare()
    rtc.rejoinAfterReconnect()
    const replacement = await negotiate()
    const screen = present(replacement.senders[1])
    expect(screen.track).toBe(present(stream.getVideoTracks()[0]))
    expect(present(screen.setParameters.mock.invocationCallOrder[0])).toBeLessThan(present(screen.replaceTrack.mock.invocationCallOrder[0]))
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
    const pc = present(FakePC.instances.at(-1))
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
    const pc = present(FakePC.instances.at(-1))
    const offer = SFU_OFFER.replace(/(m=video 9 UDP\/TLS\/RTP\/SAVPF) 96(\r\na=mid:(\d))/g, (_, m, rest) =>
      `${m} 96 102${rest}\r\na=rtpmap:96 VP8/90000\r\na=rtpmap:102 H264/90000\r\na=fmtp:102 packetization-mode=1;profile-level-id=42e01f`)
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: offer } })
    await vi.waitFor(() => expect(present(pc.remoteDescription)).not.toBeNull())
    const applied = present(pc.remoteDescription).sdp
    expect(present(pc.remoteDescription).type).toBe('offer')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 102 96\r\na=mid:1')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 96 102\r\na=mid:2')
    expect(applied).toContain('x-google-start-bitrate')
  })

  it('reconnect starts a fresh connection and announces the join again', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const oldPC = present(FakePC.instances.at(-1))
    rtc.rejoinAfterReconnect()
    expect(oldPC.closed).toBe(true)
    expect(present(FakePC.instances.at(-1))).not.toBe(oldPC)
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

// --- Screen share audio, per-user volume, camera ----------------------------

async function joined() {
  const ctx = setup()
  const join = ctx.rtc.joinVoiceChannel('ch-1')
  const mic = await grantMic()
  await join
  const pc = await negotiate()
  const audio = present(pc.senders[0]), screen = present(pc.senders[1]), camera = present(pc.senders[2]), screenAudio = present(pc.senders[3])
  return { ...ctx, mic, pc, audio, screen, camera, screenAudio }
}

function stubDisplayMedia(stream: MediaStream) {
  navigator.mediaDevices.getDisplayMedia = vi.fn(async () => stream)
}

describe('screen share with audio', () => {
  it('sends the screen audio on its own line, apart from the mic', async () => {
    const { rtc, audio, screen, screenAudio, mic, voice } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)

    await rtc.startScreenShare()

    expect(screen.track).toBe(present(display.getVideoTracks()[0]))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    // The mic line keeps the bare microphone: nothing is mixed into the voice.
    expect(audio.track).toBe(present(mic.getAudioTracks()[0]))
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
    const { rtc, mic, screenAudio, voice } = await joined()
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
    expect(present(mic.getAudioTracks()[0]).enabled).toBe(true)
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
    const { rtc, voice, screenAudio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleMute()
    expect(present(mic.getAudioTracks()[0]).enabled).toBe(false)
    expect(present(screenAudio.track).enabled).toBe(true)
  })

  it("the streamer's stream mute silences only the screen audio line", async () => {
    const { rtc, voice, audio, screenAudio, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(present(display.getAudioTracks()[0]).enabled).toBe(false))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    expect(audio.track).toBe(present(mic.getAudioTracks()[0]))
    expect(present(mic.getAudioTracks()[0]).enabled).toBe(true)
    voice.toggleScreenAudioMute()
    await vi.waitFor(() => expect(present(display.getAudioTracks()[0]).enabled).toBe(true))
  })

  it('without screen audio the plain mic keeps being sent', async () => {
    const { rtc, audio, screenAudio, mic, voice } = await joined()
    const calls = audio.replaceTrack.mock.calls.length
    stubDisplayMedia(fakeStream(['video']))
    await rtc.startScreenShare()
    expect(audio.replaceTrack.mock.calls.length).toBe(calls)
    expect(audio.track).toBe(present(mic.getAudioTracks()[0]))
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
    expect(audio.track).toBe(present(mic.getAudioTracks()[0]))
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

    expect(audio.track).toBe(present(lastMic.getAudioTracks()[0]))
    expect(screenAudio.track).toBe(present(display.getAudioTracks()[0]))
    expect(voice.hasScreenAudio).toBe(true)
  })

  it('a reconnect sends the screen audio on its line again', async () => {
    const { rtc, mic } = await joined()
    const display = fakeStream(['video', 'audio'])
    stubDisplayMedia(display)
    await rtc.startScreenShare()

    rtc.rejoinAfterReconnect()
    const fresh = await negotiate()
    expect(present(fresh.senders[0]).track).toBe(present(mic.getAudioTracks()[0]))
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
    voice.setScreenQuality({ resolution: 720, fps: 15 })
    const { stream, track } = display4k()
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    // Every new share starts at the default, whatever the last one used.
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
  const subs = (sent: ClientSentEvent[]) => sent.filter(e => e.type === 'webrtc_subscribe').map(e => e.payload)

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
    voice.mediaState = { bob: { channel_id: 'ch-1', screen: true, camera: false } }
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
    chat.handleWSEvent({ type: 'webrtc_media_state', payload: { channel_id: 'ch-1', user_id: 'bob', screen: true, camera: false } })
    expect(voice.mediaState.bob).toEqual({ channel_id: 'ch-1', screen: true, camera: false })
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
  const video = (mid: string) => [
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
  const section = (sdp: string, mid: string) => {
    const parts = sdp.split('\r\nm=')
    return present(parts.find(p => p.includes('a=mid:' + mid + '\r\n'))).split('\r\n')
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
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'silent' } })
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
    expect(present(pc.senders[0]).track).toBe(replacement.getAudioTracks()[0])
    expect(present(replacement.getAudioTracks()[0]).enabled).toBe(false)
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
    present(pc.ontrack)({ track: fakeTrack(), streams: [] })
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

describe('microphone pipeline fallbacks and filter cancellation', () => {
  function node(): SuppressorDouble { return { connect: vi.fn(), disconnect: vi.fn(), destroy: vi.fn() } }

  it('falls back to the captured mic when AudioContext construction fails', async () => {
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

describe('negotiation retry, ordering and sender capabilities', () => {
  it.each(['setRemoteDescription', 'createAnswer', 'setLocalDescription'] as const)('reports %s failures and accepts the next offer', async method => {
    const { chat, pc, sent } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fail = vi.spyOn(pc, method).mockRejectedValueOnce(new Error('rejected SDP'))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(sent).toContainEqual({ type: 'webrtc_diag', payload: { event: 'negotiation_error', error: 'rejected SDP' } }))
    fail.mockRestore()
    const answers = sent.filter(e => e.type === 'webrtc_answer').length
    await negotiate()
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(answers + 1)
  })

  it('reports non-Error rejection values without blocking later offers', async () => {
    const { chat, pc, sent } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(pc, 'setRemoteDescription').mockRejectedValueOnce('invalid')
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: '' } })
    await vi.waitFor(() => expect(sent).toContainEqual({ type: 'webrtc_diag', payload: { event: 'negotiation_error', error: 'invalid' } }))
  })

  it('drops ICE after leaving and handles rejected live candidates while preserving later candidates', async () => {
    const { rtc, chat, pc } = await joined()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(pc, 'addIceCandidate').mockRejectedValueOnce(new Error('bad candidate'))
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'bad' } })
    await vi.waitFor(() => expect(warn).toHaveBeenCalled())
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'good' } })
    await vi.waitFor(() => expect(pc.candidates).toEqual([{ candidate: 'good' }]))
    rtc.leaveVoiceChannel()
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'late' } })
    expect(pc.candidates).toEqual([{ candidate: 'good' }])
  })

  it('suppresses rejected ICE warnings from a replaced connection', async () => {
    const { rtc, chat, pc } = await joined()
    let fail: ((reason: Error) => void) | undefined
    pc.addIceCandidate = vi.fn(() => new Promise<void>((_resolve, reject) => { fail = reject }))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'pending' } })
    rtc.rejoinAfterReconnect()
    present(fail)(new Error('old connection'))
    await Promise.resolve()
    expect(warn).not.toHaveBeenCalled()
  })

  it('answers despite a rejected queued ICE candidate', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    vi.spyOn(pc, 'addIceCandidate').mockRejectedValueOnce(new Error('candidate'))
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'early' } })
    await negotiate()
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
  })

  it.each(['candidate', 'local', 'tuning'] as const)('drops a retired offer after its %s await', async stage => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    let finish: (() => void) | undefined
    const defer = () => new Promise<void>(resolve => { finish = resolve })
    if (stage === 'candidate') {
      chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'early' } })
      pc.addIceCandidate = vi.fn(defer)
    } else if (stage === 'local') pc.setLocalDescription = vi.fn(defer)
    else {
      const original = pc.setRemoteDescription.bind(pc)
      pc.setRemoteDescription = async description => {
        await original(description)
        present(pc.senders[0]).setParameters = vi.fn(defer)
      }
    }
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(finish)()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(stage === 'tuning' ? 1 : 0)
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'negotiation_error')).toEqual([])
  })

  it('ignores a rejected obsolete offer instead of publishing its diagnostics', async () => {
    const { rtc, chat, pc, sent } = await joined()
    let fail: ((reason: Error) => void) | undefined
    pc.setRemoteDescription = vi.fn(() => new Promise<void>((_resolve, reject) => { fail = reject }))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(fail).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(fail)(new Error('obsolete'))
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'negotiation_error')).toEqual([])
  })

  it('negotiates offers without publish lines and with a partial mic line', async () => {
    const { chat, pc, sent } = await joined()
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'v=0\r\n' } })
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(2))
    expect(pc.unmatched).toEqual([])
  })

  it('tolerates browsers rejecting capability lookup and tuning fields', async () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => { throw new Error('unsupported') } })
    const debug = vi.spyOn(console, 'debug').mockImplementation(() => {})
    const { rtc, chat, voice, sent } = setup()
    voice.qosHighPriority = false
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    const original = pc.setRemoteDescription.bind(pc)
    pc.setRemoteDescription = async description => {
      await original(description)
      for (const sender of pc.senders) {
        sender.getParameters = () => ({ transactionId: '', codecs: [], headerExtensions: [], rtcp: {}, encodings: [] })
        sender.setParameters.mockRejectedValue(new Error('unknown field'))
      }
    }
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(debug).toHaveBeenCalledTimes(4))
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
    expect(present(present(pc.senders[0]).setParameters.mock.calls[0])?.[0]?.encodings[0]).toMatchObject({ priority: 'medium', networkPriority: 'medium' })
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

describe('remote rendering and audio output robustness', () => {
  it('plays unidentified audio at the master volume and ignores anonymous video for user maps', async () => {
    const { pc, voice } = await joined()
    voice.outputVolume = 40
    const track = fakeTrack()
    present(pc.ontrack)({ track, streams: [] })
    const el = present(audioElements.at(-1))
    expect(el.volume).toBe(0.4)
    expect(el.dataset.userId).toBeUndefined()
    present(pc.ontrack)({ track, streams: [remoteStreamFor('screen:')] })
    expect(el.dataset.userId).toBeUndefined()
    expect(el.dataset.source).toBe('screen')
    present(pc.ontrack)({ track, streams: [] })
    expect(el.dataset.source).toBeUndefined()
    const video = fakeTrack('video')
    present(pc.ontrack)({ track: video, streams: [] })
    present(video.onended).call(video, new Event('ended'))
    expect(voice.userVideoStreams).toEqual({})
    expect(voice.remoteScreenStream).toBeNull()
    const camera = fakeTrack('video')
    present(pc.ontrack)({ track: camera, streams: [remoteStreamFor('cam:')] })
    present(camera.onended).call(camera, new Event('ended'))
    expect(voice.userVideoStreams).toEqual({})
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

describe('SDP capability edge cases', () => {
  it('ignores unknown media, unnumbered lines and forwarded lines beyond four publish slots', () => {
    expect(publishMids('v=0\nm=audio 9 UDP 1\nm=application 9 UDP 1\na=mid:data\nm=audio 9 UDP 1\na=mid:mic\nm=audio 9 UDP 1\na=mid:sound\nm=audio 9 UDP 1\na=mid:other\nm=video 9 UDP 96\na=mid:screen\nm=video 9 UDP 96\na=mid:camera\nm=video 9 UDP 96\na=mid:other-video')).toEqual({ audio: 'mic', screenAudio: 'sound', video: ['screen', 'camera'] })
    expect(publishMids(null)).toEqual({ audio: null, screenAudio: null, video: [] })
  })

  it('preserves LF, existing Google codec hints and ranks unconstrained H264 after baseline', () => {
    const sdp = ['v=0', 'm=video 9 UDP 96 102 103 104 105 106', 'a=mid:screen', 'a=rtpmap:96 VP8/90000', 'a=fmtp:96 x-google-start-bitrate=2000', 'a=rtpmap:102 H264/90000', 'a=fmtp:102 packetization-mode=1;profile-level-id=64001f', 'a=rtpmap:103 H264/90000', 'a=fmtp:103 packetization-mode=1', 'a=rtpmap:104 H264/90000', 'a=rtpmap:105 red/90000', ''].join('\n')
    const out = tuneScreenOffer(sdp, 'screen', { h264: true })
    expect(out.split('\n')[1]).toBe('m=video 9 UDP 102 103 104 96 105 106')
    expect(out).toContain('a=fmtp:96 x-google-start-bitrate=2000\n')
    expect(out).not.toContain('\r\n')
    expect(tuneScreenOffer(sdp, 'screen', { startKbps: 0 })).toBe(sdp)
    expect(tuneScreenOffer(sdp, undefined)).toBe(sdp)
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

describe('server ICE configuration initialization', () => {
  it('retries a failed config request on a later join and applies only validated servers', async () => {
    vi.resetModules()
    const isolated = (await import('./useWebRTC')).useWebRTC()
    const chat = useChatStore()
    chat.sendWSEvent = vi.fn()
    const fetch = vi.mocked(globalThis.fetch)
    fetch.mockRejectedValueOnce(new Error('offline'))
    const first = isolated.joinVoiceChannel('first')
    await grantMic()
    await first
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([])
    isolated.leaveVoiceChannel()
    fetch.mockResolvedValueOnce(new Response(JSON.stringify({ ice_servers: [{ urls: ['stun:stun.example.invalid'] }] }), { status: 200, headers: { 'content-type': 'application/json' } }))
    const second = isolated.joinVoiceChannel('second')
    await grantMic()
    await second
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([{ urls: ['stun:stun.example.invalid'] }])
    isolated.leaveVoiceChannel()
  })

  it('handles an HTTP config failure without preventing a local voice connection', async () => {
    vi.resetModules()
    const isolated = (await import('./useWebRTC')).useWebRTC()
    useChatStore().sendWSEvent = vi.fn()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    const join = isolated.joinVoiceChannel('local-only')
    await grantMic()
    await join
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([])
    isolated.leaveVoiceChannel()
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
    const track = present(stream.getAudioTracks()[0])
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
