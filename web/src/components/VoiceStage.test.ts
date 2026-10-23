import type { VueWrapper } from '@vue/test-utils'
import type { ComponentPublicInstance } from 'vue'
import type { VoiceUser } from '../types/domain'
import { requireValue } from '../test-fixtures.fixture'
import { userFixture, categoryFixture, channelFixture, voiceUserFixture, readStateFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale, t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import VoiceStage from './VoiceStage.vue'

const rtc = vi.hoisted(() => ({
  joinVoiceChannel: vi.fn(),
  leaveVoiceChannel: vi.fn(),
  startScreenShare: vi.fn(),
  stopScreenShare: vi.fn(),
  applyAudioSettings: vi.fn(),
  toggleCamera: vi.fn()
}))
vi.mock('../composables/useWebRTC', () => ({ useWebRTC: () => rtc }))

const getUserMedia = vi.fn()

function seed() {
  useAuthStore().user = userFixture({ id: 'me', username: 'me', display_name: 'Me', role: 'user' })
  const chat = useChatStore()
  chat.categories = [categoryFixture({ id: 'cat', name: 'Room', channels: [
    channelFixture({ id: 'v1', name: 'Lounge', type: 'voice' }),
    channelFixture({ id: 'v2', name: 'Other', type: 'voice' })
  ] })]
  const voice = useVoiceStore()
  voice.channelUsers = { v1: { a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }) } }
  return { chat, voice }
}

// Unmounted after each test: a mounted stage listens on the document (F key).
let mounted: VueWrapper<ComponentPublicInstance>[] = []
function mountStage(props = {}) {
  const w = mount(VoiceStage, { props, attachTo: document.body })
  mounted.push(w)
  return w
}

beforeEach(() => {
  // Camera choices are remembered in localStorage; each test starts clean.
  localStorage.clear()
  setLocale('en')
  setActivePinia(createPinia())
  Object.values(rtc).forEach(fn => fn.mockReset())
  getUserMedia.mockReset()
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve([]) })))
})
afterEach(() => {
  for (const w of mounted) {
    try { w.unmount() } catch { /* already unmounted */ }
  }
  mounted = []
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('VoiceStage preview', () => {
  it('shows the channel and who is in it without touching the microphone', async () => {
    seed()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.text()).toContain('Lounge')
    expect(w.text()).toContain('Alice')
    expect(w.text()).toContain('Join')
    // No control dock before joining.
    expect(w.text()).not.toContain('Leave')
    expect(getUserMedia).not.toHaveBeenCalled()
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
  })

  it('Join emits and joins that channel', async () => {
    seed()
    const w = mountStage({ channelId: 'v1' })
    const join = w.findAll<HTMLElement>('button').find(b => b.text().includes('Join'))
    await requireValue(join).trigger('click')
    expect(w.emitted('join')).toEqual([['v1']])
    expect(rtc.joinVoiceChannel).toHaveBeenCalledWith('v1')
  })

  it('previewing another channel while connected still offers Join', async () => {
    const { voice } = seed()
    voice.setChannel('v2')
    const w = mountStage({ channelId: 'v1' })
    expect(w.text()).toContain('Lounge')
    expect(w.text()).toContain('Join')
    expect(w.text()).not.toContain('Leave')
  })

  it('shows the dock once connected to the shown channel', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    const w = mountStage({ channelId: 'v1' })
    expect(w.text()).toContain('Leave')
    expect(w.text()).not.toContain('Join')
  })

  it('falls back to the connected channel without a channelId prop', () => {
    const { voice } = seed()
    voice.setChannel('v1')
    const w = mountStage()
    expect(w.text()).toContain('Lounge')
    expect(w.text()).toContain('Leave')
  })

  it('the chat button in the header opens and closes the chat (it lives under the stage, not in it)', async () => {
    seed()
    const w = mountStage({ channelId: 'v1', showChat: false })
    const toggle = w.get<HTMLElement>('[data-testid="voice-chat-toggle"]')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    expect(toggle.attributes('aria-label')).toBe('Open chat')
    await toggle.trigger('click')
    expect(w.emitted('update:showChat')).toEqual([[true]])

    await w.setProps({ showChat: true })
    expect(toggle.attributes('aria-pressed')).toBe('true')
    expect(toggle.attributes('aria-label')).toBe('Close chat')
    await toggle.trigger('click')
    expect(requireValue(requireValue(w.emitted('update:showChat'))[1])).toEqual([false])
    // VoiceChatPanel renders the chat; the stage has no composer of its own.
    expect(w.find<HTMLTextAreaElement>('textarea').exists()).toBe(false)
    expect(w.find<HTMLInputElement>('input[type="file"]').exists()).toBe(false)
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
  })

  it('shows the unread count of the closed chat on the chat button', async () => {
    const { chat } = seed()
    const w = mountStage({ channelId: 'v1', showChat: false })
    expect(w.find<HTMLElement>('[data-testid="voice-chat-unread"]').exists()).toBe(false)

    chat.readStates = { v1: readStateFixture({ channel_id: 'v1', unread_count: 3, mention_count: 0 }) }
    await nextTick()
    expect(w.get<HTMLElement>('[data-testid="voice-chat-unread"]').text()).toBe('3')
    expect(w.get<HTMLElement>('[data-testid="voice-chat-toggle"]').attributes('aria-label')).toBe('Open chat, 3 unread')

    chat.readStates = { v1: readStateFixture({ channel_id: 'v1', unread_count: 120, mention_count: 0 }) }
    await nextTick()
    expect(w.get<HTMLElement>('[data-testid="voice-chat-unread"]').text()).toBe('99+')

    // Other channels' unreads don't count; an open chat shows no badge.
    chat.readStates = { v2: readStateFixture({ channel_id: 'v2', unread_count: 5, mention_count: 0 }) }
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="voice-chat-unread"]').exists()).toBe(false)
    chat.readStates = { v1: readStateFixture({ channel_id: 'v1', unread_count: 2, mention_count: 0 }) }
    await w.setProps({ showChat: true })
    expect(w.find<HTMLElement>('[data-testid="voice-chat-unread"]').exists()).toBe(false)
  })

  it('the stage keeps its full height with the chat open', async () => {
    const { voice } = seed()
    voice.isConnected = true
    voice.currentChannelId = 'v1'
    const w = mountStage({ channelId: 'v1', showChat: true })
    await nextTick()
    const html = w.html()
    expect(html).not.toContain('h-80')
    expect(html).not.toContain('h-44')
  })
})

