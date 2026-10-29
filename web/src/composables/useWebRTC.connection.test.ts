import { describe, it, expect, vi } from 'vitest'
import { present } from '../media-test.fixture'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { installHarness, joined } from './useWebRTC.harness.fixture'
import { relayOffered } from './webrtc/iceServers'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

const texts = () => useToastStore().toasts.map(toast => toast.text)

describe('a media connection that never comes up', () => {
  it('tells the member once that the network blocks the server', async () => {
    const { pc } = await joined()
    pc.iceConnectionState = 'checking'
    present(pc.oniceconnectionstatechange)()
    expect(texts()).toEqual([])
    pc.iceConnectionState = 'failed'
    present(pc.oniceconnectionstatechange)()
    present(pc.oniceconnectionstatechange)()
    expect(texts()).toEqual([t('voice.noMediaConnection')])
    expect(useToastStore().toasts[0]?.type).toBe('info')
  })

  it('does not repeat itself when the server reports the same loss', async () => {
    const { pc, chat, voice } = await joined()
    pc.iceConnectionState = 'failed'
    present(pc.oniceconnectionstatechange)()
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: present(voice.currentChannelId), reason: 'connection_lost' } })
    expect(voice.currentChannelId).toBeNull()
    expect(texts()).toEqual([t('voice.noMediaConnection')])
  })

  it('still reports a lost connection the browser did not notice', async () => {
    const { chat, voice } = await joined()
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: present(voice.currentChannelId), reason: 'connection_lost' } })
    expect(texts()).toEqual([t('voice.connectionLost')])
  })

  it('knows whether the server offers a relay', () => {
    expect(relayOffered()).toBe(false)
  })
})
