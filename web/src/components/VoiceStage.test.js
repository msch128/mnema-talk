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

function mountStage(props = {}) {
  return mount(VoiceStage, { props, attachTo: document.body })
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  Object.values(rtc).forEach(fn => fn.mockReset())
  getUserMedia.mockReset()
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve([]) })))
})
afterEach(() => {
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

  it('the chat toggle emits update:showChat and the chat follows the prop', async () => {
    const { chat } = seed()
    chat.selectChannel = vi.fn()
    const w = mountStage({ channelId: 'v1', showChat: false })
    expect(w.find('input[type="file"]').exists()).toBe(false)
    const toggle = w.findAll('button').find(b => b.text().includes('Show chat'))
    await toggle.trigger('click')
    expect(w.emitted('update:showChat')).toEqual([[true]])

    await w.setProps({ showChat: true })
    expect(w.find('input[type="file"]').exists()).toBe(true)
    // The previewed talk's chat is loaded without joining.
    expect(chat.selectChannel).toHaveBeenCalledWith(expect.objectContaining({ id: 'v1' }))
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
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
    await w.find('[data-testid="screen-card"] button').trigger('click')
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

    // Slider updates user volume
    await w.find('[data-testid="viewer-stream-volume-slider"]').setValue('150')
    expect(voice.getUserVolume('a')).toBe(150)

    // Mute button toggles local mute for stream
    await w.find('[data-testid="viewer-stream-audio-mute"]').trigger('click')
    expect(voice.isUserLocalMuted('a')).toBe(true)
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
    await w.find('[data-screen-card="a"] button').trigger('click')
    voice.setRemoteScreen('a', alice)
    await flushPromises()
    expect(cardKeys()).toEqual(['own'])
    expect(w.find('[data-screen-card="own"]').text()).toContain('Your screen')
    expect(stageVideo().srcObject).toBe(alice)
    expect(stageVideo().muted).toBe(false)
    expect(w.text()).toContain('Alice')
    expect(w.find('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(true)
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(false)
    expect(w.find('button[aria-label="Stop watching"]').exists()).toBe(true)

    // Back to my own share: Alice's stays received, as a card.
    await w.find('[data-screen-card="own"] button').trigger('click')
    await flushPromises()
    expect(stageVideo().srcObject).toBe(own)
    expect(stageVideo().muted).toBe(true)
    expect(cardKeys()).toEqual(['a'])
    expect(w.find('[data-screen-card="a"] button').text()).toBe('Show on stage')
    expect(w.find('[data-testid="streamer-audio-toggle"]').exists()).toBe(true)
    expect(w.find('[data-testid="viewer-stream-audio-mute"]').exists()).toBe(false)
    expect(voice.watchedScreens).toEqual({ a: true })

    // And to Alice again.
    await w.find('[data-screen-card="a"] button').trigger('click')
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
      await w.find('[data-screen-card="own"] button').trigger('click')
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(true)
    } finally {
      hasFocus.mockRestore()
    }
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

describe('VoiceStage chat', () => {
  function withChat() {
    const { chat } = seed()
    chat.selectChannel = vi.fn()
    chat.activeChannel = { id: 'v1', name: 'Lounge', type: 'voice' }
    chat.messages = [{
      id: 'm1', channel_id: 'v1', user_id: 'a', username: 'alice', display_name: 'Alice',
      content: 'hi', created_at: '2026-01-01T10:00:00Z', reactions: [],
      attachments: [{ id: 'att', url: '/m/v', mime_type: 'video/mp4', original_filename: 'clip.mp4', size_bytes: 10 }]
    }]
    return chat
  }

  it('author name is a button and videos play inline', async () => {
    const chat = withChat()
    const open = vi.spyOn(chat, 'openUserProfile').mockImplementation(() => {})
    const w = mountStage({ channelId: 'v1', showChat: true })
    await nextTick()
    const name = w.find('[data-testid="author-name"]')
    expect(name.element.tagName).toBe('BUTTON')
    await name.trigger('click')
    expect(open).toHaveBeenCalled()
    expect(w.find('[data-attachment="video"] video').exists()).toBe(true)
  })

  it('clears the file input after a failed upload', async () => {
    const chat = withChat()
    vi.spyOn(chat, 'uploadMedia').mockRejectedValue(new Error('nope'))
    const w = mountStage({ channelId: 'v1', showChat: true })
    const input = w.find('input[type="file"]').element
    Object.defineProperty(input, 'files', { configurable: true, value: [new File(['x'], 'a.png', { type: 'image/png' })] })
    let value = 'C:\\fakepath\\a.png'
    Object.defineProperty(input, 'value', { configurable: true, get: () => value, set: v => { value = v } })
    input.dispatchEvent(new Event('change'))
    await flushPromises()
    expect(chat.uploadMedia).toHaveBeenCalled()
    expect(value).toBe('')
  })
})
