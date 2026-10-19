import { streamFixture } from '../store-test-support.fixture'
import { voiceUserFixture, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useVoiceStore } from './voice'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe("the volume of someone's stream", () => {
  it('starts at 50 %, apart from the voice volume', () => {
    const voice = useVoiceStore()
    expect(voice.getStreamVolume('u1')).toBe(50)
    expect(voice.streamGain('u1')).toBe(0.5)
    voice.setStreamVolume('u1', 80)
    expect(voice.getStreamVolume('u1')).toBe(80)
    expect(voice.getUserVolume('u1')).toBe(100)
    voice.setUserVolume('u1', 150)
    expect(voice.getStreamVolume('u1')).toBe(80)
  })

  it('is clamped to 0..100 % and remembered per person once changed', () => {
    const voice = useVoiceStore()
    voice.setStreamVolume('u1', 150)
    voice.setStreamVolume('u2', -3)
    voice.setStreamVolume('u3', 'nope')
    voice.setStreamVolume('u4', 50)
    expect(voice.getStreamVolume('u1')).toBe(100)
    expect(voice.getStreamVolume('u2')).toBe(0)
    expect(voice.getStreamVolume('u3')).toBe(50)
    setActivePinia(createPinia())
    const again = useVoiceStore()
    expect(again.getStreamVolume('u1')).toBe(100)
    expect(again.getStreamVolume('u2')).toBe(0)
    expect(JSON.parse(localStorage.getItem('mnema_stream_volumes') ?? '{}')).toEqual({ u1: 100, u2: 0, u4: 50 })
  })

  it('0 % is muted; mute keeps the volume, the slider unmutes', () => {
    const voice = useVoiceStore()
    voice.setStreamVolume('u1', 70)
    voice.toggleStreamMute('u1')
    expect(voice.isStreamMuted('u1')).toBe(true)
    expect(voice.streamGain('u1')).toBe(0)
    expect(voice.streamVolumeShown('u1')).toBe(0)
    expect(voice.getStreamVolume('u1')).toBe(70)
    expect(voice.isUserLocalMuted('u1')).toBe(false)
    voice.toggleStreamMute('u1')
    expect(voice.streamGain('u1')).toBe(0.7)

    voice.toggleStreamMute('u1')
    voice.setStreamVolume('u1', 40)
    expect(voice.isStreamMuted('u1')).toBe(false)
    expect(voice.streamVolumeShown('u1')).toBe(40)

    // All the way down counts as muted; unmuting brings back the default.
    voice.setStreamVolume('u1', 0)
    expect(voice.isStreamMuted('u1')).toBe(true)
    voice.toggleStreamMute('u1')
    expect(voice.getStreamVolume('u1')).toBe(50)
    expect(voice.isStreamMuted('u1')).toBe(false)
  })

  it('still works when storage is unavailable', () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const voice = useVoiceStore()
    voice.setStreamVolume('u1', 20)
    expect(voice.getStreamVolume('u1')).toBe(20)
    setItem.mockRestore()
    localStorage.setItem('mnema_stream_volumes', '[1,2')
    setActivePinia(createPinia())
    expect(useVoiceStore().getStreamVolume('u1')).toBe(50)
  })
})

