import { required, streamFixture } from '../store-test-support.fixture'
import type { VoiceStore } from './voice'
import { userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useVoiceStore } from './voice'
import { useAuthStore } from './auth'
import { SCREEN_QUALITY_STORAGE_KEY } from '../lib/streamQuality'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('the screen share stage', () => {
  // A remote share that is published, watched and received.
  function receive(voice: VoiceStore, id: string, stream = streamFixture()) {
    voice.handleMediaState({ channel_id: 'v1', user_id: id, screen: true })
    voice.watchScreen(id)
    voice.setRemoteScreen(id, stream)
    return stream
  }

  it('is empty without any share', () => {
    expect(useVoiceStore().stage).toBeNull()
  })

  it('starting to share while watching nobody puts my own share on the stage', () => {
    const voice = useVoiceStore()
    const own = streamFixture()
    voice.localScreenStream = own
    expect(voice.stage).toEqual({ kind: 'screen', own: true, userId: null, stream: own })
    expect(voice.ownScreenFocused).toBe(true)
  })

  it('starting to share while watching someone keeps their share on the stage', () => {
    const voice = useVoiceStore()
    const a = receive(voice, 'a')
    voice.localScreenStream = streamFixture()
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'a', stream: a })
    expect(voice.ownScreenFocused).toBe(false)
  })

  it('switches between my own share and a watched one both ways', () => {
    const voice = useVoiceStore()
    const own = streamFixture()
    voice.localScreenStream = own
    const a = receive(voice, 'a')
    // Watching puts theirs on the stage.
    expect(required(voice.stage).stream).toBe(a)
    voice.focusOwnScreen()
    expect(voice.stage).toEqual({ kind: 'screen', own: true, userId: null, stream: own })
    // Their share is still received and in focus for the way back.
    expect(voice.watchedScreens).toEqual({ a: true })
    voice.focusScreen('a')
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'a', stream: a })
  })

  it('keeps my own share on the stage until a newly watched share arrives', () => {
    const voice = useVoiceStore()
    const own = streamFixture()
    voice.localScreenStream = own
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    expect(required(voice.stage).stream).toBe(own)
    const a = streamFixture()
    voice.setRemoteScreen('a', a)
    expect(required(voice.stage).stream).toBe(a)
  })

  it('a share arriving after I chose my own does not replace it on the stage', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = streamFixture()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.focusOwnScreen()
    voice.setRemoteScreen('a', streamFixture())
    expect(required(voice.stage).own).toBe(true)
  })

  it('falls back to my own share when the watched one on the stage ends', () => {
    const voice = useVoiceStore()
    receive(voice, 'a')
    const own = streamFixture()
    voice.localScreenStream = own
    expect(required(voice.stage).own).toBe(false)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: false })
    expect(voice.stage).toEqual({ kind: 'screen', own: true, userId: null, stream: own })
  })

  it('falls back to another watched share before my own', () => {
    const voice = useVoiceStore()
    receive(voice, 'a')
    const b = receive(voice, 'b')
    voice.localScreenStream = streamFixture()
    voice.focusScreen('a')
    voice.unwatchScreen('a')
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'b', stream: b })
  })

  it('falls back to a watched share when I stop sharing', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = streamFixture()
    const a = receive(voice, 'a')
    voice.focusOwnScreen()
    voice.localScreenStream = null
    expect(voice.ownScreenFocused).toBe(false)
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'a', stream: a })
  })

  it('focusing my own share needs one, a remote share needs an opt-in', () => {
    const voice = useVoiceStore()
    voice.focusOwnScreen()
    expect(voice.ownScreenFocused).toBe(false)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.focusScreen('a')
    expect(voice.remoteScreenUserId).toBeNull()
    expect(voice.watchedScreens).toEqual({})
  })

  it('leaving the call clears the choice', () => {
    const voice = useVoiceStore()
    voice.localScreenStream = streamFixture()
    voice.focusCamera('u1')
    voice.disconnect()
    expect(voice.ownScreenFocused).toBe(false)
    expect(voice.focusedCamera).toBeNull()
  })
})

