import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import VoiceStage from './VoiceStage.vue'
import ParticipantTile from './ParticipantTile.vue'
import PipHost from './PipHost.vue'
import { pendingConfirm } from '../lib/confirm'

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
  useAuthStore().user = { id: 'me', username: 'me', display_name: 'Me', role: 'user' }
  const chat = useChatStore()
  chat.categories = [{ id: 'cat', name: 'Room', channels: [
    { id: 'v1', name: 'Lounge', type: 'voice' },
    { id: 'v2', name: 'Other', type: 'voice' }
  ] }]
  const voice = useVoiceStore()
  voice.channelUsers = { v1: { a: { id: 'a', username: 'alice', display_name: 'Alice' } } }
  return { chat, voice }
}

// Unmounted after each test: a mounted stage listens on the document (F key).
let mounted = []
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
    const join = w.findAll('button').find(b => b.text().includes('Join'))
    await join.trigger('click')
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
    const toggle = w.get('[data-testid="voice-chat-toggle"]')
    expect(toggle.attributes('aria-pressed')).toBe('false')
    expect(toggle.attributes('aria-label')).toBe('Open chat')
    await toggle.trigger('click')
    expect(w.emitted('update:showChat')).toEqual([[true]])

    await w.setProps({ showChat: true })
    expect(toggle.attributes('aria-pressed')).toBe('true')
    expect(toggle.attributes('aria-label')).toBe('Close chat')
    await toggle.trigger('click')
    expect(w.emitted('update:showChat')[1]).toEqual([false])
    // VoiceChatPanel renders the chat; the stage has no composer of its own.
    expect(w.find('textarea').exists()).toBe(false)
    expect(w.find('input[type="file"]').exists()).toBe(false)
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
  })

  it('shows the unread count of the closed chat on the chat button', async () => {
    const { chat } = seed()
    const w = mountStage({ channelId: 'v1', showChat: false })
    expect(w.find('[data-testid="voice-chat-unread"]').exists()).toBe(false)

    chat.readStates = { v1: { channel_id: 'v1', unread_count: 3, mention_count: 0 } }
    await nextTick()
    expect(w.get('[data-testid="voice-chat-unread"]').text()).toBe('3')
    expect(w.get('[data-testid="voice-chat-toggle"]').attributes('aria-label')).toBe('Open chat, 3 unread')

    chat.readStates = { v1: { channel_id: 'v1', unread_count: 120, mention_count: 0 } }
    await nextTick()
    expect(w.get('[data-testid="voice-chat-unread"]').text()).toBe('99+')

    // Other channels' unreads don't count; an open chat shows no badge.
    chat.readStates = { v2: { channel_id: 'v2', unread_count: 5, mention_count: 0 } }
    await nextTick()
    expect(w.find('[data-testid="voice-chat-unread"]').exists()).toBe(false)
    chat.readStates = { v1: { channel_id: 'v1', unread_count: 2, mention_count: 0 } }
    await w.setProps({ showChat: true })
    expect(w.find('[data-testid="voice-chat-unread"]').exists()).toBe(false)
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
      a: { id: 'a', username: 'alice', display_name: 'Alice' },
      b: { id: 'b', username: 'bob', display_name: 'Bob' }
    } }
    voice.setUserVideoStream('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const videos = w.findAll('video')
    expect(videos).toHaveLength(1)
    expect(videos[0].attributes('aria-label')).toBe('Camera of Alice')
    expect(w.text()).toContain('Bob')
  })

  it('the camera button toggles the camera', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    const w = mountStage({ channelId: 'v1' })
    await w.find('button[aria-label="Turn on camera"]').trigger('click')
    expect(rtc.toggleCamera).toHaveBeenCalled()
  })
})

