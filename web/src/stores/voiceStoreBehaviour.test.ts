import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { useVoiceStore } from './voice'
import * as auth from './auth'
import { userFixture, voiceUserFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import { streamFixture } from '../store-test-support.fixture'
import { TestTrack } from '../media-test.fixture'
import { SCREEN_QUALITY_STORAGE_KEY } from '../lib/streamQuality'
import { playSoundEffect } from '../lib/soundEffects'

vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  vi.clearAllMocks()
})
afterEach(() => vi.restoreAllMocks())

describe('voice settings restored and persisted across sessions', () => {
  it.each(['ai', 'ai-lite', 'browser', 'off', 'false', 'true', 'corrupt'])('restores legacy and current noise mode %s', saved => {
    localStorage.setItem('mnema_noise', saved)
    localStorage.setItem('mnema_input_mode', 'ptt')
    const voice = useVoiceStore()
    const expected = saved === 'false' ? 'off' : saved === 'true' || saved === 'corrupt' ? 'ai' : saved
    expect(voice.noiseMode).toBe(expected)
    expect(voice.noiseCancelling).toBe(expected !== 'off')
    expect(voice.inputMode).toBe('ptt')
  })

  it('saves gate, hardware, output and sound-event controls when changed', async () => {
    const voice = useVoiceStore()
    voice.inputMode = 'ptt'
    voice.pttKey = 'KeyM'
    voice.autoSensitivity = true
    voice.sensitivityThreshold = 42
    voice.hangoverMs = 500
    voice.warnNoAudioDetected = false
    voice.warnSwitchChannel = false
    voice.qosHighPriority = false
    voice.soundEffectsEnabled = false
    voice.soundEffectsVolume = 25
    voice.autoGainControl = true
    voice.echoCancellation = false
    voice.selectedInputDeviceId = 'synthetic-mic'
    voice.inputVolume = 75
    voice.outputVolume = 60
    voice.selectedOutputDeviceId = 'synthetic-speaker'
    await nextTick()
    expect(localStorage.getItem('mnema_output_dev')).toBe('synthetic-speaker')
    voice.toggleSoundEvent('join')
    expect(voice.soundEvents.join).toBe(false)
    voice.toggleSoundEvent('join')
    expect(voice.soundEvents.join).toBe(true)
    setActivePinia(createPinia())
    const restored = useVoiceStore()
    expect(restored).toMatchObject({ inputMode: 'ptt', pttKey: 'KeyM', autoSensitivity: true, sensitivityThreshold: 42, hangoverMs: 500,
      warnNoAudioDetected: false, warnSwitchChannel: false, qosHighPriority: false, soundEffectsEnabled: false,
      soundEffectsVolume: 25, autoGainControl: true, echoCancellation: false, selectedInputDeviceId: 'synthetic-mic',
      inputVolume: 75, outputVolume: 60, selectedOutputDeviceId: 'synthetic-speaker' })
    restored.setNoiseMode('browser')
    expect(localStorage.getItem('mnema_noise')).toBe('browser')
    restored.toggleNoiseCancelling()
    expect(restored.noiseMode).toBe('off')
    restored.toggleNoiseCancelling()
    expect(restored.noiseMode).toBe('ai')
  })

  it('restores custom quality and defaults after corrupted JSON', () => {
    localStorage.setItem(SCREEN_QUALITY_STORAGE_KEY, JSON.stringify({ resolution: 'source', fps: 15, custom: true }))
    expect(useVoiceStore().screenQuality).toEqual({ resolution: 'source', fps: 15, custom: true })
    setActivePinia(createPinia())
    localStorage.setItem(SCREEN_QUALITY_STORAGE_KEY, '{bad')
    expect(useVoiceStore().screenQuality).toEqual({ resolution: 1080, fps: 30 })
    setActivePinia(createPinia())
    localStorage.setItem(SCREEN_QUALITY_STORAGE_KEY, 'null')
    expect(useVoiceStore().screenQuality).toEqual({ resolution: 1080, fps: 30 })
  })

  it('keeps custom quality for the session when persistence fails', () => {
    const voice = useVoiceStore()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('synthetic quota limit') })
    voice.setScreenQuality({ resolution: 720, fps: 60, custom: true })
    expect(voice.screenQuality).toEqual({ resolution: 720, fps: 60, custom: true })
  })
})