describe('VoiceStage participants', () => {
  it('renders a camera for participants who have one and an avatar for the rest', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    voice.setUserVideoStream('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const videos = w.findAll<HTMLVideoElement>('video')
    expect(videos).toHaveLength(1)
    expect(requireValue(videos[0]).attributes('aria-label')).toBe('Camera of Alice')
    expect(w.text()).toContain('Bob')
  })

  it('the camera button toggles the camera', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    const w = mountStage({ channelId: 'v1' })
    await w.find<HTMLElement>('button[aria-label="Turn on camera"]').trigger('click')
    expect(rtc.toggleCamera).toHaveBeenCalled()
  })
})

describe('VoiceStage screen share opt-in', () => {
  function connected() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    return voice
  }

  it('shows a card instead of the stream until the viewer opts in', async () => {
    const voice = connected()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const cards = w.findAll<HTMLElement>('[data-testid="screen-card"]')
    expect(cards).toHaveLength(1)
    expect(requireValue(cards[0]).text()).toContain('Alice is sharing their screen')
    expect(requireValue(cards[0]).text()).toContain('Watch')
    // Nothing on the stage yet.
    expect(w.find<HTMLVideoElement>('video').exists()).toBe(false)
  })

  it('no card for someone who does not share', async () => {
    connected()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="screen-card"]').exists()).toBe(false)
  })

  it('clicking Watch subscribes and the stream goes on the stage', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find<HTMLElement>('[data-testid="screen-card"] [data-testid="screen-card-action"]').trigger('click')
    expect(sink).toHaveBeenCalledWith({ kind: 'screen', user_id: 'a', on: true })
    expect(voice.watchedScreens).toEqual({ a: true })
    // Waiting for the media to arrive.
    expect(w.find<HTMLElement>('[data-testid="screen-card"]').text()).toContain('Connecting')

    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="screen-card"]').exists()).toBe(false)
    expect(w.text()).toContain('Alice')
    expect(w.find<HTMLVideoElement>('video').exists()).toBe(true)
  })

  it('Stop watching on the stage unsubscribes', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find<HTMLElement>('button[aria-label="Stop watching"]').trigger('click')
    expect(sink).toHaveBeenLastCalledWith({ kind: 'screen', user_id: 'a', on: false })
    expect(voice.watchedScreens).toEqual({})
    // The share goes on: the card is back.
    expect(w.find<HTMLElement>('[data-testid="screen-card"]').exists()).toBe(true)
  })

  it('a second share stays a card and can be put on the stage', async () => {
    const voice = connected()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.handleMediaState({ channel_id: 'v1', user_id: 'b', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    voice.watchScreen('b')
    voice.setRemoteScreen('b', new MediaStream())
    voice.focusScreen('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const cards = w.findAll<HTMLElement>('[data-testid="screen-card"]')
    expect(cards).toHaveLength(1)
    expect(requireValue(cards[0]).text()).toContain('Bob')
    await requireValue(cards[0]).find<HTMLElement>('button').trigger('click')
    expect(voice.remoteScreenUserId).toBe('b')
  })

  it('the all-cameras toggle and a tile button hide cameras', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find<HTMLElement>('button[aria-label="Hide camera"]').trigger('click')
    expect(sink).toHaveBeenCalledWith({ kind: 'camera', user_id: 'a', on: false })
    await w.find<HTMLElement>('button[aria-label="Hide all other cameras"]').trigger('click')
    expect(sink).toHaveBeenLastCalledWith({ kind: 'camera', all: true, on: false })
    expect(voice.allCamerasOff).toBe(true)
  })

  it('renders LIVE badge on participant tile and provides stream audio controls', async () => {
    const voice = connected()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()

    // ParticipantTile shows LIVE badge
    expect(w.find<HTMLElement>('[data-testid="tile-live-badge"]').exists()).toBe(true)

    // Watch stream
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()

    // Viewer audio controls exist on the stage
    expect(w.find<HTMLElement>('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(true)
    expect(w.find<HTMLInputElement>('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(true)

    // The slider is the stream's sound, 0..100 %, starting at half volume.
    const slider = w.find<HTMLInputElement>('[data-testid="viewer-stream-volume-slider"]')
    expect(slider.attributes('max')).toBe('100')
    expect(slider.element.value).toBe('50')
    await slider.setValue('30')
    expect(voice.getStreamVolume('a')).toBe(30)
    // ... never the person's voice.
    expect(voice.getUserVolume('a')).toBe(100)
    expect(JSON.parse(requireValue(localStorage.getItem('mnema_stream_volumes')))).toEqual({ a: 30 })

    // The mute button mutes only the stream's sound, and shows 0 meanwhile.
    const mute = w.find<HTMLElement>('[data-testid="viewer-stream-audio-mute"]')
    await mute.trigger('click')
    expect(voice.isStreamMuted('a')).toBe(true)
    expect(voice.isUserLocalMuted('a')).toBe(false)
    expect(mute.attributes('aria-pressed')).toBe('true')
    expect(slider.element.value).toBe('0')
    await mute.trigger('click')
    expect(voice.isStreamMuted('a')).toBe(false)
    expect(slider.element.value).toBe('30')
  })

  it('offers the quality of someone’s screen share once it has layers, not for my own share', async () => {
    const voice = connected()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()
    // One encoding: nothing to choose.
    expect(w.find('[data-testid="screen-layer-button"]').exists()).toBe(false)
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'a', layers: [{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }] })
    await nextTick()
    const button = w.get('[data-testid="stage"] [data-testid="screen-layer-button"]')
    expect(button.text()).toBe(t('talk.layers.auto'))
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.setScreenChoice('a', 1)
    await nextTick()
    expect(sink).toHaveBeenLastCalledWith({ kind: 'screen', user_id: 'a', on: true, quality: 1 })
    expect(button.text()).toBe('720p')
    // My own share has the streamer's gear instead.
    voice.unwatchScreen('a')
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    await nextTick()
    expect(w.find('[data-testid="stage"] [data-testid="screen-layer-button"]').exists()).toBe(false)
  })

  it('provides streamer audio toggle when sharing own screen', async () => {
    const voice = connected()
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    const w = mountStage({ channelId: 'v1' })
    await nextTick()

    const toggle = w.find<HTMLElement>('[data-testid="streamer-audio-toggle"]')
    expect(toggle.exists()).toBe(true)
    expect(voice.isScreenAudioMuted).toBe(false)
    await toggle.trigger('click')
    expect(voice.isScreenAudioMuted).toBe(true)
    // The gear with the stream quality, on my own share only.
    expect(w.find<HTMLElement>('[data-testid="stage"] [data-testid="stream-quality-button"]').exists()).toBe(true)
    expect(w.find<HTMLInputElement>('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(false)
  })

  it('while I share and watch someone, the cards switch the stage both ways', async () => {
    const voice = connected()
    const own = new MediaStream()
    const alice = new MediaStream()
    voice.localScreenStream = own
    voice.isScreenSharing = true
    voice.handleMediaState({ channel_id: 'v1', user_id: 'me', screen: true })
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const stageVideo = () => w.find<HTMLVideoElement>('video').element
    const cardKeys = () => w.findAll<HTMLElement>('[data-testid="screen-card"]').map(c => c.attributes('data-screen-card'))

    // My own share is on the stage, Alice's is offered.
    expect(w.find<HTMLElement>('[data-testid="streamer-audio-toggle"]').exists()).toBe(true)
    expect(cardKeys()).toEqual(['a'])

    // Watching Alice puts her share on the stage once it arrives; mine is a card.
    await w.find<HTMLElement>('[data-screen-card="a"] [data-testid="screen-card-action"]').trigger('click')
    voice.setRemoteScreen('a', alice)
    await flushPromises()
    expect(cardKeys()).toEqual(['own'])
    expect(w.find<HTMLElement>('[data-screen-card="own"]').text()).toContain('Your screen')
    // Its quality stays reachable from the card; the stage has no gear for Alice.
    expect(w.find<HTMLElement>('[data-screen-card="own"] [data-testid="stream-quality-button"]').exists()).toBe(true)
    expect(w.find<HTMLElement>('[data-testid="stage"] [data-testid="stream-quality-button"]').exists()).toBe(false)
    expect(stageVideo().srcObject).toBe(alice)
    // The dedicated screen-audio sink plays the sound, never the stage video.
    expect(stageVideo().muted).toBe(true)
    expect(w.text()).toContain('Alice')
    expect(w.find<HTMLElement>('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(true)
    expect(w.find<HTMLElement>('[data-testid="streamer-audio-toggle"]').exists()).toBe(false)
    expect(w.find<HTMLElement>('button[aria-label="Stop watching"]').exists()).toBe(true)

    // Back to my own share: Alice's stays received, as a card.
    await w.find<HTMLElement>('[data-screen-card="own"] [data-testid="screen-card-action"]').trigger('click')
    await flushPromises()
    expect(stageVideo().srcObject).toBe(own)
    expect(stageVideo().muted).toBe(true)
    expect(cardKeys()).toEqual(['a'])
    expect(w.find<HTMLElement>('[data-screen-card="a"] [data-testid="screen-card-action"]').text()).toBe('Show on stage')
    expect(w.find<HTMLElement>('[data-testid="streamer-audio-toggle"]').exists()).toBe(true)
    expect(w.find<HTMLElement>('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(false)
    expect(voice.watchedScreens).toEqual({ a: true })

    // And to Alice again.
    await w.find<HTMLElement>('[data-screen-card="a"] [data-testid="screen-card-action"]').trigger('click')
    await flushPromises()
    expect(stageVideo().srcObject).toBe(alice)
    expect(cardKeys()).toEqual(['own'])
  })

  it('starting to share while watching someone keeps their share on the stage', async () => {
    const voice = connected()
    const alice = new MediaStream()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', alice)
    const w = mountStage({ channelId: 'v1' })
    await flushPromises()
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    await flushPromises()
    expect(w.find<HTMLVideoElement>('video').element.srcObject).toBe(alice)
    expect(w.findAll<HTMLElement>('[data-testid="screen-card"]').map(c => c.attributes('data-screen-card'))).toEqual(['own'])
  })

  it('a click on a camera tile puts it on the stage; switching to a screen and back to the grid', async () => {
    const voice = connected()
    const cam = new MediaStream()
    const screen = new MediaStream()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.handleMediaState({ channel_id: 'v1', user_id: 'b', screen: true })
    voice.setUserVideoStream('a', cam)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="stage"]').exists()).toBe(false)

    const aliceTile = () => w.find<HTMLElement>('[data-participant-tile][aria-label="Enlarge camera of Alice"]')
    await aliceTile().trigger('click')
    await flushPromises()
    const stageEl = w.find<HTMLElement>('[data-testid="stage"]')
    expect(stageEl.attributes('data-stage-source')).toBe('camera:a')
    expect(w.find<HTMLVideoElement>('[data-testid="stage"] video').element.srcObject).toBe(cam)
    expect(w.find<HTMLVideoElement>('[data-testid="stage"] video').element.muted).toBe(true)
    expect(stageEl.text()).toContain('Alice')
    // No stream audio, LIVE badge or Stop watching for a camera.
    expect(stageEl.text()).not.toContain('LIVE')
    expect(w.find<HTMLInputElement>('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(false)
    expect(w.find<HTMLElement>('[data-testid="streamer-audio-toggle"]').exists()).toBe(false)
    expect(w.find<HTMLElement>('button[aria-label="Stop watching"]').exists()).toBe(false)
    // The tile shows that its camera is on the stage.
    expect(w.find<HTMLElement>('[data-participant-tile][aria-pressed="true"]').attributes('aria-label')).toBe('Back to everyone')

    // Bob's screen goes on the stage.
    await w.find<HTMLElement>('[data-screen-card="b"] [data-testid="screen-card-action"]').trigger('click')
    voice.setRemoteScreen('b', screen)
    await flushPromises()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('b')
    expect(w.find<HTMLInputElement>('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(true)

    // Back to Alice's camera, then off the stage: Bob's screen returns.
    await aliceTile().trigger('click')
    await flushPromises()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:a')
    expect(w.find<HTMLElement>('[data-screen-card="b"] [data-testid="screen-card-action"]').text()).toBe('Show on stage')
    await w.find<HTMLElement>('[data-testid="stage-unfocus-camera"]').trigger('click')
    await flushPromises()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('b')

    // Without a screen share, unfocusing (a second click) returns to the grid.
    voice.unwatchScreen('b')
    await aliceTile().trigger('click')
    await flushPromises()
    await w.find<HTMLElement>('[data-participant-tile][aria-pressed="true"]').trigger('click')
    await flushPromises()
    expect(w.find<HTMLElement>('[data-testid="stage"]').exists()).toBe(false)
  })

  it('an avatar tile still opens the profile', async () => {
    const voice = connected()
    const open = vi.spyOn(useChatStore(), 'openUserProfile').mockImplementation(async () => {})
    voice.setUserVideoStream('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find<HTMLElement>(`[data-participant-tile][aria-label="Open Bob’s profile"]`).trigger('click')
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: 'b' }))
    expect(voice.focusedCamera).toBeNull()
  })

  it('my own camera on the stage is mirrored', async () => {
    const voice = connected()
    voice.localCameraStream = new MediaStream()
    voice.focusCamera('me')
    const w = mountStage({ channelId: 'v1' })
    await flushPromises()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:me')
    expect(w.find<HTMLVideoElement>('[data-testid="stage"] video').classes()).toContain('-scale-x-100')
    expect(w.find<HTMLElement>('[data-testid="stage"]').text()).toContain('Your camera')
  })

  it('the paused own preview only shows while my own share is on the stage', async () => {
    const voice = connected()
    voice.localScreenStream = new MediaStream()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    const hasFocus = vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    try {
      const w = mountStage({ channelId: 'v1' })
      await flushPromises()
      expect(w.find<HTMLElement>('[data-testid="own-stream-paused"]').exists()).toBe(false)
      await w.find<HTMLElement>('[data-screen-card="own"] [data-testid="screen-card-action"]').trigger('click')
      expect(w.find<HTMLElement>('[data-testid="own-stream-paused"]').exists()).toBe(true)
    } finally {
      hasFocus.mockRestore()
    }
  })
})

describe('VoiceStage screen viewers', () => {
  function sharing() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    voice.handleMediaState({ channel_id: 'v1', user_id: 'me', screen: true })
    return voice
  }
  const stageViewers = (w: ReturnType<typeof mount<typeof VoiceStage>>) => w.find<HTMLElement>('[data-testid="stage"] [data-testid="screen-viewers-button"]')

  it('my own share on the stage shows who watches it, 0 before anyone does', async () => {
    const voice = sharing()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(stageViewers(w).text()).toBe('0')
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'me', viewers: ['a', 'b'] })
    await nextTick()
    expect(stageViewers(w).text()).toBe('2')
    expect(stageViewers(w).attributes('aria-label')).toBe('2 viewers: Alice, Bob')
    await stageViewers(w).trigger('click')
    expect(w.find<HTMLElement>('[data-testid="stage"] [data-testid="screen-viewers-list"]').text()).toContain('Alice')
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'me', viewers: ['b'] })
    await nextTick()
    expect(stageViewers(w).text()).toBe('1')
  })

  it("someone else's share on the stage and on a card shows its viewers", async () => {
    const voice = sharing()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'a', viewers: ['b'] })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    // Alice's share is a card (mine is on the stage) and shows its viewer.
    expect(w.find<HTMLElement>('[data-screen-card="a"] [data-testid="screen-viewers-button"]').text()).toBe('1')

    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'a', viewers: ['b', 'me'] })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('a')
    expect(stageViewers(w).text()).toBe('2')
    expect(stageViewers(w).attributes('aria-label')).toBe('2 viewers: Bob, Me')
    // My own share is the card now, still with its count.
    expect(w.find<HTMLElement>('[data-screen-card="own"] [data-testid="screen-viewers-button"]').text()).toBe('0')
  })

  it('a camera on the stage has no viewer count', async () => {
    const voice = sharing()
    voice.localCameraStream = new MediaStream()
    voice.focusCamera('me')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:me')
    expect(stageViewers(w).exists()).toBe(false)
  })
})

