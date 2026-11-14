import { describe, it, expect, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { present } from '../../media-test.fixture'
import { FakePC, grantMic, installHarness, setup } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

// A voice_join sent while the realtime socket is still connecting would be
// dropped: the join waits for the socket (webrtc/session.ts).
describe('a Talk join before the realtime socket is up', () => {
  it('holds the join until the socket is up', async () => {
    const { rtc, chat, sent } = setup()
    chat.isConnected = false
    const join = rtc.joinVoiceChannel('early')
    await grantMic()
    await flushPromises()
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
    chat.isConnected = true
    await join
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'early', tab: expect.any(String) } })
  })

  it('sends no resume beside a held join when the socket comes back', async () => {
    const { rtc, chat, sent } = setup()
    chat.isConnected = false
    const join = rtc.joinVoiceChannel('back')
    await grantMic()
    await flushPromises()
    rtc.rejoinAfterReconnect()
    chat.isConnected = true
    await join
    expect(sent.filter(e => e.type === 'voice_join')).toEqual([{ type: 'voice_join', payload: { channel_id: 'back', tab: expect.any(String) } }])
  })

  it('drops a held join when the member left meanwhile', async () => {
    const { rtc, chat, sent } = setup()
    chat.isConnected = false
    const join = rtc.joinVoiceChannel('gone')
    await grantMic()
    await flushPromises()
    rtc.leaveVoiceChannel()
    chat.isConnected = true
    await join
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
  })

  it('releases the microphone of a held join the member left, also when it was sent unrouted', async () => {
    // Without an AudioContext the capture itself is the stream that would be sent.
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', undefined)
    const { rtc, chat } = setup()
    chat.isConnected = false
    const join = rtc.joinVoiceChannel('gone')
    const capture = await grantMic()
    await flushPromises()
    rtc.leaveVoiceChannel()
    chat.isConnected = true
    await join
    expect(present(capture.getTracks()[0]).stop).toHaveBeenCalled()
  })

  it('lets a later reconnect resume the call when setting up the held join threw', async () => {
    const { rtc, chat, voice, sent } = setup()
    chat.isConnected = false
    const join = rtc.joinVoiceChannel('broken')
    await grantMic()
    await flushPromises()
    vi.stubGlobal('RTCPeerConnection', class { constructor() { throw new DOMException('bad ICE server', 'SyntaxError') } })
    chat.isConnected = true
    await expect(join).rejects.toThrow('bad ICE server')
    vi.stubGlobal('RTCPeerConnection', FakePC)
    expect(voice.currentChannelId).toBe('broken')
    rtc.rejoinAfterReconnect()
    expect(sent).toContainEqual({ type: 'voice_join', payload: { channel_id: 'broken', resume: true, tab: expect.any(String) } })
  })
})
