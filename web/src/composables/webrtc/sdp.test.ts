import { describe, it, expect, vi } from 'vitest'
import { present } from '../../media-test.fixture'
import { useChatStore } from '../../stores/chat'
import { publishMids, restrictVideoCodecs, tuneScreenLines, tuneScreenOffer, SCREEN_START_KBPS, SCREEN_MIN_KBPS } from './sdp'
import { installHarness, FakePC, SFU_LAYER_OFFER, grantMic, setup } from '../useWebRTC.harness.fixture'

vi.mock('../../lib/noiseSuppressor', async () => (await import('../useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

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
    const ours = { audio: '0', screenAudio: '3', video: ['1', '2'], cameraLayers: [] }
    expect(publishMids(offer)).toEqual(ours)
    // Someone was already talking: the SFU forwards their voice on our mic
    // line, which then reads sendrecv. It is still our microphone line.
    expect(publishMids(offer.replace('a=mid:0\r\na=recvonly', 'a=mid:0\r\na=sendrecv'))).toEqual(ours)
    expect(publishMids('')).toEqual({ audio: null, screenAudio: null, video: [], cameraLayers: [] })
  })
})

describe('restrictVideoCodecs', () => {
  const offer = [
    'v=0',
    'm=audio 9 UDP/TLS/RTP/SAVPF 111', 'a=mid:0', 'a=rtpmap:111 opus/48000/2',
    'm=video 9 UDP/TLS/RTP/SAVPF 96 97 98 99 100 101 102 45 46', 'a=mid:1',
    'a=rtcp-fb:* nack',
    'a=rtpmap:96 VP8/90000', 'a=rtcp-fb:96 nack', 'a=rtpmap:97 rtx/90000', 'a=fmtp:97 apt=96',
    'a=rtpmap:98 VP9/90000', 'a=fmtp:98 profile-id=0', 'a=rtpmap:99 rtx/90000', 'a=fmtp:99 apt=98',
    'a=rtpmap:100 H264/90000', 'a=fmtp:100 packetization-mode=1;profile-level-id=42e01f', 'a=rtpmap:101 rtx/90000', 'a=fmtp:101 apt=100',
    'a=rtpmap:102 H265/90000', 'a=rtpmap:45 AV1/90000', 'a=rtpmap:46 rtx/90000', 'a=fmtp:46 apt=45',
    ''
  ].join('\r\n')

  it('keeps VP8, H.264 and their retransmissions only', () => {
    const out = restrictVideoCodecs(offer)
    expect(out).toContain('m=video 9 UDP/TLS/RTP/SAVPF 96 97 100 101\r\n')
    for (const gone of ['VP9', 'H265', 'AV1', 'apt=98', 'apt=45', 'profile-id=0']) expect(out).not.toContain(gone)
    for (const kept of ['VP8/90000', 'H264/90000', 'apt=96', 'apt=100', 'a=rtcp-fb:96 nack', 'a=rtcp-fb:* nack', 'opus/48000/2', 'm=audio 9 UDP/TLS/RTP/SAVPF 111']) expect(out).toContain(kept)
  })

  it('leaves a line alone when no codec on it can be sealed', () => {
    const odd = ['v=0', 'm=video 9 UDP/TLS/RTP/SAVPF 98', 'a=rtpmap:98 VP9/90000', ''].join('\n')
    expect(restrictVideoCodecs(odd)).toBe(odd)
    expect(restrictVideoCodecs('')).toBe('')
  })

  it('is applied to the offer the browser receives', async () => {
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: offer } })
    await vi.waitFor(() => expect(pc.remoteDescription).not.toBeNull())
    expect(String(pc.remoteDescription?.sdp)).not.toContain('VP9')
    expect(String(pc.remoteDescription?.sdp)).toContain('VP8/90000')
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

describe('SDP capability edge cases', () => {
  it('ignores unknown media, unnumbered lines and forwarded lines beyond four publish slots', () => {
    expect(publishMids('v=0\nm=audio 9 UDP 1\nm=application 9 UDP 1\na=mid:data\nm=audio 9 UDP 1\na=mid:mic\nm=audio 9 UDP 1\na=mid:sound\nm=audio 9 UDP 1\na=mid:other\nm=video 9 UDP 96\na=mid:screen\nm=video 9 UDP 96\na=mid:camera\nm=video 9 UDP 96\na=mid:other-video')).toEqual({ audio: 'mic', screenAudio: 'sound', video: ['screen', 'camera'], cameraLayers: [] })
    expect(publishMids(null)).toEqual({ audio: null, screenAudio: null, video: [], cameraLayers: [] })
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

describe('screen lines of an SFU offer', () => {
  it('publishMids takes the lower layer lines only where the server has them', () => {
    const offer = SFU_LAYER_OFFER
    expect(publishMids(offer)).toEqual({ audio: '0', screenAudio: '3', video: ['1', '2'], cameraLayers: [] })
    expect(publishMids(offer, 2)).toEqual({ audio: '0', screenAudio: '3', video: ['1', '2', '4', '5'], cameraLayers: [] })
    expect(publishMids(offer, 1).video).toEqual(['1', '2', '4'])
  })

  it('publishMids puts the camera layer lines after the screen\'s, only where the server has them', () => {
    const lines = ['1', '2', '4', '5', '6', '7'].flatMap(mid => ['m=video 9 UDP 96', `a=mid:${mid}`, 'a=recvonly'])
    const offer = ['v=0', 'm=audio 9 UDP 111', 'a=mid:0', 'a=recvonly', 'm=audio 9 UDP 111', 'a=mid:3', 'a=recvonly', ...lines, ''].join('\r\n')
    expect(publishMids(offer, 2, 2)).toEqual({ audio: '0', screenAudio: '3', video: ['1', '2', '4', '5'], cameraLayers: ['6', '7'] })
    expect(publishMids(offer, 2, 1).cameraLayers).toEqual(['6'])
    // An older server without camera lines: the lines are not ours to use.
    expect(publishMids(offer, 2, 0).cameraLayers).toEqual([])
    // A server with camera lines but no screen layers.
    expect(publishMids(offer, 0, 2)).toMatchObject({ video: ['1', '2'], cameraLayers: ['4', '5'] })
  })

  it('tuneScreenLines gives every layer line the screen line tuning', () => {
    const line = (mid: string) => ['m=video 9 UDP/TLS/RTP/SAVPF 96 102', `a=mid:${mid}`, 'a=rtpmap:96 VP8/90000', 'a=rtpmap:102 H264/90000', 'a=fmtp:102 packetization-mode=1;profile-level-id=42e01f']
    const sdp = ['v=0', ...line('1'), ...line('2'), ...line('4'), ...line('5'), ''].join('\r\n')
    const out = tuneScreenLines(sdp, { video: ['1', '2', '4', '5'] }, true)
    const sections = out.split('m=video').slice(1)
    // Screen and both layer lines now start with H.264; the camera line does not.
    expect(sections.map(s => s.split('\r\n')[0])).toEqual([' 9 UDP/TLS/RTP/SAVPF 102 96', ' 9 UDP/TLS/RTP/SAVPF 96 102', ' 9 UDP/TLS/RTP/SAVPF 102 96', ' 9 UDP/TLS/RTP/SAVPF 102 96'])
    expect(out).toContain(`x-google-start-bitrate=${SCREEN_START_KBPS}`)
    expect(tuneScreenLines(sdp, { video: ['1'] }, false).split('m=video')[1]).toContain('x-google-start-bitrate')
  })
})
