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

describe('leaving while connected', () => {
  it('sends voice_leave with channel_id and tab when join was sent', async () => {
    const { rtc, sent } = await joined()
    const before = sent.length
    rtc.leaveVoiceChannel()
    expect(sent.slice(before)).toEqual([{ type: 'voice_leave', payload: { channel_id: 'ch-1', tab: expect.any(String) } }])
  })

  it('sends empty voice_leave when no join was sent', async () => {
    const { rtc, sent } = setup()
    void rtc.joinVoiceChannel('ch-1')
    await flushPromises()
    const before = sent.length
    rtc.leaveVoiceChannel()
    expect(sent.slice(before)).toEqual([{ type: 'voice_leave', payload: {} }])
  })
})

describe('leaving while the socket is down', () => {
  it('sends one voice_leave with channel_id and tab after reconnect and no voice_join', async () => {
    const { rtc, chat, sent } = await joined()
    chat.isConnected = false
    rtc.leaveVoiceChannel()
    const before = sent.length
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    const after = sent.slice(before)
    expect(after).toEqual([{ type: 'voice_leave', payload: { channel_id: 'ch-1', tab: expect.any(String) } }])
    expect(after.some(e => e.type === 'voice_join')).toBe(false)
  })

  it('owes nothing when a join was never sent', async () => {
    const { rtc, chat, sent } = setup()
    chat.isConnected = false
    void rtc.joinVoiceChannel('ch-1')
    await flushPromises()
    rtc.leaveVoiceChannel()
    const before = sent.length
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    expect(sent.slice(before)).toEqual([])
  })

  it('clears the owed leave when a later join occurs', async () => {
    const { rtc, chat, sent } = await joined()
    chat.isConnected = false
    rtc.leaveVoiceChannel()
    chat.isConnected = true
    const join2 = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join2
    rtc.leaveVoiceChannel()
    const before = sent.length
    chat.isConnected = false
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    expect(sent.slice(before)).toEqual([])
  })

  it('sends nothing more on a second reconnect', async () => {
    const { rtc, chat, sent } = await joined()
    chat.isConnected = false
    rtc.leaveVoiceChannel()
    chat.isConnected = true
    rtc.rejoinAfterReconnect()
    const before = sent.length
    rtc.rejoinAfterReconnect()
    expect(sent.slice(before)).toEqual([])
  })
})