describe('per-user volume and local mute', () => {
  it('defaults to 100 % and not muted', () => {
    const voice = useVoiceStore()
    expect(voice.getUserVolume('u1')).toBe(100)
    expect(voice.isUserLocalMuted('u1')).toBe(false)
  })

  it('stores a volume per user, clamped to 0..200', () => {
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 150)
    voice.setUserVolume('u2', 999)
    voice.setUserVolume('u3', -5)
    voice.setUserVolume('u4', 'nope')
    expect(voice.getUserVolume('u1')).toBe(150)
    expect(voice.getUserVolume('u2')).toBe(200)
    expect(voice.getUserVolume('u3')).toBe(0)
    expect(voice.getUserVolume('u4')).toBe(100)
  })

  it('persists across store instances and drops default entries', () => {
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 40)
    voice.toggleLocalMute('u2')
    setActivePinia(createPinia())
    const again = useVoiceStore()
    expect(again.getUserVolume('u1')).toBe(40)
    expect(again.isUserLocalMuted('u2')).toBe(true)

    again.setUserVolume('u1', 100)
    again.toggleLocalMute('u2')
    expect(JSON.parse(localStorage.getItem('mnema_user_volumes') ?? '{}')).toEqual({})
    expect(JSON.parse(localStorage.getItem('mnema_user_muted') ?? '{}')).toEqual({})
  })

  it('still works when storage is unavailable', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked') })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    voice.setUserVolume('u1', 70)
    voice.toggleLocalMute('u1')
    expect(voice.getUserVolume('u1')).toBe(70)
    expect(voice.isUserLocalMuted('u1')).toBe(true)
    getItem.mockRestore()
    setItem.mockRestore()
  })

  it('ignores corrupt stored values', () => {
    localStorage.setItem('mnema_user_volumes', '[1,2')
    localStorage.setItem('mnema_user_muted', '"x"')
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    expect(voice.getUserVolume('u1')).toBe(100)
    expect(voice.isUserLocalMuted('u1')).toBe(false)
  })
})

describe('participant video streams', () => {
  it('keeps a newer stream when a stale one is removed', () => {
    const voice = useVoiceStore()
    const oldStream = streamFixture()
    const newStream = streamFixture()
    voice.setUserVideoStream('u1', oldStream)
    voice.setUserVideoStream('u1', newStream)
    voice.removeUserVideoStream('u1', oldStream)
    expect(voice.userVideoStreams.u1).toBe(newStream)
    voice.removeUserVideoStream('u1', newStream)
    expect(voice.userVideoStreams.u1).toBeUndefined()
  })

  it('drops a participant camera when they leave', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('u1', streamFixture())
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'c', user_id: 'u1' })
    expect(voice.userVideoStreams.u1).toBeUndefined()
  })
})

describe('video subscriptions', () => {
  it('cameras are visible by default and opt-outs are persisted', () => {
    const voice = useVoiceStore()
    expect(voice.isCameraHidden('u1')).toBe(false)
    voice.setCameraHidden('u1', true)
    expect(voice.isCameraHidden('u1')).toBe(true)
    expect(voice.isCameraHidden('u2')).toBe(false)

    setActivePinia(createPinia())
    const again = useVoiceStore()
    expect(again.isCameraHidden('u1')).toBe(true)
    again.toggleCameraHidden('u1')
    expect(again.isCameraHidden('u1')).toBe(false)
    expect(JSON.parse(localStorage.getItem('mnema_hidden_cameras') ?? '{}')).toEqual({})
  })

  it('all cameras off hides everyone and is persisted', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('u1', streamFixture())
    voice.setAllCamerasOff(true)
    expect(voice.isCameraHidden('anyone')).toBe(true)
    expect(voice.userVideoStreams).toEqual({})
    voice.setUserVideoStream('u1', streamFixture())
    expect(voice.userVideoStreams).toEqual({})
    setActivePinia(createPinia())
    expect(useVoiceStore().allCamerasOff).toBe(true)
  })

  it('survives blocked storage', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    const voice = useVoiceStore()
    expect(() => voice.setCameraHidden('u1', true)).not.toThrow()
    expect(voice.isCameraHidden('u1')).toBe(true)
    spy.mockRestore()
  })

  it('screen shares are opt-in, not persisted, and watching sends a subscribe', () => {
    const voice = useVoiceStore()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    expect(voice.watchedScreens).toEqual({})

    voice.setRemoteScreen('u1', streamFixture())
    expect(voice.remoteScreenStream).toBeNull()

    voice.watchScreen('u1')
    expect(sink).toHaveBeenCalledWith({ kind: 'screen', user_id: 'u1', on: true })
    const stream = streamFixture()
    voice.setRemoteScreen('u1', stream)
    expect(voice.remoteScreenStream).toBe(stream)
    expect(voice.remoteScreenUserId).toBe('u1')

    voice.unwatchScreen('u1')
    expect(sink).toHaveBeenLastCalledWith({ kind: 'screen', user_id: 'u1', on: false })
    expect(voice.remoteScreenStream).toBeNull()
    expect(voice.remoteScreenUserId).toBeNull()

    setActivePinia(createPinia())
    expect(useVoiceStore().watchedScreens).toEqual({})
  })

  it('several shares: the viewer picks which one is on the stage', () => {
    const voice = useVoiceStore()
    const a = streamFixture()
    const b = streamFixture()
    voice.watchScreen('a')
    voice.watchScreen('b')
    voice.setRemoteScreen('a', a)
    voice.setRemoteScreen('b', b)
    voice.focusScreen('a')
    expect(voice.remoteScreenStream).toBe(a)
    voice.unwatchScreen('a')
    expect(voice.remoteScreenStream).toBe(b)
    expect(voice.remoteScreenUserId).toBe('b')
  })

  it('a finished share ends the opt-in', () => {
    const voice = useVoiceStore()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'u1', screen: true })
    voice.watchScreen('u1')
    voice.setRemoteScreen('u1', streamFixture())
    voice.handleMediaState({ channel_id: 'v1', user_id: 'u1', screen: false })
    expect(voice.watchedScreens).toEqual({})
    expect(voice.remoteScreenStream).toBeNull()
    expect(voice.mediaState.u1).toBeUndefined()
  })

  it('leaving the call drops all media state', () => {
    const voice = useVoiceStore()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'u1', screen: true, camera: true })
    voice.watchScreen('u1')
    voice.disconnect()
    expect(voice.watchedScreens).toEqual({})
    expect(voice.mediaState).toEqual({})
  })
})

