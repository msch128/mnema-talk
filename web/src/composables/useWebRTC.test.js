import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { toRaw } from 'vue'
import { useWebRTC } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'

// --- Browser API stand-ins -------------------------------------------------

function fakeTrack(kind = 'audio') {
  return { kind, enabled: true, stop: vi.fn(), clone() { return fakeTrack(kind) } }
}

function fakeStream() {
  const tracks = [fakeTrack()]
  return { getTracks: () => tracks, getAudioTracks: () => tracks, getVideoTracks: () => [] }
}

class FakePC {
  static instances = []
  constructor() {
    this.senders = []
    this.remoteDescription = null
    this.candidates = []
    this.closed = false
    FakePC.instances.push(this)
  }
  addTrack(track) { const s = { track, replaceTrack: vi.fn(async t => { s.track = t }) }; this.senders.push(s); return s }
  getSenders() { return this.senders }
  async setRemoteDescription(d) { this.remoteDescription = d }
  async createAnswer() { return { type: 'answer', sdp: 'a' } }
  async setLocalDescription() {}
  async addIceCandidate(c) { this.candidates.push(c) }
  async getStats() { return new Map() }
  close() { this.closed = true }
}

class FakeAudioContext {
  constructor() { this.state = 'running'; this.currentTime = 0 }
  createAnalyser() { return { fftSize: 256, connect() {}, getByteTimeDomainData() {} } }
  createMediaStreamSource() { return { connect() {} } }
  createBiquadFilter() { return { type: '', frequency: { setValueAtTime() {} }, connect() {} } }
  resume() { return Promise.resolve() }
  close() { return Promise.resolve() }
}

let micRequests
beforeEach(() => {
  setActivePinia(createPinia())
  FakePC.instances = []
  micRequests = []
  vi.stubGlobal('RTCPeerConnection', FakePC)
  vi.stubGlobal('RTCSessionDescription', function (d) { return d })
  vi.stubGlobal('RTCIceCandidate', function (c) { return c })
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal('MediaStream', function (tracks) { return { getTracks: () => tracks, getAudioTracks: () => tracks } })
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
    expect(FakePC.instances.at(-1).senders).toHaveLength(1)
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
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'o' } })
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
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'o' } })
    await vi.waitFor(() => expect(sent.some(e => e.type === 'webrtc_answer')).toBe(true))
    expect(pc.candidates).toEqual([{ candidate: 'c1' }])
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
    const sender = FakePC.instances.at(-1).senders[0]

    const apply = rtc.applyAudioSettings()
    const newStream = await grantMic()
    await apply
    expect(sender.replaceTrack).toHaveBeenCalledWith(newStream.getAudioTracks()[0])
    expect(oldStream.getTracks()[0].stop).toHaveBeenCalled()
    expect(toRaw(rtc.localAudioStream.value)).toBe(newStream)
  })
})
