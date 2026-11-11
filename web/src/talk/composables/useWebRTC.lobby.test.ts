import { describe, it, expect, vi } from 'vitest'
import { installHarness, grantMic, setup } from './useWebRTC.harness.fixture'
import * as router from '../../shared/lib/router'
import { channelFixture } from '../../test-fixtures.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

const voiceJoins = (sent: { type: string, payload?: unknown }[]) => sent.filter(e => e.type === 'voice_join')

function lobbyWorld() {
  const ctx = setup()
  ctx.chat.uncategorized = [channelFixture({ id: 'lobby', is_lobby: true, type: 'voice' }), channelFixture({ id: 'ch-1', type: 'voice' }), channelFixture({ id: 'new-talk', type: 'voice' })]
  vi.spyOn(ctx.chat, 'fetchChannels').mockResolvedValue(undefined)
  return ctx
}

describe('a Create-a-Talk lobby join that is still waiting', () => {
  it('does not pull the member into the new Talk after they joined another one', async () => {
    const { rtc, chat, sent } = lobbyWorld()
    const navigate = vi.spyOn(router, 'navigate').mockImplementation(() => {})
    void rtc.joinVoiceChannel('lobby')
    await vi.waitFor(() => expect(voiceJoins(sent)).toHaveLength(1))
    const other = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await other
    chat.handleWSEvent({ type: 'talk_moved', payload: { from_channel_id: 'lobby', channel_id: 'new-talk' } })
    await new Promise(r => setTimeout(r, 0))
    expect(voiceJoins(sent).map(e => (e.payload as { channel_id: string }).channel_id)).toEqual(['lobby', 'ch-1'])
    expect(navigate).not.toHaveBeenCalled()
  })

  it('does not join the new Talk after the member left', async () => {
    const { rtc, chat, sent } = lobbyWorld()
    const navigate = vi.spyOn(router, 'navigate').mockImplementation(() => {})
    void rtc.joinVoiceChannel('lobby')
    await vi.waitFor(() => expect(voiceJoins(sent)).toHaveLength(1))
    rtc.leaveVoiceChannel()
    chat.handleWSEvent({ type: 'talk_moved', payload: { from_channel_id: 'lobby', channel_id: 'new-talk' } })
    await new Promise(r => setTimeout(r, 0))
    expect(voiceJoins(sent)).toHaveLength(1)
    expect(navigate).not.toHaveBeenCalled()
  })
})
