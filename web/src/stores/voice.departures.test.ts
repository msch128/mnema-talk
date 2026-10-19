import { streamFixture } from '../store-test-support.fixture'
import { voiceUserFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useVoiceStore } from './voice'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})

describe('voice departures are scoped to their channel', () => {
  it('preserves the current room media when a delayed departure belongs to the previous room', () => {
    const voice = useVoiceStore()
    voice.setChannel('current-room')
    const participant = voiceUserFixture({ id: 'participant' })
    voice.channelUsers = { 'previous-room': { participant }, 'current-room': { participant } }
    voice.handleSpeakingEvent({ channel_id: 'current-room', user_id: 'participant', active: true })
    const camera = streamFixture()
    const screen = streamFixture()
    voice.setUserVideoStream('participant', camera)
    voice.handleMediaState({ user_id: 'participant', channel_id: 'current-room', screen: true, camera: true })
    voice.watchScreen('participant')
    voice.setRemoteScreen('participant', screen)
    voice.handleScreenViewers({ user_id: 'participant', channel_id: 'current-room', viewers: ['viewer'] })
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'previous-room', user_id: 'participant' })
    expect(voice.channelUsers['previous-room']).toEqual({})
    expect(voice.channelUsers['current-room']?.participant).toEqual(participant)
    expect(voice.speakingUsers.participant).toBe(true)
    expect(voice.userVideoStreams.participant).toBe(camera)
    expect(voice.mediaState.participant).toEqual({ channel_id: 'current-room', screen: true, camera: true })
    expect(voice.watchedScreens.participant).toBe(true)
    expect(voice.remoteScreenStream).toBe(screen)
    expect(voice.viewersOf('participant')).toEqual(['viewer'])
  })

  it('cleans the current room media when its participant actually leaves', () => {
    const voice = useVoiceStore()
    voice.setChannel('current-room')
    const participant = voiceUserFixture({ id: 'participant' })
    voice.channelUsers = { 'current-room': { participant } }
    voice.handleSpeakingEvent({ channel_id: 'current-room', user_id: 'participant', active: true })
    voice.setUserVideoStream('participant', streamFixture())
    voice.handleMediaState({ user_id: 'participant', channel_id: 'current-room', screen: true, camera: true })
    voice.watchScreen('participant')
    voice.setRemoteScreen('participant', streamFixture())
    voice.handleScreenViewers({ user_id: 'participant', channel_id: 'current-room', viewers: ['viewer'] })
    voice.handleVoiceStateUpdate({ action: 'leave', channel_id: 'current-room', user_id: 'participant' })
    expect(voice.channelUsers['current-room']).toEqual({})
    expect(voice.speakingUsers.participant).toBeUndefined()
    expect(voice.userVideoStreams.participant).toBeUndefined()
    expect(voice.mediaState.participant).toBeUndefined()
    expect(voice.watchedScreens.participant).toBeUndefined()
    expect(voice.remoteScreenStream).toBeNull()
    expect(voice.viewersOf('participant')).toEqual([])
  })
})