describe('VoiceStage screen share opt-in', () => {
  function connected() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: { id: 'a', username: 'alice', display_name: 'Alice' },
      b: { id: 'b', username: 'bob', display_name: 'Bob' }
    } }
    return voice
  }

  it('shows a card instead of the stream until the viewer opts in', async () => {
    const voice = connected()
    voice.handleMediaState({ user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const cards = w.findAll('[data-testid="screen-card"]')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('Alice is sharing their screen')
    expect(cards[0].text()).toContain('Watch')
    // Nothing on the stage yet.
    expect(w.find('video').exists()).toBe(false)
  })

  it('no card for someone who does not share', async () => {
    connected()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find('[data-testid="screen-card"]').exists()).toBe(false)
  })

  it('clicking Watch subscribes and the stream goes on the stage', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find('[data-testid="screen-card"] [data-testid="screen-card-action"]').trigger('click')
    expect(sink).toHaveBeenCalledWith({ kind: 'screen', user_id: 'a', on: true })
    expect(voice.watchedScreens).toEqual({ a: true })
    // Waiting for the media to arrive.
    expect(w.find('[data-testid="screen-card"]').text()).toContain('Connecting')

    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()
    expect(w.find('[data-testid="screen-card"]').exists()).toBe(false)
    expect(w.text()).toContain('Alice')
    expect(w.find('video').exists()).toBe(true)
  })

  it('Stop watching on the stage unsubscribes', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find('button[aria-label="Stop watching"]').trigger('click')
    expect(sink).toHaveBeenLastCalledWith({ kind: 'screen', user_id: 'a', on: false })
    expect(voice.watchedScreens).toEqual({})
    // The share goes on: the card is back.
    expect(w.find('[data-testid="screen-card"]').exists()).toBe(true)
  })

  it('a second share stays a card and can be put on the stage', async () => {
    const voice = connected()
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.handleMediaState({ user_id: 'b', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    voice.watchScreen('b')
    voice.setRemoteScreen('b', new MediaStream())
    voice.focusScreen('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const cards = w.findAll('[data-testid="screen-card"]')
    expect(cards).toHaveLength(1)
    expect(cards[0].text()).toContain('Bob')
    await cards[0].find('button').trigger('click')
    expect(voice.remoteScreenUserId).toBe('b')
  })

  it('the all-cameras toggle and a tile button hide cameras', async () => {
    const voice = connected()
    const sink = vi.fn()
    voice.setSubscriptionSink(sink)
    voice.handleMediaState({ user_id: 'a', camera: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find('button[aria-label="Hide camera"]').trigger('click')
    expect(sink).toHaveBeenCalledWith({ kind: 'camera', user_id: 'a', on: false })
    await w.find('button[aria-label="Hide all other cameras"]').trigger('click')
    expect(sink).toHaveBeenLastCalledWith({ kind: 'camera', all: true, on: false })
    expect(voice.allCamerasOff).toBe(true)
  })

  it('renders LIVE badge on participant tile and provides stream audio controls', async () => {
    const voice = connected()
    voice.handleMediaState({ user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()

    // ParticipantTile shows LIVE badge
    expect(w.find('[data-testid="tile-live-badge"]').exists()).toBe(true)

    // Watch stream
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()

    // Viewer audio controls exist on the stage
    expect(w.find('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(true)
    expect(w.find('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(true)

    // The slider is the stream's sound, 0..100 %, starting at half volume.
    const slider = w.find('[data-testid="viewer-stream-volume-slider"]')
    expect(slider.attributes('max')).toBe('100')
    expect(slider.element.value).toBe('50')
    await slider.setValue('30')
    expect(voice.getStreamVolume('a')).toBe(30)
    // ... never the person's voice.
    expect(voice.getUserVolume('a')).toBe(100)
    expect(JSON.parse(localStorage.getItem('mnema_stream_volumes'))).toEqual({ a: 30 })

    // The mute button mutes only the stream's sound, and shows 0 meanwhile.
    const mute = w.find('[data-testid="viewer-stream-audio-mute"]')
    await mute.trigger('click')
    expect(voice.isStreamMuted('a')).toBe(true)
    expect(voice.isUserLocalMuted('a')).toBe(false)
    expect(mute.attributes('aria-pressed')).toBe('true')
    expect(slider.element.value).toBe('0')
    await mute.trigger('click')
    expect(voice.isStreamMuted('a')).toBe(false)
    expect(slider.element.value).toBe('30')
  })

  it('provides streamer audio toggle when sharing own screen', async () => {
    const voice = connected()
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    const w = mountStage({ channelId: 'v1' })
    await nextTick()

    const toggle = w.find('[data-testid="streamer-audio-toggle"]')
    expect(toggle.exists()).toBe(true)
    expect(voice.isScreenAudioMuted).toBe(false)
    await toggle.trigger('click')
    expect(voice.isScreenAudioMuted).toBe(true)
    // The gear with the stream quality, on my own share only.
    expect(w.find('[data-testid="stage"] [data-testid="stream-quality-button"]').exists()).toBe(true)
    expect(w.find('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(false)
  })

  it('while I share and watch someone, the cards switch the stage both ways', async () => {
    const voice = connected()
    const own = new MediaStream()
    const alice = new MediaStream()
    voice.localScreenStream = own
    voice.isScreenSharing = true
    voice.handleMediaState({ user_id: 'me', screen: true })
    voice.handleMediaState({ user_id: 'a', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const stageVideo = () => w.find('video').element
    const cardKeys = () => w.findAll('[data-testid="screen-card"]').map(c => c.attributes('data-screen-card'))

    // My own share is on the stage, Alice's is offered.
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(true)
    expect(cardKeys()).toEqual(['a'])

    // Watching Alice puts her share on the stage once it arrives; mine is a card.
    await w.find('[data-screen-card="a"] [data-testid="screen-card-action"]').trigger('click')
    voice.setRemoteScreen('a', alice)
    await flushPromises()
    expect(cardKeys()).toEqual(['own'])
    expect(w.find('[data-screen-card="own"]').text()).toContain('Your screen')
    // Its quality stays reachable from the card; the stage has no gear for Alice.
    expect(w.find('[data-screen-card="own"] [data-testid="stream-quality-button"]').exists()).toBe(true)
    expect(w.find('[data-testid="stage"] [data-testid="stream-quality-button"]').exists()).toBe(false)
    expect(stageVideo().srcObject).toBe(alice)
    expect(stageVideo().muted).toBe(false)
    expect(w.text()).toContain('Alice')
    expect(w.find('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(true)
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Stop watching"]').exists()).toBe(true)

    // Back to my own share: Alice's stays received, as a card.
    await w.find('[data-screen-card="own"] [data-testid="screen-card-action"]').trigger('click')
    await flushPromises()
    expect(stageVideo().srcObject).toBe(own)
    expect(stageVideo().muted).toBe(true)
    expect(cardKeys()).toEqual(['a'])
    expect(w.find('[data-screen-card="a"] [data-testid="screen-card-action"]').text()).toBe('Show on stage')
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(true)
    expect(w.find('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(false)
    expect(voice.watchedScreens).toEqual({ a: true })

    // And to Alice again.
    await w.find('[data-screen-card="a"] [data-testid="screen-card-action"]').trigger('click')
    await flushPromises()
    expect(stageVideo().srcObject).toBe(alice)
    expect(cardKeys()).toEqual(['own'])
  })

  it('starting to share while watching someone keeps their share on the stage', async () => {
    const voice = connected()
    const alice = new MediaStream()
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', alice)
    const w = mountStage({ channelId: 'v1' })
    await flushPromises()
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    await flushPromises()
    expect(w.find('video').element.srcObject).toBe(alice)
    expect(w.findAll('[data-testid="screen-card"]').map(c => c.attributes('data-screen-card'))).toEqual(['own'])
  })

  it('a click on a camera tile puts it on the stage; switching to a screen and back to the grid', async () => {
    const voice = connected()
    const cam = new MediaStream()
    const screen = new MediaStream()
    voice.handleMediaState({ user_id: 'a', camera: true })
    voice.handleMediaState({ user_id: 'b', screen: true })
    voice.setUserVideoStream('a', cam)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find('[data-testid="stage"]').exists()).toBe(false)

    const aliceTile = () => w.find('[data-participant-tile][aria-label="Enlarge camera of Alice"]')
    await aliceTile().trigger('click')
    await flushPromises()
    const stageEl = w.find('[data-testid="stage"]')
    expect(stageEl.attributes('data-stage-source')).toBe('camera:a')
    expect(w.find('[data-testid="stage"] video').element.srcObject).toBe(cam)
    expect(w.find('[data-testid="stage"] video').element.muted).toBe(true)
    expect(stageEl.text()).toContain('Alice')
    // No stream audio, LIVE badge or Stop watching for a camera.
    expect(stageEl.text()).not.toContain('LIVE')
    expect(w.find('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(false)
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Stop watching"]').exists()).toBe(false)
    // The tile shows that its camera is on the stage.
    expect(w.find('[data-participant-tile][aria-pressed="true"]').attributes('aria-label')).toBe('Back to everyone')

    // Bob's screen goes on the stage.
    await w.find('[data-screen-card="b"] [data-testid="screen-card-action"]').trigger('click')
    voice.setRemoteScreen('b', screen)
    await flushPromises()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('b')
    expect(w.find('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(true)

    // Back to Alice's camera, then off the stage: Bob's screen returns.
    await aliceTile().trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:a')
    expect(w.find('[data-screen-card="b"] [data-testid="screen-card-action"]').text()).toBe('Show on stage')
    await w.find('[data-testid="stage-unfocus-camera"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('b')

    // Without a screen share, unfocusing (a second click) returns to the grid.
    voice.unwatchScreen('b')
    await aliceTile().trigger('click')
    await flushPromises()
    await w.find('[data-participant-tile][aria-pressed="true"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="stage"]').exists()).toBe(false)
  })

  it('an avatar tile still opens the profile', async () => {
    const voice = connected()
    const open = vi.spyOn(useChatStore(), 'openUserProfile').mockImplementation(() => {})
    voice.setUserVideoStream('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.find(`[data-participant-tile][aria-label="Open Bob's profile"]`).trigger('click')
    expect(open).toHaveBeenCalledWith(expect.objectContaining({ id: 'b' }))
    expect(voice.focusedCamera).toBeNull()
  })

  it('my own camera on the stage is mirrored', async () => {
    const voice = connected()
    voice.localCameraStream = new MediaStream()
    voice.focusCamera('me')
    const w = mountStage({ channelId: 'v1' })
    await flushPromises()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:me')
    expect(w.find('[data-testid="stage"] video').classes()).toContain('-scale-x-100')
    expect(w.find('[data-testid="stage"]').text()).toContain('Your camera')
  })

  it('the paused own preview only shows while my own share is on the stage', async () => {
    const voice = connected()
    voice.localScreenStream = new MediaStream()
    voice.handleMediaState({ user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    const hasFocus = vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    try {
      const w = mountStage({ channelId: 'v1' })
      await flushPromises()
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(false)
      await w.find('[data-screen-card="own"] [data-testid="screen-card-action"]').trigger('click')
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(true)
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
      a: { id: 'a', username: 'alice', display_name: 'Alice' },
      b: { id: 'b', username: 'bob', display_name: 'Bob' }
    } }
    voice.localScreenStream = new MediaStream()
    voice.isScreenSharing = true
    voice.handleMediaState({ channel_id: 'v1', user_id: 'me', screen: true })
    return voice
  }
  const stageViewers = w => w.find('[data-testid="stage"] [data-testid="screen-viewers-button"]')

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
    expect(w.find('[data-testid="stage"] [data-testid="screen-viewers-list"]').text()).toContain('Alice')
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
    expect(w.find('[data-screen-card="a"] [data-testid="screen-viewers-button"]').text()).toBe('1')

    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    voice.handleScreenViewers({ channel_id: 'v1', user_id: 'a', viewers: ['b', 'me'] })
    await nextTick()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('a')
    expect(stageViewers(w).text()).toBe('2')
    expect(stageViewers(w).attributes('aria-label')).toBe('2 viewers: Bob, Me')
    // My own share is the card now, still with its count.
    expect(w.find('[data-screen-card="own"] [data-testid="screen-viewers-button"]').text()).toBe('0')
  })

  it('a camera on the stage has no viewer count', async () => {
    const voice = sharing()
    voice.localCameraStream = new MediaStream()
    voice.focusCamera('me')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:me')
    expect(stageViewers(w).exists()).toBe(false)
  })
})

describe('VoiceStage grid', () => {
  let observers
  beforeEach(() => {
    observers = []
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb) { this.cb = cb; observers.push(this) }
      observe(el) { this.el = el }
      disconnect() { this.el = null }
    })
  })
  const resizeGrid = (width, height) => {
    for (const o of observers) if (o.el) o.cb([{ target: o.el, contentRect: { width, height } }])
    return nextTick()
  }
  function people(n) {
    const { voice } = seed()
    voice.setChannel('v1')
    // I am one of the n tiles.
    const users = {}
    for (let i = 1; i < n; i++) users[`u${i}`] = { id: `u${i}`, username: `user${i}`, display_name: `User ${i}` }
    voice.channelUsers = { v1: users }
    return voice
  }
  const tileWidths = w => w.findAll('[data-testid="talk-grid"] [data-participant-tile]').map(t => t.element.style.width)

  it('fits the tiles into the area at 16:9 and follows its size', async () => {
    people(4)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const grid = w.get('[data-testid="talk-grid"]')
    await resizeGrid(1612, 912)
    // 2x2 with a 12 px gap would be 800 wide: avatar tiles stop at 640.
    expect(grid.attributes('data-grid-cols')).toBe('2')
    expect(tileWidths(w)).toEqual(['640px', '640px', '640px', '640px'])
    expect(grid.find('[data-participant-tile]').element.style.height).toBe('360px')

    // A wide, low area: one row.
    await resizeGrid(1600, 220)
    expect(grid.attributes('data-grid-cols')).toBe('4')
    expect(tileWidths(w)[0]).toBe('391px')
    // The row is exactly as wide as its tiles, so it wraps and centers.
    expect(grid.find('.flex-wrap').element.style.width).toBe(`${4 * 391 + 3 * 12}px`)
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
    const tile = w.find('[data-testid="talk-grid"] [data-participant-tile]')
    expect(tile.element.style.width).toBe('')
    expect(tile.classes()).toContain('w-56')
    expect(tile.classes()).toContain('aspect-video')
  })
})

describe('VoiceStage full screen', () => {
  let fsEl
  let requestFullscreen
  let exitFullscreen
  beforeEach(() => {
    fsEl = null
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => fsEl })
    requestFullscreen = vi.fn(function () {
      fsEl = this
      document.dispatchEvent(new Event('fullscreenchange'))
      return Promise.resolve()
    })
    exitFullscreen = vi.fn(() => {
      fsEl = null
      document.dispatchEvent(new Event('fullscreenchange'))
      return Promise.resolve()
    })
    HTMLElement.prototype.requestFullscreen = requestFullscreen
    document.exitFullscreen = exitFullscreen
  })
  afterEach(() => {
    delete HTMLElement.prototype.requestFullscreen
    delete document.exitFullscreen
    delete document.fullscreenElement
  })

  function withCameras() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: { id: 'a', username: 'alice', display_name: 'Alice' },
      b: { id: 'b', username: 'bob', display_name: 'Bob' }
    } }
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    return voice
  }
  const stageEl = w => w.find('[data-testid="stage"]')
  const tileOf = (w, id) => w.find(`[data-participant-tile][data-user-id="${id}"]`)
  const press = (key, init = {}, target = document.body) => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }))
    return flushPromises()
  }

  it('the button and a double-click on the stage toggle full screen', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const button = w.get('[data-testid="stage-fullscreen"]')
    expect(button.attributes('aria-pressed')).toBe('false')
    await button.trigger('click')
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
    expect(button.attributes('aria-pressed')).toBe('true')
    expect(button.attributes('aria-label')).toBe('Exit full screen')

    await stageEl(w).find('video').trigger('dblclick')
    await flushPromises()
    expect(fsEl).toBe(null)
    await stageEl(w).find('video').trigger('dblclick')
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
  })

  it('a double-click on a stage button does not toggle full screen', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get('[data-testid="stage-unfocus-camera"]').trigger('dblclick')
    await flushPromises()
    expect(requestFullscreen).not.toHaveBeenCalled()
  })

  it('a double-click on a camera tile puts it on the stage in full screen', async () => {
    withCameras()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    // The browser's order: click, click (detail 2), dblclick.
    await tileOf(w, 'a').trigger('click', { detail: 1 })
    expect(stageEl(w).attributes('data-stage-source')).toBe('camera:a')
    // The tiles moved under the stage: the rest lands on the stage.
    await stageEl(w).find('video').trigger('click', { detail: 2 })
    await stageEl(w).find('video').trigger('dblclick')
    await flushPromises()
    expect(stageEl(w).attributes('data-stage-source')).toBe('camera:a')
    expect(fsEl).toBe(stageEl(w).element)
  })

  it('a double-click on a camera already on the stage keeps it there in full screen', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    // The first click takes it off the stage, the dblclick brings it back.
    await tileOf(w, 'a').trigger('click', { detail: 1 })
    expect(stageEl(w).exists()).toBe(false)
    await tileOf(w, 'a').trigger('click', { detail: 2 })
    await tileOf(w, 'a').trigger('dblclick')
    await flushPromises()
    expect(stageEl(w).attributes('data-stage-source')).toBe('camera:a')
    expect(fsEl).toBe(stageEl(w).element)
  })

  it('a dblclick on a camera tile long after a click just goes full screen with it', async () => {
    const voice = withCameras()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await tileOf(w, 'a').trigger('dblclick')
    await flushPromises()
    expect(voice.focusedCamera).toBe('a')
    expect(fsEl).toBe(stageEl(w).element)
  })

  it('F toggles full screen, but not while typing, with modifiers or as push-to-talk key', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()

    await press('f')
    expect(fsEl).toBe(stageEl(w).element)
    await press('F')
    expect(fsEl).toBe(null)

    // Typing in a field of the Talk view (e.g. the stream volume) is not F.
    const input = document.createElement('input')
    w.element.appendChild(input)
    await press('f', {}, input)
    await press('f', { ctrlKey: true })
    await press('f', { repeat: true })
    expect(requestFullscreen).toHaveBeenCalledTimes(1)

    const editable = document.createElement('div')
    editable.setAttribute('contenteditable', 'true')
    document.body.appendChild(editable)
    await press('f', {}, editable)
    expect(requestFullscreen).toHaveBeenCalledTimes(1)

    voice.inputMode = 'ptt'
    voice.pttKey = 'KeyF'
    await press('f', { code: 'KeyF' })
    expect(requestFullscreen).toHaveBeenCalledTimes(1)
  })

  it('F only counts while the focus is in the Talk view and no dialog is open', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    // Focus in the sidebar (outside the Talk view).
    const sidebarButton = document.createElement('button')
    document.body.appendChild(sidebarButton)
    await press('f', {}, sidebarButton)
    expect(requestFullscreen).not.toHaveBeenCalled()

    const dialog = document.createElement('div')
    dialog.setAttribute('aria-modal', 'true')
    document.body.appendChild(dialog)
    await press('f')
    expect(requestFullscreen).not.toHaveBeenCalled()
    dialog.remove()

    // A button of the Talk view is fine.
    await press('f', {}, w.get('[data-testid="stage-fullscreen"]').element)
    expect(requestFullscreen).toHaveBeenCalledTimes(1)
  })

  it('F does nothing without a stage and stops listening when the Talk view goes', async () => {
    withCameras()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await press('f')
    expect(requestFullscreen).not.toHaveBeenCalled()
    w.unmount()
    await press('f')
    expect(requestFullscreen).not.toHaveBeenCalled()
  })

  it('a right-click on the stage offers full screen and taking the camera off', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await stageEl(w).trigger('contextmenu')
    await flushPromises()
    const items = [...document.querySelectorAll('[role="menuitem"]')]
    expect(items.map(i => i.textContent.trim())).toEqual(['Full screenF', 'Back to everyone'])
    items[0].click()
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
  })
})

describe('VoiceStage hide participants without video', () => {
  function room() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: { id: 'a', username: 'alice', display_name: 'Alice' },
      b: { id: 'b', username: 'bob', display_name: 'Bob' },
      c: { id: 'c', username: 'carl', display_name: 'Carl' }
    } }
    return voice
  }
  const tileIds = w => w.findAll('[data-participant-tile]').map(t => t.attributes('data-user-id'))
  const toggle = w => w.get('[data-testid="hide-no-video"]')

  it('hides tiles without camera or screen share, me included, and remembers it', async () => {
    const voice = room()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    voice.handleMediaState({ channel_id: 'v1', user_id: 'b', screen: true })
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(tileIds(w)).toEqual(['me', 'a', 'b', 'c'])
    expect(toggle(w).attributes('aria-pressed')).toBe('false')
    expect(toggle(w).attributes('aria-label')).toBe('Hide participants without video')

    await toggle(w).trigger('click')
    expect(toggle(w).attributes('aria-pressed')).toBe('true')
    expect(tileIds(w)).toEqual(['a', 'b'])
    expect(JSON.parse(localStorage.getItem('mnema_hide_no_video'))).toEqual({ on: true })

    // My camera counts as video too.
    voice.localCameraStream = new MediaStream()
    await nextTick()
    expect(tileIds(w)).toEqual(['me', 'a', 'b'])

    // A camera I hid is no video for me.
    voice.setCameraHidden('a', true)
    await nextTick()
    expect(tileIds(w)).toEqual(['me', 'b'])

    setActivePinia(createPinia())
    expect(useVoiceStore().hideNoVideo).toBe(true)
  })

  it('shows a hint instead of an empty area when nobody has video', async () => {
    const voice = room()
    voice.setHideNoVideo(true)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(tileIds(w)).toEqual([])
    const hint = w.get('[data-testid="no-video-hint"]')
    expect(hint.text()).toContain('Nobody has video on right now')
    await hint.get('button').trigger('click')
    expect(voice.hideNoVideo).toBe(false)
    expect(tileIds(w)).toEqual(['me', 'a', 'b', 'c'])
  })

  it('under a stage only the tiles with video stay in the strip', async () => {
    const voice = room()
    voice.setHideNoVideo(true)
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.get('[data-testid="talk-strip"]').classes()).not.toContain('hidden')
    expect(tileIds(w)).toEqual(['a'])
    voice.setCameraHidden('a', true)
    await nextTick()
    // The camera left the stage and nobody has video: the hint.
    expect(w.find('[data-testid="no-video-hint"]').exists()).toBe(true)
  })

  it('does not apply to the preview of a Talk I am not in', async () => {
    const { voice } = seed()
    voice.setHideNoVideo(true)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find('[data-testid="hide-no-video"]').exists()).toBe(false)
    expect(tileIds(w)).toEqual(['a'])
  })

  it('works when the browser blocks storage', async () => {
    const voice = room()
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    try {
      voice.setHideNoVideo(true)
      expect(voice.hideNoVideo).toBe(true)
    } finally {
      setItem.mockRestore()
    }
  })
})

