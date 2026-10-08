import { required, streamFixture } from '../store-test-support.fixture'
import type { TalkScreenCard } from './useTalkStage'
import type { VoiceStore } from '../stores/voice'
import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { screenCardsFor, useTalkStage } from './useTalkStage'

const me = userFixture({ id: 'me', username: 'me' })
const alice = userFixture({ id: 'a', username: 'alice' })
const bob = userFixture({ id: 'b', username: 'bob' })
const carol = userFixture({ id: 'c', username: 'carol' })

const summary = (cards: TalkScreenCard[]) => cards.map(c => [c.key, c.state])

describe('screenCardsFor', () => {
  const base = {
    users: [me, alice, bob, carol],
    myId: 'me',
    mediaState: { me: { channel_id: 'v1', screen: true, camera: false }, a: { channel_id: 'v1', screen: true, camera: false }, b: { channel_id: 'v1', screen: true, camera: false }, c: { channel_id: 'v1', camera: true, screen: false } },
    watched: { a: true, b: true },
    streams: { a: streamFixture() }
  }

  it('lists my own share and every remote share that is not on the stage', () => {
    const cards = screenCardsFor({ ...base, ownStream: streamFixture(), stage: { kind: 'screen', own: false, userId: 'a', stream: streamFixture() } })
    expect(summary(cards)).toEqual([['own', 'queued'], ['b', 'pending']])
  })

  it('no own card while my own share is on the stage', () => {
    const cards = screenCardsFor({ ...base, ownStream: streamFixture(), stage: { kind: 'screen', own: true, userId: 'me', stream: streamFixture() } })
    expect(summary(cards)).toEqual([['a', 'queued'], ['b', 'pending']])
  })

  it('shares I do not watch are idle; people without a share have no card', () => {
    const cards = screenCardsFor({ ...base, watched: {}, streams: {}, stage: null })
    expect(summary(cards)).toEqual([['a', 'idle'], ['b', 'idle']])
  })
})

describe('useTalkStage', () => {
  let voice: VoiceStore
  const users = ref([me, alice])
  const active = ref(true)
  function stage(watch?: (id: string) => void | Promise<void>) {
    return useTalkStage({ users: () => users.value, myId: () => 'me', active: () => active.value, ...(watch ? { watch } : {}) })
  }

  beforeEach(() => {
    setActivePinia(createPinia())
    voice = useVoiceStore()
    active.value = true
  })

  it('switches the stage both ways through the cards', () => {
    const s = stage()
    voice.localScreenStream = streamFixture()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    expect(s.ownScreenOnStage.value).toBe(true)
    expect(summary(s.cards.value)).toEqual([['a', 'idle']])

    // Watch: Alice goes on the stage once her media arrives; mine becomes a card.
    s.selectCard(required(s.cards.value[0]))
    expect(voice.watchedScreens).toEqual({ a: true })
    expect(summary(s.cards.value)).toEqual([['a', 'pending']])
    voice.setRemoteScreen('a', streamFixture())
    expect(s.stage.value).toMatchObject({ kind: 'screen', own: false, userId: 'a' })
    expect(summary(s.cards.value)).toEqual([['own', 'queued']])

    s.selectCard(required(s.cards.value[0]))
    expect(s.ownScreenOnStage.value).toBe(true)
    expect(summary(s.cards.value)).toEqual([['a', 'queued']])

    s.selectCard(required(s.cards.value[0]))
    expect(s.stage.value).toMatchObject({ kind: 'screen', own: false, userId: 'a' })
  })

  it('opts in through the given watch function', () => {
    const calls: string[] = []
    const s = stage(id => { calls.push(id) })
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    s.selectCard(required(s.cards.value[0]))
    expect(calls).toEqual(['a'])
    expect(voice.watchedScreens).toEqual({})
  })

  it('a pending card does nothing', () => {
    const s = stage()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.localScreenStream = streamFixture()
    const card = required(s.cards.value.find(c => c.key === 'a'))
    expect(card.state).toBe('pending')
    s.selectCard(card)
    expect(s.ownScreenOnStage.value).toBe(true)
  })

  it('a camera on the stage leaves every screen share as a card', () => {
    const s = stage()
    voice.localScreenStream = streamFixture()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true, camera: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', streamFixture())
    voice.setUserVideoStream('a', streamFixture())
    expect(s.isCameraFocused(alice)).toBe(false)

    s.toggleCamera(alice)
    expect(s.cameraOnStage.value).toBe(true)
    expect(s.isCameraFocused(alice)).toBe(true)
    expect(s.remoteScreenUserId.value).toBeNull()
    expect(s.ownScreenOnStage.value).toBe(false)
    expect(summary(s.cards.value)).toEqual([['own', 'queued'], ['a', 'queued']])

    // A card switches to that screen share; the tile again to the camera.
    s.selectCard(required(s.cards.value[1]))
    expect(s.stage.value).toMatchObject({ kind: 'screen', userId: 'a' })
    s.toggleCamera(alice)
    s.toggleCamera(alice)
    expect(s.stage.value).toMatchObject({ kind: 'screen', userId: 'a' })
  })

  it('cameras are only focusable while connected', () => {
    const s = stage()
    voice.setUserVideoStream('a', streamFixture())
    expect(s.canFocusCamera(alice)).toBe(true)
    expect(s.canFocusCamera(bob)).toBe(false)
    active.value = false
    expect(s.canFocusCamera(alice)).toBe(false)
    s.toggleCamera(alice)
    expect(voice.focusedCamera).toBeNull()
  })

  it('a preview has neither stage nor cards', () => {
    const s = stage()
    active.value = false
    voice.localScreenStream = streamFixture()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    expect(s.stage.value).toBeNull()
    expect(s.cards.value).toEqual([])
  })
})

describe('screen card empty and camera stages', () => {
  it('defaults omitted maps and does not invent shares for absent media state', () => {
    expect(screenCardsFor({})).toEqual([])
    expect(screenCardsFor({ users: [alice] })).toEqual([])
    expect(summary(screenCardsFor({ users: [alice], ownStream: streamFixture(), stage: { kind: 'camera', own: false, userId: 'a', stream: streamFixture() } }))).toEqual([['own', 'queued']])
  })
  it('reports the remote screen user on stage and refuses focus without a camera', () => {
    setActivePinia(createPinia())
    const voice = useVoiceStore()
    const s = useTalkStage({ users: () => [alice], myId: () => null, active: () => true })
    s.toggleCamera(alice)
    expect(voice.focusedCamera).toBeNull()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', streamFixture())
    expect(s.remoteScreenUserId.value).toBe('a')
  })
})
