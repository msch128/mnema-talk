import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useVoiceStore } from './voice'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
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
    expect(JSON.parse(localStorage.getItem('mnema_user_volumes'))).toEqual({})
    expect(JSON.parse(localStorage.getItem('mnema_user_muted'))).toEqual({})
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
    const oldStream = {}
    const newStream = {}
    voice.setUserVideoStream('u1', oldStream)
    voice.setUserVideoStream('u1', newStream)
    voice.removeUserVideoStream('u1', oldStream)
    expect(voice.userVideoStreams.u1).toBe(newStream)
    voice.removeUserVideoStream('u1', newStream)
    expect(voice.userVideoStreams.u1).toBeUndefined()
  })

  it('drops a participant camera when they leave', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('u1', {})
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
    expect(JSON.parse(localStorage.getItem('mnema_hidden_cameras'))).toEqual({})
  })

  it('all cameras off hides everyone and is persisted', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('u1', {})
    voice.setAllCamerasOff(true)
    expect(voice.isCameraHidden('anyone')).toBe(true)
    expect(voice.userVideoStreams).toEqual({})
    voice.setUserVideoStream('u1', {})
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

    voice.setRemoteScreen('u1', {})
    expect(voice.remoteScreenStream).toBeNull()

    voice.watchScreen('u1')
    expect(sink).toHaveBeenCalledWith({ kind: 'screen', user_id: 'u1', on: true })
    const stream = {}
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
    const a = {}
    const b = {}
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
    voice.handleMediaState({ user_id: 'u1', screen: true })
    voice.watchScreen('u1')
    voice.setRemoteScreen('u1', {})
    voice.handleMediaState({ user_id: 'u1', screen: false })
    expect(voice.watchedScreens).toEqual({})
    expect(voice.remoteScreenStream).toBeNull()
    expect(voice.mediaState.u1).toBeUndefined()
  })

  it('leaving the call drops all media state', () => {
    const voice = useVoiceStore()
    voice.handleMediaState({ user_id: 'u1', screen: true, camera: true })
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

    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'v2', started_at: '2026-10-05T11:00:00Z', user: { id: 'u1', joined_at: '2026-10-05T11:00:00Z' } })
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
    voice.handleVoiceStateUpdate({ action: 'join', channel_id: 'v1', user: { id: 'u2' } })
    voice.handleSpeakingEvent({ user_id: 'u2', active: true })
    expect(voice.isSpeaking('u2')).toBe(true)

    voice.handleMuteState({ channel_id: 'v1', user_id: 'u2', muted: true, deafened: false })
    expect(voice.muteStateOf('u2')).toEqual({ muted: true, deafened: false })
    expect(voice.isSpeaking('u2')).toBe(false)

    voice.handleMuteState({ channel_id: 'v1', user_id: 'u2', muted: false, deafened: false })
    expect(voice.isSpeaking('u2')).toBe(true)
  })
})

describe('the screen share stage', () => {
  // A remote share that is published, watched and received.
  function receive(voice, id, stream = { id }) {
    voice.handleMediaState({ user_id: id, screen: true })
    voice.watchScreen(id)
    voice.setRemoteScreen(id, stream)
    return stream
  }

  it('is empty without any share', () => {
    expect(useVoiceStore().stage).toBeNull()
  })

  it('starting to share while watching nobody puts my own share on the stage', () => {
    const voice = useVoiceStore()
    const own = {}
    voice.localScreenStream = own
    expect(voice.stage).toEqual({ kind: 'own', userId: null, stream: own })
    expect(voice.ownScreenFocused).toBe(true)
  })

  it('starting to share while watching someone keeps their share on the stage', () => {
    const voice = useVoiceStore()
    const a = receive(voice, 'a')
    voice.localScreenStream = {}
    expect(voice.stage).toEqual({ kind: 'remote', userId: 'a', stream: a })
    expect(voice.ownScreenFocused).toBe(false)
  })

  it('switches between my own share and a watched one both ways', () => {
    const voice = useVoiceStore()
    const own = {}
    voice.localScreenStream = own
    const a = receive(voice, 'a')
    // Watching puts theirs on the stage.
    expect(voice.stage.stream).toBe(a)
    voice.focusOwnScreen()
    expect(voice.stage).toEqual({ kind: 'own', userId: null, stream: own })
    // Their share is still received and in focus for the way back.
    expect(voice.watchedScreens).toEqual({ a: true })
    voice.focusScreen('a')
    expect(voice.stage).toEqual({ kind: 'remote', userId: 'a', stream: a })
  })

  it('keeps my own share on the stage until a newly watched share arrives', () => {
    const voice = useVoiceStore()
    const own = {}
    voice.localScreenStream = own
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    expect(voice.stage.stream).toBe(own)
    const a = {}
    voice.setRemoteScreen('a', a)
    expect(voice.stage.stream).toBe(a)
  })

  it('a share arriving after I chose my own does not replace it on the stage', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = {}
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.focusOwnScreen()
    voice.setRemoteScreen('a', {})
    expect(voice.stage.kind).toBe('own')
  })

  it('falls back to my own share when the watched one on the stage ends', () => {
    const voice = useVoiceStore()
    receive(voice, 'a')
    const own = {}
    voice.localScreenStream = own
    expect(voice.stage.kind).toBe('remote')
    voice.handleMediaState({ user_id: 'a', screen: false })
    expect(voice.stage).toEqual({ kind: 'own', userId: null, stream: own })
  })

  it('falls back to another watched share before my own', () => {
    const voice = useVoiceStore()
    receive(voice, 'a')
    const b = receive(voice, 'b')
    voice.localScreenStream = {}
    voice.focusScreen('a')
    voice.unwatchScreen('a')
    expect(voice.stage).toEqual({ kind: 'remote', userId: 'b', stream: b })
  })

  it('falls back to a watched share when I stop sharing', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = {}
    const a = receive(voice, 'a')
    voice.focusOwnScreen()
    voice.localScreenStream = null
    expect(voice.ownScreenFocused).toBe(false)
    expect(voice.stage).toEqual({ kind: 'remote', userId: 'a', stream: a })
  })

  it('focusing my own share needs one, a remote share needs an opt-in', () => {
    const voice = useVoiceStore()
    voice.focusOwnScreen()
    expect(voice.ownScreenFocused).toBe(false)
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.focusScreen('a')
    expect(voice.remoteScreenUserId).toBeNull()
    expect(voice.watchedScreens).toEqual({})
  })

  it('leaving the call clears the choice', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = {}
    voice.disconnect()
    expect(voice.ownScreenFocused).toBe(false)
  })
})
