import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWebRTC } from './useWebRTC'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { streamEncoding } from '../lib/streamQuality'

vi.mock('../lib/noiseSuppressor', () => ({
  isNoiseSuppressionSupported: () => false,
  preloadNoiseSuppressor: () => {},
  createNoiseSuppressorNode: vi.fn()
}))

const offer = ['v=0',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:1',
  'm=video 9 UDP/TLS/RTP/SAVPF 96', 'a=mid:2',
  'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:3', ''].join('\r\n')

class Peer {
  static last
  constructor() { Peer.last = this; this.transceivers = [] }
  getTransceivers() { return this.transceivers }
  async setRemoteDescription(description) {
    this.remoteDescription = description
    this.transceivers = ['0', '1', '2', '3'].map(mid => {
      const sender = {
        track: null,
        getParameters: () => ({ encodings: [{}] }),
        setParameters: vi.fn(async () => {}),
        replaceTrack: vi.fn(async track => { sender.track = track })
      }
      return { mid, direction: 'recvonly', sender }
    })
  }
  async createAnswer() { return { type: 'answer', sdp: 'answer' } }
  async setLocalDescription() {}
  async addIceCandidate() {}
  close() {}
}

function display() {
  const track = {
    kind: 'video', readyState: 'live', contentHint: '',
    getSettings: () => ({ width: 3840, height: 2160, frameRate: 60 }),
    stop: vi.fn()
  }
  return { getTracks: () => [track], getVideoTracks: () => [track], getAudioTracks: () => [] }
}

let rtc, voice, chat
beforeEach(async () => {
  setActivePinia(createPinia())
  vi.stubGlobal('RTCPeerConnection', Peer)
  vi.stubGlobal('RTCSessionDescription', function (description) { return description })
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ ice_servers: [] }) })))
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      enumerateDevices: vi.fn(async () => []),
      getUserMedia: vi.fn(async () => { throw new Error('microphone not granted in screen-only test') }),
      getDisplayMedia: vi.fn(async () => display())
    }
  })
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  chat = useChatStore()
  chat.sendWSEvent = vi.fn()
  voice = useVoiceStore()
  rtc = useWebRTC()
  await rtc.joinVoiceChannel('test-channel')
  chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: offer } })
  await vi.waitFor(() => expect(chat.sendWSEvent).toHaveBeenCalledWith('webrtc_answer', expect.anything()))
  await vi.waitFor(() => expect(Peer.last.transceivers[1].sender.setParameters).toHaveBeenCalled())
  Peer.last.transceivers[1].sender.setParameters.mockClear()
})

afterEach(() => {
  rtc.leaveVoiceChannel()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('screen capture startup', () => {
  it('configures the chosen quality before attaching a 4K capture to the dormant encoder', async () => {
    const sender = Peer.last.transceivers[1].sender
    const events = []
    sender.setParameters.mockImplementation(async () => { events.push('limits'); expect(sender.track).toBeNull() })
    sender.replaceTrack.mockImplementation(async track => { events.push('capture'); sender.track = track })
    const quality = { resolution: 720, fps: 15 }
    await rtc.startScreenShare(quality)

    expect(events).toEqual(['limits', 'capture'])
    expect(sender.setParameters.mock.calls[0][0].encodings[0]).toMatchObject(streamEncoding(quality, { width: 3840, height: 2160 }))
    expect(voice.screenQuality).toEqual(quality)
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledWith(expect.objectContaining({
      video: { frameRate: { ideal: 15, max: 15 }, height: { max: 720 } }
    }))
  })

  it('opens one picker and starts one capture for repeated start clicks', async () => {
    let finish
    const stream = display()
    navigator.mediaDevices.getDisplayMedia.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const first = rtc.startScreenShare()
    const second = rtc.startScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(1)
    finish(stream)
    await Promise.all([first, second])
    expect(chat.sendWSEvent.mock.calls.filter(([type]) => type === 'webrtc_screenshare_start')).toHaveLength(1)
    expect(stream.getVideoTracks()[0].stop).not.toHaveBeenCalled()
  })

  it('discards a picker result after stopping before capture permission resolves', async () => {
    let finish
    const stream = display()
    navigator.mediaDevices.getDisplayMedia.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const start = rtc.startScreenShare()
    rtc.stopScreenShare()
    finish(stream)
    await start
    expect(stream.getVideoTracks()[0].stop).toHaveBeenCalled()
    expect(voice.isScreenSharing).toBe(false)
    expect(chat.sendWSEvent).not.toHaveBeenCalledWith('webrtc_screenshare_start', {})
  })

  it('opens one replacement picker and preserves the chosen quality and current share until permission resolves', async () => {
    const quality = { resolution: 'source', fps: 60 }
    await rtc.startScreenShare(quality)
    const current = Peer.last.transceivers[1].sender.track
    const replacement = display()
    let finish
    navigator.mediaDevices.getDisplayMedia.mockClear()
    navigator.mediaDevices.getDisplayMedia.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const first = rtc.replaceScreenShare()
    const second = rtc.replaceScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(1)
    expect(current.stop).not.toHaveBeenCalled()
    finish(replacement)
    await Promise.all([first, second])
    expect(current.stop).toHaveBeenCalledTimes(1)
    expect(voice.screenQuality).toEqual(quality)
    expect(Peer.last.transceivers[1].sender.track).toBe(replacement.getVideoTracks()[0])
  })

  it('allows another picker after permission was cancelled', async () => {
    navigator.mediaDevices.getDisplayMedia.mockRejectedValueOnce(new DOMException('cancelled', 'NotAllowedError'))
    await rtc.startScreenShare()
    expect(voice.isScreenSharing).toBe(false)
    await rtc.startScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(2)
    expect(voice.isScreenSharing).toBe(true)
  })

  it('does not resurrect a share stopped while its sender is being configured', async () => {
    const sender = Peer.last.transceivers[1].sender
    let finish
    sender.setParameters.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const start = rtc.startScreenShare()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.stopScreenShare()
    finish()
    await start
    expect(sender.replaceTrack.mock.calls.some(([track]) => track !== null)).toBe(false)
    expect(chat.sendWSEvent).not.toHaveBeenCalledWith('webrtc_screenshare_start', {})
  })
})