describe('a camera on the stage', () => {
  function withScreen(voice: VoiceStore, id: string, stream = streamFixture()) {
    voice.handleMediaState({ channel_id: 'v1', user_id: id, screen: true })
    voice.watchScreen(id)
    voice.setRemoteScreen(id, stream)
    return stream
  }

  it('a remote camera goes on the stage and back off', () => {
    const voice = useVoiceStore()
    const cam = streamFixture()
    voice.setUserVideoStream('a', cam)
    voice.focusCamera('a')
    expect(voice.stage).toEqual({ kind: 'camera', own: false, userId: 'a', stream: cam })
    voice.toggleCameraFocus('a')
    expect(voice.stage).toBeNull()
  })

  it('my own camera can go on the stage', () => {
    useAuthStore().user = userFixture({ id: 'me', username: 'me' })
    const voice = useVoiceStore()
    const cam = streamFixture()
    voice.localCameraStream = cam
    voice.focusCamera('me')
    expect(voice.stage).toEqual({ kind: 'camera', own: true, userId: 'me', stream: cam })
    // Turning it off takes it off the stage.
    voice.localCameraStream = null
    expect(voice.focusedCamera).toBeNull()
    expect(voice.stage).toBeNull()
  })

  it('a camera that is off or hidden cannot be focused', () => {
    const voice = useVoiceStore()
    voice.focusCamera('a')
    expect(voice.focusedCamera).toBeNull()
    voice.setUserVideoStream('b', streamFixture())
    voice.hiddenCameras = { b: true }
    expect(voice.canFocusCamera('b')).toBe(false)
    voice.focusCamera('b')
    expect(voice.focusedCamera).toBeNull()
  })

  it('switches between a camera and screen shares both ways', () => {
    const voice = useVoiceStore()
    const screen = withScreen(voice, 'a')
    const own = streamFixture()
    voice.localScreenStream = own
    const cam = streamFixture()
    voice.setUserVideoStream('b', cam)

    voice.focusCamera('b')
    expect(required(voice.stage).stream).toBe(cam)
    // Unfocusing returns to the share it replaced.
    voice.unfocusCamera()
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'a', stream: screen })

    voice.focusCamera('b')
    voice.focusOwnScreen()
    expect(required(voice.stage).stream).toBe(own)
    voice.focusCamera('b')
    voice.focusScreen('a')
    expect(required(voice.stage).stream).toBe(screen)
  })

  it('watching a new share replaces the camera; a share arriving on its own does not', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('b', streamFixture())
    voice.focusCamera('b')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.localScreenStream = streamFixture()
    expect(required(voice.stage).kind).toBe('camera')
    voice.watchScreen('a')
    expect(voice.focusedCamera).toBeNull()
    const screen = streamFixture()
    voice.setRemoteScreen('a', screen)
    expect(required(voice.stage).stream).toBe(screen)
  })

  it('falls back to a screen share, else the grid, when the camera goes', () => {
    const voice = useVoiceStore()
    const screen = withScreen(voice, 'a')
    const cam = streamFixture()
    voice.setUserVideoStream('b', cam)
    voice.setUserVideoStream('c', streamFixture())

    voice.focusCamera('b')
    voice.removeUserVideoStream('b', cam)
    expect(voice.focusedCamera).toBeNull()
    expect(voice.stage).toEqual({ kind: 'screen', own: false, userId: 'a', stream: screen })

    voice.unwatchScreen('a')
    voice.focusCamera('c')
    // Hiding it locally (or all cameras) takes it off the stage too.
    voice.setCameraHidden('c', true)
    expect(voice.stage).toBeNull()
  })

  it('a member leaving takes their camera off the stage', () => {
    const voice = useVoiceStore()
    voice.channelUsers = { v1: { b: voiceUserFixture({ id: 'b' }) } }
    voice.setUserVideoStream('b', streamFixture())
    voice.focusCamera('b')
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'v1', user_id: 'b' })
    expect(voice.stage).toBeNull()
  })

  it('hiding all cameras takes a focused one off the stage', () => {
    const voice = useVoiceStore()
    voice.setUserVideoStream('b', streamFixture())
    voice.focusCamera('b')
    voice.setAllCamerasOff(true)
    expect(voice.focusedCamera).toBeNull()
  })
})