describe('measured ping history', () => {
  it('shows no aggregate until measured, rejects invalid samples and keeps thirty readings', () => {
    const voice = useVoiceStore()
    expect([voice.minPing, voice.maxPing, voice.avgPing]).toEqual([null, null, null])
    voice.recordPing(NaN)
    voice.recordPing(-1)
    Reflect.apply(voice.recordPing, voice, ['unmeasured'])
    expect(voice.ping).toBeNull()
    for (let i = 0; i < 31; i++) voice.recordPing(i + 0.4)
    expect(voice.pingHistory).toEqual(Array.from({ length: 30 }, (_, i) => i + 1))
    expect([voice.minPing, voice.maxPing, voice.avgPing]).toEqual([1, 30, 16])
    voice.recordPing(10000)
    expect(voice.ping).toBe(9999)
  })
})

describe('voice membership events and moderator profile updates', () => {
  it('preserves room timing until the last member leaves and updates all participant copies', () => {
    auth.useAuthStore().user = userFixture({ id: 'me' })
    const voice = useVoiceStore()
    voice.setChannel('room')
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'room', started_at: FIXTURE_TIMESTAMP, user: voiceUserFixture({ id: 'other' }) })
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'room', started_at: FIXTURE_TIMESTAMP, user: voiceUserFixture({ id: 'me' }) })
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'elsewhere', started_at: FIXTURE_TIMESTAMP, user: voiceUserFixture({ id: 'other' }) })
    expect(vi.mocked(playSoundEffect).mock.calls.filter(([event]) => event === 'user_join')).toHaveLength(1)
    voice.updateUser(userFixture({ id: 'other', display_name: 'Renamed member' }))
    expect(voice.channelUsers.room?.other).toMatchObject({ display_name: 'Renamed member', joined_at: FIXTURE_TIMESTAMP })
    expect(voice.channelUsers.elsewhere?.other?.display_name).toBe('Renamed member')
    voice.updateUser(userFixture({ id: 'absent', display_name: 'Absent' }))
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'room', user_id: 'other' })
    expect(voice.roomStartedAt.room).toBe(FIXTURE_TIMESTAMP)
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'room', user_id: 'me' })
    expect(voice.roomStartedAt.room).toBeUndefined()
    expect(vi.mocked(playSoundEffect).mock.calls.filter(([event]) => event === 'user_leave')).toHaveLength(1)
  })

  it('loads snapshots, handles missing join times, and replaces room timers without clock data', () => {
    const voice = useVoiceStore()
    voice.setVoiceSnapshot({ room: { member: voiceUserFixture({ id: 'member', joined_at: '' }) } })
    expect(voice.joinedAtOf('member')).toBe('')
    expect(voice.joinedAtOf(null)).toBe('')
    expect(voice.joinedAtOf('absent')).toBe('')
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'room', started_at: '', user: voiceUserFixture({ id: 'member', joined_at: '' }) })
    expect(voice.roomStartedAt).toEqual({})
    voice.setVoiceRooms({ started: { room: FIXTURE_TIMESTAMP }, now: '' })
    expect(voice.roomStartedAt).toEqual({ room: FIXTURE_TIMESTAMP })
    Reflect.apply(voice.setVoiceRooms, voice, [null])
    expect(voice.roomStartedAt).toEqual({})
    voice.setVoiceSnapshot(null)
    expect(voice.channelUsers).toEqual({})
  })

  it('handles absent authentication during event and stage lookups', () => {
    const voice = useVoiceStore()
    vi.spyOn(auth, 'useAuthStore').mockImplementation(() => { throw new Error('no active authentication context') })
    voice.setChannel('room')
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'room', started_at: FIXTURE_TIMESTAMP, user: voiceUserFixture({ id: 'member' }) })
    voice.localScreenStream = streamFixture()
    expect(voice.stage?.userId).toBeNull()
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'room', user_id: 'member' })
    expect(voice.channelUsers.room).toEqual({})
  })

  it('keeps speaking state idempotent and suppresses rings for deafened members', () => {
    const voice = useVoiceStore()
    voice.setVoiceSnapshot({ room: { member: voiceUserFixture({ id: 'member' }) } })
    voice.handleSpeakingEvent({ channel_id: 'room', user_id: 'member', active: true })
    voice.handleSpeakingEvent({ channel_id: 'room', user_id: 'member', active: true })
    expect(voice.speakingUsers).toEqual({ member: true })
    voice.handleMuteState({ channel_id: 'room', user_id: 'member', muted: false, deafened: true })
    expect(voice.isSpeaking('member')).toBe(false)
    voice.handleMuteState({ channel_id: 'missing', user_id: 'member', muted: true, deafened: true })
    voice.handleMuteState({ channel_id: 'room', user_id: 'absent', muted: true, deafened: true })
    voice.handleSpeakingEvent({ channel_id: 'room', user_id: 'member', active: false })
    voice.handleSpeakingEvent({ channel_id: 'room', user_id: 'member', active: false })
    expect(voice.speakingUsers).toEqual({})
    expect(voice.isSpeaking(null)).toBe(false)
    expect(voice.muteStateOf(null)).toEqual({ muted: false, deafened: false })
    expect(voice.muteStateOf('absent')).toEqual({ muted: false, deafened: false })
  })

  it('ignores malformed empty identity events at the defensive store boundary', () => {
    const voice = useVoiceStore()
    Reflect.apply(voice.handleSpeakingEvent, voice, [null])
    Reflect.apply(voice.handleMediaState, voice, [null])
    Reflect.apply(voice.updateUser, voice, [null])
    Reflect.apply(voice.handleMuteState, voice, [null])
    Reflect.apply(voice.handleVoiceStateUpdate, voice, [{ action: 'unsupported', channel_id: 'room' }])
    Reflect.apply(voice.handleVoiceStateUpdate, voice, [{ action: 'join', channel_id: 'room', user: null }])
    Reflect.apply(voice.handleVoiceStateUpdate, voice, [{ action: 'leave', channel_id: 'room', user_id: '' }])
    expect(voice.speakingUsers).toEqual({})
    expect(voice.mediaState).toEqual({})
    expect(voice.channelUsers).toEqual({ room: {} })
  })
})

