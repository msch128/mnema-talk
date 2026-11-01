import { beforeEach, afterEach, expect, vi } from 'vitest'
import { toRaw } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { TestTrack, TestStream, present } from '../media-test.fixture'
import type { ClientEventPayloads } from '../types/events'
import { installMediaCall } from '../lib/media/api'
import type { MediaCall, MediaNotice } from '../lib/media/api'
import type { MediaVerification } from '../lib/media/verify'
import { useWebRTC } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { resetNotices } from './webrtc/audioNotice'
import { suppressor } from './useWebRTC.noiseSuppressor.fixture'

export type ClientSentEvent = { type: keyof ClientEventPayloads; payload: ClientEventPayloads[keyof ClientEventPayloads] }




export function fakeTrack(kind = 'audio') { return new TestTrack(kind) }
export function streamOf(tracks: MediaStreamTrack[]) { return new TestStream(tracks) }
export function attachedStream(element: HTMLMediaElement): MediaStream {
  const stream = element.srcObject
  if (!(stream instanceof TestStream)) throw new Error('Expected synthetic playback stream')
  return stream
}
export function remoteStreamFor(id: string) { const stream = new TestStream(); stream.id = id; return stream }
export function fakeStream(kinds = ['audio']) { return streamOf(kinds.map(kind => fakeTrack(kind))) }

