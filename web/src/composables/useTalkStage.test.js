import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { screenCardsFor, useTalkStage } from './useTalkStage'

const me = { id: 'me', username: 'me' }
const alice = { id: 'a', username: 'alice' }
const bob = { id: 'b', username: 'bob' }
const carol = { id: 'c', username: 'carol' }

const summary = cards => cards.map(c => [c.key, c.state])

describe('screenCardsFor', () => {
  const base = {
    users: [me, alice, bob, carol],
    myId: 'me',
    mediaState: { me: { screen: true }, a: { screen: true }, b: { screen: true }, c: { camera: true } },
    watched: { a: true, b: true },
    streams: { a: {} }
  }

  it('lists my own share and every remote share that is not on the stage', () => {
    const cards = screenCardsFor({ ...base, ownStream: {}, stage: { kind: 'screen', own: false, userId: 'a' } })
    expect(summary(cards)).toEqual([['own', 'queued'], ['b', 'pending']])
  })

  it('no own card while my own share is on the stage', () => {
    const cards = screenCardsFor({ ...base, ownStream: {}, stage: { kind: 'screen', own: true, userId: 'me' } })
    expect(summary(cards)).toEqual([['a', 'queued'], ['b', 'pending']])
  })

  it('shares I do not watch are idle; people without a share have no card', () => {
    const cards = screenCardsFor({ ...base, watched: {}, streams: {}, stage: null })
    expect(summary(cards)).toEqual([['a', 'idle'], ['b', 'idle']])
  })
})

describe('useTalkStage', () => {
  let voice
  const users = ref([me, alice])
  const active = ref(true)
  function stage(watch) {
    return useTalkStage({ users: () => users.value, myId: () => 'me', active: () => active.value, watch })
  }

  beforeEach(() => {
    setActivePinia(createPinia())
    voice = useVoiceStore()
    active.value = true
  })

  it('switches the stage both ways through the cards', () => {
    const s = stage()
    voice.localScreenStream = { own: true }
    voice.handleMediaState({ user_id: 'a', screen: true })
    expect(s.ownScreenOnStage.value).toBe(true)
    expect(summary(s.cards.value)).toEqual([['a', 'idle']])

    // Watch: Alice goes on the stage once her media arrives; mine becomes a card.
    s.selectCard(s.cards.value[0])
    expect(voice.watchedScreens).toEqual({ a: true })
    expect(summary(s.cards.value)).toEqual([['a', 'pending']])
    voice.setRemoteScreen('a', { alice: true })
    expect(s.stage.value).toMatchObject({ kind: 'screen', own: false, userId: 'a' })
    expect(summary(s.cards.value)).toEqual([['own', 'queued']])

    s.selectCard(s.cards.value[0])
    expect(s.ownScreenOnStage.value).toBe(true)
    expect(summary(s.cards.value)).toEqual([['a', 'queued']])

    s.selectCard(s.cards.value[0])
    expect(s.stage.value).toMatchObject({ kind: 'screen', own: false, userId: 'a' })
  })

  it('opts in through the given watch function', () => {
    const calls = []
    const s = stage(id => calls.push(id))
    voice.handleMediaState({ user_id: 'a', screen: true })
    s.selectCard(s.cards.value[0])
    expect(calls).toEqual(['a'])
    expect(voice.watchedScreens).toEqual({})
  })

  it('a pending card does nothing', () => {
    const s = stage()
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.localScreenStream = {}
    const card = s.cards.value.find(c => c.key === 'a')
    expect(card.state).toBe('pending')
    s.selectCard(card)
    expect(s.ownScreenOnStage.value).toBe(true)
  })

  it('a camera on the stage leaves every screen share as a card', () => {
    const s = stage()
    voice.localScreenStream = {}
    voice.handleMediaState({ user_id: 'a', screen: true, camera: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', {})
    voice.setUserVideoStream('a', {})
    expect(s.isCameraFocused(alice)).toBe(false)

    s.toggleCamera(alice)
    expect(s.cameraOnStage.value).toBe(true)
    expect(s.isCameraFocused(alice)).toBe(true)
    expect(s.remoteScreenUserId.value).toBeNull()
    expect(s.ownScreenOnStage.value).toBe(false)
    expect(summary(s.cards.value)).toEqual([['own', 'queued'], ['a', 'queued']])

    // A card switches to that screen share; the tile again to the camera.
    s.selectCard(s.cards.value[1])
    expect(s.stage.value).toMatchObject({ kind: 'screen', userId: 'a' })
    s.toggleCamera(alice)
    s.toggleCamera(alice)
    expect(s.stage.value).toMatchObject({ kind: 'screen', userId: 'a' })
  })

  it('cameras are only focusable while connected', () => {
    const s = stage()
    voice.setUserVideoStream('a', {})
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
    voice.localScreenStream = {}
    voice.handleMediaState({ user_id: 'a', screen: true })
    expect(s.stage.value).toBeNull()
    expect(s.cards.value).toEqual([])
  })
})