describe('layers of a screen share I watch', () => {
  const layers = [{ width: 1920, height: 1080, fps: 60 }, { width: 1280, height: 720, fps: 30 }, { width: 640, height: 360, fps: 30 }]
  function watching(sharer = 'anna') {
    const voice = useVoiceStore()
    voice.setChannel('v1')
    const sent: unknown[] = []
    voice.setSubscriptionSink(msg => { sent.push(msg) })
    voice.handleMediaState({ channel_id: 'v1', user_id: sharer, screen: true, camera: false })
    voice.watchScreen(sharer)
    sent.length = 0
    return { voice, sent }
  }

  it('keeps the layers a sharer declared for the room I am in', () => {
    const { voice } = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    expect(voice.screenLayers).toEqual({ anna: layers })
    // Other rooms, no room, broken events and an empty list change nothing / end it.
    voice.handleScreenLayers({ channel_id: 'v2', user_id: 'ben', layers })
    Reflect.apply(voice.handleScreenLayers, voice, [{ channel_id: 'v1', layers }])
    Reflect.apply(voice.handleScreenLayers, voice, [null])
    expect(Object.keys(voice.screenLayers)).toEqual(['anna'])
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: [] })
    expect(voice.screenLayers).toEqual({})
    voice.disconnect()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    expect(voice.screenLayers).toEqual({})
  })

  it('tells the server my choice, from the first picture on and after a reconnect', () => {
    const { voice, sent } = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    voice.setScreenChoice('anna', 2)
    expect(sent).toEqual([{ kind: 'screen', user_id: 'anna', on: true, quality: 2 }])
    expect(voice.screenChoice).toEqual({ anna: 2 })

    sent.length = 0
    voice.resendSubscriptions()
    expect(sent).toEqual([{ kind: 'screen', user_id: 'anna', on: true, quality: 2 }])

    voice.setScreenChoice('anna', 'auto')
    expect(sent.at(-1)).toEqual({ kind: 'screen', user_id: 'anna', on: true, quality: 'auto' })
    // Nothing to choose on a share I do not watch, or without a user.
    sent.length = 0
    voice.setScreenChoice('ben', 1)
    voice.setScreenChoice(null, 1)
    expect(sent).toEqual([])
  })

  it('does not send a quality before I chose one', () => {
    const { voice, sent } = watching()
    voice.watchScreen('anna')
    expect(sent).toEqual([{ kind: 'screen', user_id: 'anna', on: true }])
  })

  it('shows what I receive, only for a share I watch in my room', () => {
    const { voice } = watching()
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 1, auto: true })
    expect(voice.receivedLayer).toEqual({ anna: { layer: 1, auto: true } })
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'ben', layer: 0, auto: true })
    voice.handleScreenQuality({ channel_id: 'v2', user_id: 'anna', layer: 0, auto: true })
    Reflect.apply(voice.handleScreenQuality, voice, [undefined])
    expect(voice.receivedLayer).toEqual({ anna: { layer: 1, auto: true } })
  })

  it('forgets my choice when the sharer changes the layers, and goes back to automatic', () => {
    const { voice, sent } = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    voice.setScreenChoice('anna', 2)
    sent.length = 0
    // The same layers again: the choice stays.
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: [...layers] })
    expect(voice.screenChoice).toEqual({ anna: 2 })
    expect(sent).toEqual([])
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: layers.slice(0, 2) })
    expect(voice.screenChoice).toEqual({})
    expect(sent).toEqual([{ kind: 'screen', user_id: 'anna', on: true, quality: 'auto' }])
    // Without a choice nothing is sent for changed layers.
    sent.length = 0
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    expect(sent).toEqual([])
  })

  it('keeps my choice when only the sizes change, as for a resized window, and shows the new labels', () => {
    const { voice, sent } = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    voice.setScreenChoice('anna', 1)
    sent.length = 0
    const resized = layers.map(l => ({ ...l, width: l.width - 320, height: l.height - 180 }))
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: resized })
    expect(voice.screenChoice).toEqual({ anna: 1 })
    expect(voice.screenLayers.anna).toEqual(resized)
    expect(sent).toEqual([])
    // Another frame rate is another offer.
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: resized.map(l => ({ ...l, fps: 24 })) })
    expect(voice.screenChoice).toEqual({})
  })

  it('keeps the layers when I stop watching but drops choice and reception', () => {
    const { voice } = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    voice.setScreenChoice('anna', 1)
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 1, auto: false })
    voice.unwatchScreen('anna')
    expect(voice.screenLayers).toEqual({ anna: layers })
    expect(voice.screenChoice).toEqual({})
    expect(voice.receivedLayer).toEqual({})
  })

  it('forgets everything about a share that ended, a room I left and a restarted connection', () => {
    const { voice } = watching()
    const fill = () => {
      voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
      voice.setScreenChoice('anna', 1)
      voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 1, auto: false })
    }
    fill()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: false, camera: false })
    expect([voice.screenLayers, voice.screenChoice, voice.receivedLayer]).toEqual([{}, {}, {}])

    voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: true, camera: false })
    voice.watchScreen('anna')
    fill()
    voice.resetRemoteMedia()
    // The choice stays for a share I still watch: it is sent again after the reconnect.
    expect([voice.screenLayers, voice.screenChoice, voice.receivedLayer]).toEqual([{}, { anna: 1 }, {}])

    voice.watchScreen('anna')
    fill()
    voice.setChannel('v1')
    expect(voice.screenChoice).toEqual({ anna: 1 }) // the same room
    voice.setChannel('v2')
    expect([voice.screenLayers, voice.screenChoice, voice.receivedLayer]).toEqual([{}, {}, {}])

    voice.setChannel('v1')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: true, camera: false })
    voice.watchScreen('anna')
    fill()
    voice.disconnect()
    expect([voice.screenLayers, voice.screenChoice, voice.receivedLayer]).toEqual([{}, {}, {}])
  })

  it('learns which layers of my own share are needed, for my room only', () => {
    const voice = useVoiceStore()
    voice.setChannel('v1')
    expect(voice.layersWanted).toBeNull()
    voice.handleLayersWanted({ channel_id: 'v1', layers: [0, 2] })
    expect(voice.layersWanted).toEqual([0, 2])
    voice.handleLayersWanted({ channel_id: 'v2', layers: [1] })
    Reflect.apply(voice.handleLayersWanted, voice, [undefined])
    expect(voice.layersWanted).toEqual([0, 2])
    voice.resetRemoteMedia()
    expect(voice.layersWanted).toBeNull()
    voice.handleLayersWanted({ channel_id: 'v1', layers: [1] })
    voice.setChannel('v2')
    expect(voice.layersWanted).toBeNull()
    voice.handleLayersWanted({ channel_id: 'v2', layers: [1] })
    voice.disconnect()
    expect(voice.layersWanted).toBeNull()
  })

  it('arrives through the chat store WebSocket switch', async () => {
    const { useChatStore } = await import('./chat')
    const { voice } = watching()
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'screen_layers', payload: { channel_id: 'v1', user_id: 'anna', layers } })
    chat.handleWSEvent({ type: 'screen_quality', payload: { channel_id: 'v1', user_id: 'anna', layer: 0, auto: true } })
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'v1', layers: [0, 1] } })
    chat.handleWSEvent({ type: 'server_info', payload: { version: '0.8.0', screen_layers: 3 } })
    expect(voice.screenLayers).toEqual({ anna: layers })
    expect(voice.receivedLayer).toEqual({ anna: { layer: 0, auto: true } })
    expect(voice.layersWanted).toEqual([0, 1])
    expect(voice.serverScreenLayers).toBe(3)
    chat.handleWSEvent({ type: 'server_info', payload: { version: '0.7.0' } })
    expect(voice.serverScreenLayers).toBe(0)
  })
})