describe('local mute, deafen and call controls', () => {
  it('mutes actual outgoing tracks, keeps local marks authoritative, and does not unmute on undeafen', () => {
    auth.useAuthStore().user = userFixture({ id: 'me' })
    const voice = useVoiceStore()
    const stream = streamFixture()
    const track = new TestTrack()
    stream.addTrack(track)
    voice.localAudioStream = stream
    voice.toggleMute()
    expect(track.enabled).toBe(false)
    expect(voice.muteStateOf('me')).toEqual({ muted: true, deafened: false })
    voice.toggleMute()
    expect(track.enabled).toBe(true)
    voice.toggleDeafen()
    expect(track.enabled).toBe(false)
    expect(voice.muteStateOf('me')).toEqual({ muted: true, deafened: true })
    voice.toggleDeafen()
    expect(voice.isMuted).toBe(true)
    expect(voice.isDeafened).toBe(false)
    voice.localAudioStream = null
    voice.toggleMute()
    voice.toggleDeafen()
    expect(voice.isMuted).toBe(true)
    expect(vi.mocked(playSoundEffect)).toHaveBeenCalledWith('unmute')
    expect(vi.mocked(playSoundEffect)).toHaveBeenCalledWith('undeafen')
  })

  it('toggles stream audio independently and joining or leaving changes the active view', () => {
    const voice = useVoiceStore()
    voice.toggleScreenAudioMute()
    expect(voice.isScreenAudioMuted).toBe(true)
    voice.toggleScreenAudioMute()
    expect(voice.isScreenAudioMuted).toBe(false)
    voice.setChannel('room')
    expect(voice.activeView).toBe('voice')
    voice.setChannel(null)
    expect(voice.isConnected).toBe(false)
    voice.disconnect()
    expect(voice.activeView).toBe('chat')
    expect(vi.mocked(playSoundEffect)).not.toHaveBeenCalledWith('leave')
  })
})