describe('who watches a screen share', () => {
  function inRoom(channel = 'v1') {
    const voice = useVoiceStore()
    voice.setChannel(channel)
    return voice
  }

  it('keeps the viewers per sharer for the room I am in', () => {
    const voice = inRoom()
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: ['ben', 'carl'] })
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'me', viewers: ['anna'] })
    expect(voice.viewersOf('anna')).toEqual(['ben', 'carl'])
    expect(voice.viewersOf('me')).toEqual(['anna'])
    expect(voice.viewersOf('nobody')).toEqual([])
  })

  it('replaces the list on change and drops it at zero viewers', () => {
    const voice = inRoom()
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: ['ben', 'carl'] })
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: ['carl'] })
    expect(voice.viewersOf('anna')).toEqual(['carl'])
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: [] })
    expect(voice.viewersOf('anna')).toEqual([])
    expect(voice.screenViewers).toEqual({})
  })

  it('ignores other rooms, broken payloads, duplicates and the sharer itself', () => {
    const voice = inRoom()
    voice.handleScreenViewers({ channel_id: 'v2', user_id: 'anna', viewers: ['ben'] })
    // Deliberately malformed inputs exercise runtime resilience after typed wire validation.
    Reflect.apply(voice.handleScreenViewers, voice, [{ channel_id: 'v1', viewers: ['ben'] }])
    Reflect.apply(voice.handleScreenViewers, voice, [null])
    expect(voice.screenViewers).toEqual({})
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: ['ben', 'ben', 'anna', ''] })
    expect(voice.viewersOf('anna')).toEqual(['ben'])
    Reflect.apply(voice.handleScreenViewers, voice, [{ channel_id: 'v1', user_id: 'anna', viewers: null }])
    expect(voice.viewersOf('anna')).toEqual([])
  })

  it('needs a room: an event before joining is dropped', () => {
    const voice = useVoiceStore()
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'anna', viewers: ['ben'] })
    expect(voice.screenViewers).toEqual({})
  })

  it('is cleared when the share ends, on a room switch, a reconnect and on leaving', () => {
    const voice = inRoom()
    const seedViewers = () => voice.handleScreenViewers({ channel_id: required(voice.currentChannelId), user_id: 'anna', viewers: ['ben'] })

    seedViewers()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: false, camera: true })
    expect(voice.viewersOf('anna')).toEqual([])

    seedViewers()
    voice.setChannel('v1') // same room: kept
    expect(voice.viewersOf('anna')).toEqual(['ben'])
    voice.setChannel('v2')
    expect(voice.viewersOf('anna')).toEqual([])

    seedViewers()
    voice.resetRemoteMedia()
    expect(voice.viewersOf('anna')).toEqual([])

    seedViewers()
    voice.disconnect()
    expect(voice.screenViewers).toEqual({})
  })

  it('arrives through the chat store WebSocket switch', async () => {
    const { useChatStore } = await import('./chat')
    const voice = inRoom()
    useChatStore().handleWSEvent({ type: 'screen_viewers', payload: { channel_id: 'v1', user_id: 'anna', viewers: ['ben'] } })
    expect(voice.viewersOf('anna')).toEqual(['ben'])
  })
})


describe('screen quality persistence and modal', () => {
  it('defaults to 1080p at 30 fps', () => {
    const voice = useVoiceStore()
    expect(voice.screenQuality.resolution).toBe(1080)
    expect(voice.screenQuality.fps).toBe(30)
  })

  it('persists chosen quality across store instances', () => {
    const voice = useVoiceStore()
    voice.setScreenQuality({ resolution: 1440, fps: 60 })
    expect(voice.screenQuality).toEqual({ resolution: 1440, fps: 60 })
    expect(JSON.parse(localStorage.getItem(SCREEN_QUALITY_STORAGE_KEY) ?? '{}')).toEqual({ resolution: 1440, fps: 60 })

    setActivePinia(createPinia())
    const nextVoice = useVoiceStore()
    expect(nextVoice.screenQuality).toEqual({ resolution: 1440, fps: 60 })
  })

  it('resetScreenQuality resets to the default 1080p/30fps', () => {
    const voice = useVoiceStore()
    voice.setScreenQuality({ resolution: 'source', fps: 15, custom: true })
    voice.resetScreenQuality()
    expect(voice.screenQuality).toEqual({ resolution: 1080, fps: 30 })
  })

  it('toggles screen share modal state', () => {
    const voice = useVoiceStore()
    expect(voice.showScreenShareModal).toBe(false)
    voice.openScreenShareModal()
    expect(voice.showScreenShareModal).toBe(true)
    voice.closeScreenShareModal()
    expect(voice.showScreenShareModal).toBe(false)
  })
})