export function fakeSender(track: MediaStreamTrack | null) {
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
export const SFU_OFFER = [
  'v=0',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:1', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:2', 'a=recvonly',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:3', 'a=recvonly',
  ''
].join('\r\n')

export interface FakeTransceiver { mid: string; direction: RTCRtpTransceiverDirection; sender: ReturnType<typeof fakeSender>; receiver: RTCRtpReceiver }
export class FakePC {
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
  private ensureNotClosed() { return this.closed }
  addTrack(track: MediaStreamTrack) { this.ensureNotClosed(); const s = fakeSender(track); this.unmatched.push(s); return s }
  addTransceiver(trackOrKind: MediaStreamTrack | string) {
    this.ensureNotClosed()
    const s = fakeSender(typeof trackOrKind === 'string' ? null : trackOrKind)
    this.unmatched.push(s)
    return { sender: s }
  }
  // Senders on the negotiated lines, in m-line order (mic, screen, camera).
  get senders() { this.ensureNotClosed(); return this.transceivers.map(t => t.sender) }
  getSenders() { return this.senders }
  getTransceivers() { this.ensureNotClosed(); return this.transceivers }
  async setRemoteDescription(d: RTCSessionDescriptionInit) {
    this.ensureNotClosed()
    this.remoteDescription = d
    for (const m of String(d.sdp).matchAll(/a=mid:(\S+)/g)) {
      if (!this.transceivers.some(t => t.mid === m[1])) {
        this.transceivers.push({ mid: present(m[1]), direction: 'recvonly', sender: fakeSender(null), receiver: {} as RTCRtpReceiver })
      }
    }
  }
  async createAnswer(): Promise<RTCSessionDescriptionInit> { this.ensureNotClosed(); return { type: 'answer', sdp: 'a' } }
  async setLocalDescription() { this.ensureNotClosed() }
  async addIceCandidate(c: RTCIceCandidateInit) { this.ensureNotClosed(); this.candidates.push(c) }
  async getStats(): Promise<Map<string, import('../lib/mediaStats').MediaStat>> { this.ensureNotClosed(); return new Map() }
  close() { this.closed = true }
}

export interface FakeNode { connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }
export interface FakeDestination extends FakeNode { stream: TestStream }
export interface FakeSource extends FakeNode { stream: MediaStream }
export interface FakeGain extends FakeNode { gain: { value: number; cancelScheduledValues: ReturnType<typeof vi.fn>; setValueAtTime: ReturnType<typeof vi.fn> } }
export class FakeAudioContext {
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
  /** What the raw capture tap reads (float samples); 0 is digital silence. */
  static rawSample = 0.05
  static analysers: Array<{ fftSize: number; connect: () => void; getByteTimeDomainData: ReturnType<typeof vi.fn>; getFloatTimeDomainData: ReturnType<typeof vi.fn> }> = []
  constructor() {
    this.state = 'running'
    this.currentTime = 0
    this.destination = {}
    this.sinkId = ''
    this.closed = false
    FakeAudioContext.instances.push(this)
  }
  private ensureActive() { return this.state }
  setSinkId(id: string) { this.ensureActive(); this.sinkId = id; return Promise.resolve() }
  createMediaStreamDestination(): FakeDestination {
    this.ensureActive()
    const node = { stream: fakeStream(), connect: vi.fn(), disconnect: vi.fn() }
    FakeAudioContext.destinations.push(node)
    return node
  }
  createGain(): FakeGain {
    this.ensureActive()
    const node = {
      gain: {
        value: 1,
        cancelScheduledValues: vi.fn(),
        setValueAtTime: vi.fn(function (this: { value: number }, v: number) { this.value = v })
      },
      connect: vi.fn(),
      disconnect: vi.fn()
    }
    FakeAudioContext.gains.push(node)
    return node
  }
  createAnalyser() {
    this.ensureActive()
    const analyser = { fftSize: 256, connect() {}, getByteTimeDomainData: vi.fn((buffer: Uint8Array) => buffer.fill(FakeAudioContext.sample)), getFloatTimeDomainData: vi.fn((buffer: Float32Array) => buffer.fill(FakeAudioContext.rawSample)) }
    FakeAudioContext.analysers.push(analyser)
    return analyser
  }
  createMediaStreamSource(stream: MediaStream): FakeSource {
    this.ensureActive()
    const node = { stream, connect: vi.fn(), disconnect: vi.fn() }
    FakeAudioContext.sources.push(node)
    return node
  }
  createBiquadFilter() { this.ensureActive(); return { type: '', frequency: { setValueAtTime() {} }, connect() {} } }
  private listeners = new Set<() => void>()
  addEventListener(_type: string, fn: () => void) { this.listeners.add(fn) }
  removeEventListener(_type: string, fn: () => void) { this.listeners.delete(fn) }
  /** Changes the state the way the browser does, with a statechange event. */
  moveTo(state: AudioContextState) { this.state = state; [...this.listeners].forEach(fn => fn()) }
  resume() { this.state = 'running'; return Promise.resolve() }
  close() { this.state = 'closed'; this.closed = true; return Promise.resolve() }
}

export type MicRequest = (() => void) & { stream: TestStream }
export let micRequests: MicRequest[]
export let audioElements: HTMLAudioElement[]


export const permissiveMediaCall: MediaCall = {
  start() {},
  roster() {},
  receive() {},
  resume() {},
  stop() {},
  onKey() {},
  onWire() {},
  onNotice() {},
  onVerification() {},
  protectSender: () => true,
  prepareIncoming: () => true,
  protectIncoming: () => true,
}

export function installRecordingCall(allow = true) {
  let onKeyListener: (() => void) | null = null
  let onWireListener: ((channelId: string, wire: string) => void) | null = null
  let onNoticeListener: ((notice: MediaNotice) => void) | null = null
  let onVerificationListener: ((verification: MediaVerification | null) => void) | null = null
  const call = {
    start: vi.fn(),
    roster: vi.fn(),
    receive: vi.fn(),
    resume: vi.fn(),
    stop: vi.fn(),
    onKey: vi.fn((listener: () => void) => { onKeyListener = listener }),
    onWire: vi.fn((listener: (channelId: string, wire: string) => void) => { onWireListener = listener }),
    onNotice: vi.fn((listener: (notice: MediaNotice) => void) => { onNoticeListener = listener }),
    onVerification: vi.fn((listener: (verification: MediaVerification | null) => void) => { onVerificationListener = listener }),
    protectSender: vi.fn((_sender: RTCRtpSender, _source: number, _layer?: number) => allow),
    prepareIncoming: vi.fn((_receiver: RTCRtpReceiver) => allow),
    protectIncoming: vi.fn((_receiver: RTCRtpReceiver | undefined, _senderId: string, _source: number) => allow),
    fireKey() { onKeyListener?.() },
    fireWire(channelId: string, wire: string) { onWireListener?.(channelId, wire) },
    fireNotice(notice: MediaNotice = 'removed') { onNoticeListener?.(notice) },
    fireVerification(verification: MediaVerification | null) { onVerificationListener?.(verification) },
  }
  installMediaCall(call)
  return call
}



export function setup() {
  const chat = useChatStore()
  const sent: ClientSentEvent[] = []
  chat.sendWSEvent = (type, payload) => { sent.push({ type, payload }) }
  return { chat, voice: useVoiceStore(), rtc: useWebRTC(), sent }
}

// The SFU sends its offer once the join is in; the answer binds our senders.
export async function negotiate() {
  const pc = present(FakePC.instances.at(-1))
  useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(4))
  await new Promise(r => setTimeout(r, 0))
  return pc
}