describe('subscription replay and stale media arrivals', () => {
  it('replays camera opt-outs and screen opt-ins after reconnect and drops ended shares', () => {
    const voice = useVoiceStore()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.setAllCamerasOff(true)
    voice.setCameraHidden('hidden', true)
    voice.watchScreen('live')
    voice.watchScreen('ended')
    voice.handleMediaState({ channel_id: 'room', user_id: 'live', screen: true })
    voice.setRemoteScreen('live', streamFixture())
    sink.mockClear()
    voice.resendSubscriptions()
    expect(sink.mock.calls.map(([payload]) => payload)).toEqual([
      { kind: 'camera', all: true, on: false }, { kind: 'camera', user_id: 'hidden', on: false },
      { kind: 'screen', user_id: 'live', on: true }, { kind: 'screen', user_id: 'ended', on: true },
    ])
    voice.resetRemoteMedia()
    expect(voice.watchedScreens).toEqual({ live: true })
    expect(voice.remoteScreenStreams).toEqual({})
    voice.setAllCamerasOff(false)
    sink.mockClear()
    voice.resendSubscriptions()
    expect(sink).not.toHaveBeenCalledWith({ kind: 'camera', all: true, on: false })
    voice.setSubscriptionSink(null)
    voice.setCameraHidden('hidden', false)
    expect(sink).toHaveBeenCalledTimes(2)
  })

  it('ignores absent identities and unselected streams without changing current subscriptions', () => {
    const voice = useVoiceStore()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.setUserVolume(null, 20)
    voice.setStreamVolume(null, 20)
    voice.toggleStreamMute(null)
    voice.toggleLocalMute(null)
    voice.setCameraHidden(null, true)
    voice.toggleCameraHidden(null)
    voice.watchScreen(null)
    voice.unwatchScreen(null)
    voice.unwatchScreen('unwatched')
    voice.focusScreen(null)
    voice.setRemoteScreen(null, streamFixture())
    voice.removeRemoteScreen('absent')
    expect(voice.userVolumes).toEqual({})
    expect(voice.streamVolumes).toEqual({})
    expect(voice.getUserVolume(null)).toBe(100)
    expect(voice.getStreamVolume(null)).toBe(50)
    expect(voice.streamVolumeShown(null)).toBe(50)
    expect(voice.viewersOf(null)).toEqual([])
    expect(voice.cameraStreamOf(null)).toBeNull()
    expect(sink).not.toHaveBeenCalled()
  })

  it('retains a newer screen on stale track end and updates another watched share without stealing focus', () => {
    const voice = useVoiceStore()
    const old = streamFixture()
    const newer = streamFixture()
    const other = streamFixture()
    voice.watchScreen('first')
    voice.setRemoteScreen('first', old)
    voice.setRemoteScreen('first', newer)
    voice.removeRemoteScreen('first', old)
    expect(voice.remoteScreenStream).toBe(newer)
    voice.watchScreen('second')
    voice.setRemoteScreen('second', other)
    voice.focusScreen('first')
    voice.setRemoteScreen('second', streamFixture())
    expect(voice.remoteScreenUserId).toBe('first')
    expect(voice.remoteScreenStream).toBe(newer)
    voice.removeRemoteScreen('first', newer)
    expect(voice.remoteScreenStream).toBeNull()
    voice.localScreenStream = old
    voice.localScreenStream = newer
    expect(voice.stage?.stream).toBe(newer)
    voice.toggleCameraFocus('absent')
    expect(voice.focusedCamera).toBeNull()
  })
})
