import { describe, it, expect, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { channelFixture } from '../../test-fixtures.fixture'
import { grantMic, installHarness, joined, setup } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

const of = (sent: { type: string, payload?: unknown }[], type: string) => sent.filter(e => e.type === type)

// What a dropped realtime socket costs a call (webrtc/socketGap.ts).
describe('a lobby join before the realtime socket is up', () => {
  it('asks for the Talk once the socket is up, not into the void', async () => {
    const { rtc, chat, sent } = setup()
    chat.uncategorized = [channelFixture({ id: 'lobby', is_lobby: true, type: 'voice' })]
    chat.isConnected = false
    void rtc.joinVoiceChannel('lobby')
    await flushPromises()
    expect(of(sent, 'voice_join')).toEqual([])
    chat.isConnected = true
    await flushPromises()
    expect(of(sent, 'voice_join')).toEqual([{ type: 'voice_join', payload: { channel_id: 'lobby' } }])
  })

  it('asks for nothing when the member gave up meanwhile', async () => {
    const { rtc, chat, sent } = setup()
    chat.uncategorized = [channelFixture({ id: 'lobby', is_lobby: true, type: 'voice' })]
    chat.isConnected = false
    void rtc.joinVoiceChannel('lobby')
    await flushPromises()
    rtc.leaveVoiceChannel()
    chat.isConnected = true
    await flushPromises()
    expect(of(sent, 'voice_join')).toEqual([])
  })
})

describe('a reconnect during the microphone prompt', () => {
  it('sends one join, no resume for a join that was never sent', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    // The socket drops and comes back while the browser still asks for the microphone.
    chat.isConnected = false
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    await grantMic()
    await join
    expect(of(sent, 'voice_join')).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', tab: expect.any(String) } }])
  })

  it('resumes the call after its join went out', async () => {
    const { rtc, sent } = await joined()
    rtc.rejoinAfterReconnect()
    expect(of(sent, 'voice_join').at(-1)).toEqual({ type: 'voice_join', payload: { channel_id: 'ch-1', resume: true, tab: expect.any(String) } })
  })
})

describe('leaving while the socket is down', () => {
  it('sends no made-up join or leave after the reconnect', async () => {
    const { rtc, chat, sent } = await joined()
    chat.isConnected = false
    rtc.leaveVoiceChannel()
    const before = sent.length
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    expect(sent.slice(before)).toEqual([])
  })
})