describe('Talk timers', () => {
  it('tracks when rooms started and when users joined', () => {
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    voice.setVoiceRooms({ started: { v1: '2026-10-05T10:00:00Z' }, now: new Date().toISOString() })
    expect(voice.roomStartedAt.v1).toBe('2026-10-05T10:00:00Z')

    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'v2', started_at: '2026-10-05T11:00:00Z', user: voiceUserFixture({ id: 'u1', joined_at: '2026-10-05T11:00:00Z' }) })
    expect(voice.roomStartedAt.v2).toBe('2026-10-05T11:00:00Z')
    expect(voice.joinedAtOf('u1')).toBe('2026-10-05T11:00:00Z')

    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'v2', user_id: 'u1' })
    expect(voice.roomStartedAt.v2).toBeUndefined()
    expect(voice.joinedAtOf('u1')).toBe('')
  })
})

describe('mute marks and the speaking ring', () => {
  it('never shows a muted or deafened member as speaking', () => {
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'v1', user: voiceUserFixture({ id: 'u2' }), started_at: FIXTURE_TIMESTAMP })
    voice.handleSpeakingEvent({ channel_id: 'v1', user_id: 'u2', active: true })
    expect(voice.isSpeaking('u2')).toBe(true)

    voice.handleMuteState({ channel_id: 'v1', user_id: 'u2', muted: true, deafened: false })
    expect(voice.muteStateOf('u2')).toEqual({ muted: true, deafened: false })
    expect(voice.isSpeaking('u2')).toBe(false)

    voice.handleMuteState({ channel_id: 'v1', user_id: 'u2', muted: false, deafened: false })
    expect(voice.isSpeaking('u2')).toBe(true)
  })
})

describe('hide participants without video', () => {
  it('is off by default, toggles and is remembered per browser', () => {
    const voice = useVoiceStore()
    expect(voice.hideNoVideo).toBe(false)
    voice.toggleHideNoVideo()
    expect(voice.hideNoVideo).toBe(true)
    setActivePinia(createPinia())
    expect(useVoiceStore().hideNoVideo).toBe(true)
    useVoiceStore().setHideNoVideo(false)
    setActivePinia(createPinia())
    expect(useVoiceStore().hideNoVideo).toBe(false)
  })

  it('treats a broken stored value as off', () => {
    localStorage.setItem('mnema_hide_no_video', '{oops')
    expect(useVoiceStore().hideNoVideo).toBe(false)
    setActivePinia(createPinia())
    localStorage.setItem('mnema_hide_no_video', '"yes"')
    expect(useVoiceStore().hideNoVideo).toBe(false)
  })
})