describe('VoiceStage grid', () => {
  class TestResizeObserver implements ResizeObserver {
    el: Element | null = null
    constructor(readonly cb: ResizeObserverCallback) { observers.push(this) }
    observe(el: Element) { this.el = el }
    disconnect() { this.el = null }
    unobserve(el: Element) { if (this.el === el) this.el = null }
  }
  let observers: TestResizeObserver[]
  beforeEach(() => {
    observers = []
    vi.stubGlobal('ResizeObserver', TestResizeObserver)
  })
  const resizeGrid = (width: number, height: number) => {
    for (const o of observers) if (o.el) o.cb([{ target: o.el, contentRect: new DOMRect(0, 0, width, height), borderBoxSize: [], contentBoxSize: [], devicePixelContentBoxSize: [] }], o)
    return nextTick()
  }
  function people(n: number) {
    const { voice } = seed()
    voice.setChannel('v1')
    // I am one of the n tiles.
    const users: Record<string, VoiceUser> = {}
    for (let i = 1; i < n; i++) users[`u${i}`] = voiceUserFixture({ id: `u${i}`, username: `user${i}`, display_name: `User ${i}` })
    voice.channelUsers = { v1: users }
    return voice
  }
  const tileWidths = (w: ReturnType<typeof mount<typeof VoiceStage>>) => w.findAll<HTMLElement>('[data-testid="talk-grid"] [data-participant-tile]').map(t => t.element.style.width)

  it('fits the tiles into the area at 16:9 and follows its size', async () => {
    people(4)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const grid = w.get<HTMLElement>('[data-testid="talk-grid"]')
    await resizeGrid(1612, 912)
    // 2x2 with a 12 px gap would be 800 wide: avatar tiles stop at 640.
    expect(grid.attributes('data-grid-cols')).toBe('2')
    expect(tileWidths(w)).toEqual(['640px', '640px', '640px', '640px'])
    expect(grid.find<HTMLElement>('[data-participant-tile]').element.style.height).toBe('360px')

    // A wide, low area: one row.
    await resizeGrid(1600, 220)
    expect(grid.attributes('data-grid-cols')).toBe('4')
    expect(tileWidths(w)[0]).toBe('391px')
    // The row is exactly as wide as its tiles, so it wraps and centers.
    expect(grid.find<HTMLElement>('.flex-wrap').element.style.width).toBe(`${4 * 391 + 3 * 12}px`)
  })

  it('tiles with a camera may grow larger than avatar-only tiles', async () => {
    const voice = people(1)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await resizeGrid(1920, 1080)
    expect(tileWidths(w)).toEqual(['640px'])
    voice.localCameraStream = new MediaStream()
    await nextTick()
    expect(tileWidths(w)).toEqual(['1280px'])
  })

  it('keeps a CSS fallback until the area has a size', async () => {
    people(2)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const tile = w.find<HTMLElement>('[data-testid="talk-grid"] [data-participant-tile]')
    expect(tile.element.style.width).toBe('')
    expect(tile.classes()).toContain('w-56')
    expect(tile.classes()).toContain('aspect-video')
  })
})

