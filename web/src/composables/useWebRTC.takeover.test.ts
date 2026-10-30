import { describe, it, expect, vi, afterEach } from 'vitest'
import { present } from '../media-test.fixture'
import { userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { pendingConfirm } from '../lib/confirm'
import * as voiceSession from '../lib/voiceSession'
import { t } from '../i18n'
import { tabId } from './webrtc/session'
import { installHarness, grantMic, setup } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()
afterEach(() => { pendingConfirm.value = null })

// The account is already listed in 'other' (a session on another device).
function setupElsewhere() {
  const ctx = setup()
  const me = userFixture()
  useAuthStore().user = me
  ctx.voice.channelUsers = { other: { [me.id]: voiceUserFixture({ id: me.id }) } }
  return ctx
}

const voiceJoins = (sent: { type: string, payload?: unknown }[]) => sent.filter(e => e.type === 'voice_join')

describe('taking a call over from another device', () => {
  it('asks first, and joins nothing while the question is open', async () => {
    const { rtc, sent } = setupElsewhere()
    void rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    const dialog = present(pendingConfirm.value)
    expect(dialog.title).toBe(t('voice.takeoverTitle'))
    expect(dialog.confirmLabel).toBe(t('voice.takeoverConfirm'))
    expect(voiceJoins(sent)).toHaveLength(0)
  })

  it('joins with takeover once confirmed', async () => {
    const { rtc, sent, voice } = setupElsewhere()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    present(pendingConfirm.value).resolve(true)
    await grantMic()
    await join
    expect(voiceJoins(sent)).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', takeover: true, tab: expect.any(String) } }])
    expect(voice.currentChannelId).toBe('ch-1')
  })

  it('does nothing when the user keeps the call where it is', async () => {
    const { rtc, sent, voice } = setupElsewhere()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    present(pendingConfirm.value).resolve(false)
    await join
    expect(voiceJoins(sent)).toHaveLength(0)
    expect(voice.currentChannelId).toBeNull()
  })

  it('does not ask when the account is in no Talk, or only in this session\'s own', async () => {
    const { rtc, sent, voice } = setup()
    const me = userFixture()
    useAuthStore().user = me
    voice.channelUsers = { 'ch-1': { [me.id]: voiceUserFixture({ id: me.id }) } }
    voice.currentChannelId = 'ch-1'
    const join = rtc.joinVoiceChannel('ch-2')
    expect(pendingConfirm.value).toBeNull()
    await grantMic()
    await join
    expect(voiceJoins(sent)).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-2', tab: expect.any(String) } }])
  })

  it('a rejoin after a reload never asks', async () => {
    const { rtc, sent, chat } = setupElsewhere()
    vi.spyOn(chat, 'isVoiceChannel').mockImplementation(id => id === 'ch-1')
    voiceSession.save('ch-1')
    const resumed = rtc.resumeVoiceSession()
    expect(pendingConfirm.value).toBeNull()
    await grantMic()
    await resumed
    expect(voiceJoins(sent)).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', tab: expect.any(String) } }])
  })

  it('a rejoin after a WebSocket reconnect says it resumes', async () => {
    const { rtc, sent, voice } = setup()
    voice.currentChannelId = 'ch-1'
    rtc.rejoinAfterReconnect()
    expect(voiceJoins(sent)).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', resume: true, tab: expect.any(String) } }])
  })
})

describe('leaving here', () => {
  it('does not take its own just-ended seat for another device', async () => {
    const { rtc, voice } = setup()
    const me = userFixture()
    useAuthStore().user = me
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    voice.channelUsers = { 'ch-1': { [me.id]: voiceUserFixture({ id: me.id }) } }
    rtc.leaveVoiceChannel()
    expect(voice.channelUsers['ch-1']).toEqual({})
    const next = rtc.joinVoiceChannel('ch-2')
    expect(pendingConfirm.value).toBeNull()
    await grantMic()
    await next
  })
})

describe('the session whose call was taken over', () => {
  it('ends its call quietly, offers to come back, and does not reconnect', async () => {
    const { rtc, sent, voice, chat } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    sent.length = 0

    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1', reason: 'voice_taken_over' } })

    expect(voice.currentChannelId).toBeNull()
    expect(sent.some(e => e.type === 'voice_leave')).toBe(false)
    expect(voiceSession.recent()).toBeNull()
    const toast = present(useToastStore().toasts.at(-1))
    expect(toast.text).toBe(t('voice.takenOver'))
    expect(toast.type).toBe('info')
    expect(toast.action?.label).toBe(t('voice.takenOverBack'))
  })

  it('"Back here" takes the call over again without asking', async () => {
    const { rtc, sent, chat } = setupElsewhere()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    present(pendingConfirm.value).resolve(true)
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1', reason: 'voice_taken_over' } })
    sent.length = 0

    void present(present(useToastStore().toasts.at(-1)).action).onClick()
    expect(pendingConfirm.value).toBeNull()
    await grantMic()
    await vi.waitFor(() => expect(voiceJoins(sent)).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', takeover: true, tab: expect.any(String) } }]))
  })

  it('ignores a takeover notice for a Talk it is not in', async () => {
    const { rtc, voice, chat } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-9', reason: 'voice_taken_over' } })
    expect(voice.currentChannelId).toBe('ch-1')
  })
})

describe('the page id', () => {
  it('is stable, and exists where crypto.randomUUID does not (plain http)', () => {
    vi.stubGlobal('crypto', {})
    const id = tabId()
    expect(id).not.toBe('')
    expect(tabId()).toBe(id)
    vi.unstubAllGlobals()
  })
})
