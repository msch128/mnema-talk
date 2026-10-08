import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWebRTC } from './useWebRTC'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { TestTrack, TestStream, present } from '../media-test.fixture'
import type { ScreenQuality } from '../lib/streamQuality'
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

function fakeSender() {
  const sender = {
    track: null as MediaStreamTrack | null,
    getParameters: () => ({ encodings: [{}] }),
    setParameters: vi.fn(async (_parameters: RTCRtpSendParameters) => {}),
    replaceTrack: vi.fn(async (track: MediaStreamTrack | null) => { sender.track = track })
  }
  return sender
}
class Peer {
  static last: Peer | undefined
  transceivers: Array<{ mid: string; direction: RTCRtpTransceiverDirection; sender: ReturnType<typeof fakeSender> }> = []
  remoteDescription: RTCSessionDescriptionInit | null = null
  constructor() { Peer.last = this }
  getTransceivers() { return this.transceivers }
  async setRemoteDescription(description: RTCSessionDescriptionInit) {
    this.remoteDescription = description
    this.transceivers = ['0', '1', '2', '3'].map(mid => ({ mid, direction: 'recvonly', sender: fakeSender() }))
  }
  async createAnswer() { return { type: 'answer', sdp: 'answer' } }
  async setLocalDescription() {}
  async addIceCandidate() {}
  close() {}
}
function display() {
  const track = new TestTrack('video')
  track.getSettings = () => ({ width: 3840, height: 2160, frameRate: 60 })
  return new TestStream([track])
}
let rtc: ReturnType<typeof useWebRTC>, voice: ReturnType<typeof useVoiceStore>, chat: ReturnType<typeof useChatStore>
function screenSender() { return present(present(Peer.last).transceivers[1]).sender }
beforeEach(async () => {
  setActivePinia(createPinia())
  vi.stubGlobal('RTCPeerConnection', Peer)
  vi.stubGlobal('RTCSessionDescription', function (description: RTCSessionDescriptionInit) { return description })
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
  await vi.waitFor(() => expect(screenSender().setParameters).toHaveBeenCalled())
  screenSender().setParameters.mockClear()
})

afterEach(() => {
  rtc.leaveVoiceChannel()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('screen capture startup', () => {
  it('configures the chosen quality before attaching a 4K capture to the dormant encoder', async () => {
    const sender = screenSender()
    const events: string[] = []
    sender.setParameters.mockImplementation(async () => { events.push('limits'); expect(sender.track).toBeNull() })
    sender.replaceTrack.mockImplementation(async track => { events.push('capture'); sender.track = track })
    const quality: ScreenQuality = { resolution: 720, fps: 15 }
    await rtc.startScreenShare(quality)

    expect(events).toEqual(['limits', 'capture'])
    expect(present(present(sender.setParameters.mock.calls[0])[0].encodings)[0]).toMatchObject(streamEncoding(quality, { width: 3840, height: 2160 }))
    expect(voice.screenQuality).toEqual(quality)
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledWith(expect.objectContaining({
      video: { frameRate: { ideal: 15, max: 15 }, height: { max: 720 } }
    }))
  })

  it('opens one picker and starts one capture for repeated start clicks', async () => {
    let finish: ((stream: MediaStream) => void) | undefined
    const stream = display()
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockImplementation(() => new Promise<MediaStream>(resolve => { finish = resolve }))
    const first = rtc.startScreenShare()
    const second = rtc.startScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(1)
    present(finish)(stream)
    await Promise.all([first, second])
    expect(vi.mocked(chat.sendWSEvent).mock.calls.filter(([type]) => type === 'webrtc_screenshare_start')).toHaveLength(1)
    expect(present(stream.getVideoTracks()[0]).stop).not.toHaveBeenCalled()
  })

  it('discards a picker result after stopping before capture permission resolves', async () => {
    let finish: ((stream: MediaStream) => void) | undefined
    const stream = display()
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockImplementation(() => new Promise<MediaStream>(resolve => { finish = resolve }))
    const start = rtc.startScreenShare()
    rtc.stopScreenShare()
    present(finish)(stream)
    await start
    expect(present(stream.getVideoTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.isScreenSharing).toBe(false)
    expect(chat.sendWSEvent).not.toHaveBeenCalledWith('webrtc_screenshare_start', {})
  })

  it('opens one replacement picker and preserves the chosen quality and current share until permission resolves', async () => {
    const quality: ScreenQuality = { resolution: 'source', fps: 60 }
    await rtc.startScreenShare(quality)
    const current = present(screenSender().track)
    const replacement = display()
    let finish: ((stream: MediaStream) => void) | undefined
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockClear()
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockImplementation(() => new Promise<MediaStream>(resolve => { finish = resolve }))
    const first = rtc.replaceScreenShare()
    const second = rtc.replaceScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(1)
    expect(current.stop).not.toHaveBeenCalled()
    present(finish)(replacement)
    await Promise.all([first, second])
    expect(current.stop).toHaveBeenCalledTimes(1)
    expect(voice.screenQuality).toEqual(quality)
    expect(screenSender().track).toBe(replacement.getVideoTracks()[0])
  })

  it('allows another picker after permission was cancelled', async () => {
    vi.mocked(navigator.mediaDevices.getDisplayMedia).mockRejectedValueOnce(new DOMException('cancelled', 'NotAllowedError'))
    await rtc.startScreenShare()
    expect(voice.isScreenSharing).toBe(false)
    await rtc.startScreenShare()
    expect(navigator.mediaDevices.getDisplayMedia).toHaveBeenCalledTimes(2)
    expect(voice.isScreenSharing).toBe(true)
  })

  it('does not resurrect a share stopped while its sender is being configured', async () => {
    const sender = screenSender()
    let finish: (() => void) | undefined
    sender.setParameters.mockImplementation(() => new Promise<void>(resolve => { finish = resolve }))
    const start = rtc.startScreenShare()
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.stopScreenShare()
    present(finish)()
    await start
    expect(sender.replaceTrack.mock.calls.some(([track]) => track !== null)).toBe(false)
    expect(chat.sendWSEvent).not.toHaveBeenCalledWith('webrtc_screenshare_start', {})
  })
})