describe('VoiceStage picture-in-picture', () => {
  let pipElement
  let requestPip
  beforeEach(() => {
    pipElement = null
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, get: () => true })
    Object.defineProperty(document, 'pictureInPictureElement', { configurable: true, get: () => pipElement })
    requestPip = vi.fn(function () {
      pipElement = this
      return Promise.resolve({})
    })
    HTMLVideoElement.prototype.requestPictureInPicture = requestPip
    document.exitPictureInPicture = vi.fn(() => {
      const el = pipElement
      pipElement = null
      el?.dispatchEvent(new Event('leavepictureinpicture'))
      return Promise.resolve()
    })
    Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, get() { return this.srcObject ? 1 : 0 } })
  })
  afterEach(() => {
    delete HTMLVideoElement.prototype.requestPictureInPicture
    delete HTMLVideoElement.prototype.readyState
    delete document.exitPictureInPicture
    delete document.pictureInPictureEnabled
    delete document.pictureInPictureElement
  })

  function cameraOnStage() {
    const { voice } = seed()
    voice.setChannel('v1')
    const stream = new MediaStream()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', stream)
    voice.focusCamera('a')
    return { voice, stream }
  }

  it('is offered only where the browser has it', async () => {
    cameraOnStage()
    delete document.pictureInPictureEnabled
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find('[data-testid="stage-pip"]').exists()).toBe(false)
  })

  it('moves the stage into the window, which keeps playing when the Talk view goes', async () => {
    const { stream } = cameraOnStage()
    const host = mount(PipHost, { attachTo: document.body })
    mounted.push(host)
    const pipVideo = host.get('video').element
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const button = w.get('[data-testid="stage-pip"]')
    expect(button.attributes('aria-label')).toBe('Picture-in-picture')
    await button.trigger('click')
    await flushPromises()
    expect(requestPip.mock.contexts[0]).toBe(pipVideo)
    expect(pipVideo.srcObject).toBe(stream)
    expect(button.attributes('aria-pressed')).toBe('true')
    // The stage says where the video went and decodes nothing meanwhile.
    expect(w.find('[data-testid="stage-in-pip"]').text()).toContain('Playing in the picture-in-picture window')
    expect(w.get('[data-testid="stage"] video').element.srcObject).toBe(null)

    // To a text channel: the stage unmounts, the window plays on.
    w.unmount()
    await flushPromises()
    expect(pipVideo.srcObject).toBe(stream)
    expect(pipElement).toBe(pipVideo)
  })

  it('"Back to the stage" closes the window and the stage plays again', async () => {
    const { stream } = cameraOnStage()
    mounted.push(mount(PipHost, { attachTo: document.body }))
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get('[data-testid="stage-pip"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="stage-in-pip"] button').trigger('click')
    await flushPromises()
    expect(pipElement).toBe(null)
    expect(w.find('[data-testid="stage-in-pip"]').exists()).toBe(false)
    expect(w.get('[data-testid="stage"] video').element.srcObject).toBe(stream)
  })

  it('the stage menu has it too', async () => {
    cameraOnStage()
    mounted.push(mount(PipHost, { attachTo: document.body }))
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get('[data-testid="stage"]').trigger('contextmenu')
    await flushPromises()
    const item = [...document.querySelectorAll('[role="menuitem"]')].find(i => i.textContent.includes('Picture-in-picture'))
    item.click()
    await flushPromises()
    expect(requestPip).toHaveBeenCalledTimes(1)
  })
})

describe('VoiceStage watch from the preview', () => {
  it('does not watch a screen when the channel switch is cancelled', async () => {
    const { voice } = seed()
    voice.setChannel('v2')
    voice.warnSwitchChannel = true
    const watch = vi.spyOn(voice, 'watchScreen')
    const w = mountStage({ channelId: 'v1' })
    w.findComponent(ParticipantTile).vm.$emit('watch-stream')
    await flushPromises()
    expect(pendingConfirm.value).toBeTruthy()
    pendingConfirm.value.resolve(false)
    await flushPromises()
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
    expect(watch).not.toHaveBeenCalled()
  })

  it('joins and then watches when the switch is confirmed', async () => {
    const { voice } = seed()
    voice.setChannel('v2')
    voice.warnSwitchChannel = true
    const watch = vi.spyOn(voice, 'watchScreen')
    const w = mountStage({ channelId: 'v1' })
    w.findComponent(ParticipantTile).vm.$emit('watch-stream')
    await flushPromises()
    pendingConfirm.value.resolve(true)
    await flushPromises()
    expect(rtc.joinVoiceChannel).toHaveBeenCalledWith('v1')
    expect(watch).toHaveBeenCalledWith('a')
  })
})