export async function grantMic() {
  await vi.waitFor(() => expect(micRequests.length).toBeGreaterThan(0))
  const req = present(micRequests.shift())
  req()
  return req.stream
}



export async function joined() {
  const ctx = setup()
  const join = ctx.rtc.joinVoiceChannel('ch-1')
  const mic = await grantMic()
  await join
  const pc = await negotiate()
  const audio = present(pc.senders[0]), screen = present(pc.senders[1]), camera = present(pc.senders[2]), screenAudio = present(pc.senders[3])
  // The captured microphone is routed through the audio context: the mic line sends the stream the gate switches, not the capture itself.
  const sentMic = present(present(toRaw(ctx.voice.localAudioStream)).getAudioTracks()[0])
  return { ...ctx, mic, sentMic, pc, audio, screen, camera, screenAudio }
}

export function stubDisplayMedia(stream: MediaStream) {
  navigator.mediaDevices.getDisplayMedia = vi.fn(async () => stream)
}

// The SFU's offer from 0.8 on: the lines for the screen's two lower layers
// follow the four fixed ones.
export const SFU_LAYER_OFFER = SFU_OFFER + [
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:4', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:5', 'a=recvonly',
  ''
].join('\r\n')

// The offer of a server that also takes layers of the camera: the lines for the
// camera's two lower layers follow those of the screen's.
export const SFU_CAMERA_LAYER_OFFER = SFU_LAYER_OFFER + [
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:6', 'a=recvonly',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:7', 'a=recvonly',
  ''
].join('\r\n')

export async function joinedWithCameraLayers({ screen = 3, camera = 3 } = {}) {
  const ctx = setup()
  ctx.voice.serverScreenLayers = screen
  ctx.voice.serverCameraLayers = camera
  const join = ctx.rtc.joinVoiceChannel('ch-1')
  const mic = await grantMic()
  await join
  const pc = present(FakePC.instances.at(-1))
  ctx.chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_CAMERA_LAYER_OFFER } })
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(8))
  await new Promise(r => setTimeout(r, 0))
  const senders = pc.senders
  return { ...ctx, mic, pc, camera: present(senders[2]), cameraLow1: present(senders[6]), cameraLow2: present(senders[7]) }
}

/** A camera capture of this size, as getUserMedia returns it. */
export function cameraCapture(width: number, height: number) {
  const stream = fakeStream(['video'])
  const track = present(stream.getVideoTracks()[0])
  track.getSettings = () => ({ width, height, frameRate: 30 })
  return { stream, track }
}

export async function joinedWithLayers({ announce = 3 } = {}) {
  const ctx = setup()
  ctx.voice.serverScreenLayers = announce
  const join = ctx.rtc.joinVoiceChannel('ch-1')
  const mic = await grantMic()
  await join
  const pc = present(FakePC.instances.at(-1))
  ctx.chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_LAYER_OFFER } })
  await vi.waitFor(() => expect(pc.transceivers.length).toBe(6))
  await new Promise(r => setTimeout(r, 0))
  const senders = pc.senders
  return { ...ctx, mic, pc, screen: present(senders[1]), low1: present(senders[4]), low2: present(senders[5]) }
}

export function display(width: number, height: number) {
  const stream = fakeStream(['video'])
  const track = present(stream.getVideoTracks()[0])
  track.getSettings = () => ({ width, height, frameRate: 30 })
  track.applyConstraints = vi.fn(async () => {})
  return { stream, track }
}

// The fake sender keeps no parameters between calls, so what was set is the
// merge of everything set, latest last.
export const encodingOf = (sender: ReturnType<typeof fakeSender>) => Object.assign({}, ...sender.setParameters.mock.calls.map(c => present(present(c)[0]).encodings[0])) as RTCRtpEncodingParameters
export const declared = (sent: ClientSentEvent[]) => sent.filter(e => e.type === 'webrtc_layers').map(e => e.payload)
export const declaredLayersOf = (sent: ClientSentEvent[], n: number) => (declared(sent)[n] as { layers: { width: number; height: number; fps: number }[] }).layers

export function installHarness() {
  beforeEach(() => {
    audioElements = []
    localStorage.clear()
    // The default AI filter is unsupported here (see suppressor.supported); specs that need it say so.
    localStorage.setItem('mnema_noise', 'browser')
    resetNotices()
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
    FakeAudioContext.rawSample = 0.05
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
    installMediaCall(permissiveMediaCall)
    const { leaveVoiceChannel, stopMicTest } = useWebRTC()
    stopMicTest()
    leaveVoiceChannel()
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
}

