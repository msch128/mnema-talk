import type { VueWrapper } from '@vue/test-utils'
import type { ComponentPublicInstance } from 'vue'
import { requireValue } from '../test-fixtures.fixture'
import { userFixture, categoryFixture, channelFixture, voiceUserFixture } from '../test-fixtures.fixture'
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
import TalkControlBar from './TalkControlBar.vue'
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

describe('VoiceStage hide participants without video', () => {
  function room() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' }),
      c: voiceUserFixture({ id: 'c', username: 'carl', display_name: 'Carl' })
    } }
    return voice
  }
  const tileIds = (w: ReturnType<typeof mount<typeof VoiceStage>>) => w.findAll<HTMLElement>('[data-participant-tile]').map(t => t.attributes('data-user-id'))
  const toggle = (w: ReturnType<typeof mount<typeof VoiceStage>>) => w.get<HTMLElement>('[data-testid="hide-no-video"]')

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
    expect(JSON.parse(requireValue(localStorage.getItem('mnema_hide_no_video')))).toEqual({ on: true })

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
    const hint = w.get<HTMLElement>('[data-testid="no-video-hint"]')
    expect(hint.text()).toContain('Nobody has video on right now')
    await hint.get<HTMLElement>('button').trigger('click')
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
    expect(w.get<HTMLElement>('[data-testid="talk-strip"]').classes()).not.toContain('hidden')
    expect(tileIds(w)).toEqual(['a'])
    voice.setCameraHidden('a', true)
    await nextTick()
    // The camera left the stage and nobody has video: the hint.
    expect(w.find<HTMLElement>('[data-testid="no-video-hint"]').exists()).toBe(true)
  })

  it('does not apply to the preview of a Talk I am not in', async () => {
    const { voice } = seed()
    voice.setHideNoVideo(true)
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="hide-no-video"]').exists()).toBe(false)
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
    requireValue(pendingConfirm.value).resolve(false)
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
    requireValue(pendingConfirm.value).resolve(true)
    await flushPromises()
    expect(rtc.joinVoiceChannel).toHaveBeenCalledWith('v1')
    expect(watch).toHaveBeenCalledWith('a')
  })
})

describe('VoiceStage floating controls', () => {
  function connected() {
    const { voice } = seed()
    voice.setChannel('v1')
    return voice
  }
  const bar = (w: VueWrapper<ComponentPublicInstance>) => w.get<HTMLElement>('[data-testid="talk-controls"]')

  it('fade out after 3 s without activity and come back on a mouse move', async () => {
    vi.useFakeTimers()
    try {
      connected()
      const w = mountStage({ channelId: 'v1' })
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('true')
      vi.advanceTimersByTime(2900)
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('true')
      vi.advanceTimersByTime(100)
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('false')
      await w.get<HTMLElement>('main').trigger('pointermove')
      expect(bar(w).attributes('data-visible')).toBe('true')
    } finally {
      vi.useRealTimers()
    }
  })

  it('stay while keyboard focus or the pointer is on them', async () => {
    vi.useFakeTimers()
    try {
      connected()
      const w = mountStage({ channelId: 'v1' })
      await nextTick()
      bar(w).get<HTMLElement>('[data-testid="talk-mute"]').element.focus()
      await nextTick()
      vi.advanceTimersByTime(10_000)
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('true')
      bar(w).get<HTMLElement>('[data-testid="talk-mute"]').element.blur()
      await nextTick()
      await bar(w).trigger('pointerenter', { pointerType: 'mouse' })
      vi.advanceTimersByTime(10_000)
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('true')
      await bar(w).trigger('pointerleave', { pointerType: 'mouse' })
      vi.advanceTimersByTime(3000)
      await nextTick()
      expect(bar(w).attributes('data-visible')).toBe('false')
    } finally {
      vi.useRealTimers()
    }
  })

  it('stay while the "more" menu of the compact bar is open', async () => {
    connected()
    const w = mount(TalkControlBar, { props: { compact: true }, attachTo: document.body })
    mounted.push(w)
    expect(requireValue(w.emitted('update:pinned')).at(-1)).toEqual([false])
    await w.get<HTMLElement>('[data-testid="talk-more"]').trigger('click')
    await flushPromises()
    expect(requireValue(w.emitted('update:pinned')).at(-1)).toEqual([true])
    expect(document.body.textContent).toContain('Noise suppression')
  })

  it('offers stream settings in more menu while sharing screen', async () => {
    const voice = connected()
    voice.isScreenSharing = true
    const w = mount(TalkControlBar, { props: { compact: true }, attachTo: document.body })
    mounted.push(w)
    await w.get<HTMLElement>('[data-testid="talk-more"]').trigger('click')
    await flushPromises()
    const item = document.querySelector<HTMLElement>('[data-menu-item="stream-settings"]')
    expect(item).not.toBeNull()
    expect(voice.showScreenShareModal).toBe(false)
    requireValue(item).click()
    expect(voice.showScreenShareModal).toBe(true)
  })

  it('are never there in a preview (nothing hides the Join button)', async () => {
    vi.useFakeTimers()
    try {
      seed()
      const w = mountStage({ channelId: 'v1' })
      await nextTick()
      vi.advanceTimersByTime(10_000)
      await nextTick()
      expect(w.find<HTMLElement>('[data-testid="talk-controls"]').exists()).toBe(false)
      expect(w.text()).toContain('Join')
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('VoiceStage participant strip', () => {
  function watching() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    return voice
  }

  it('collapses under a stage and stays collapsed (per browser)', async () => {
    watching()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const toggle = () => w.get<HTMLElement>('[data-testid="talk-strip-toggle"]')
    const strip = () => w.get<HTMLElement>('[data-testid="talk-strip"]')
    expect(toggle().attributes('aria-expanded')).toBe('true')
    expect(strip().attributes('data-collapsed')).toBe('false')
    await toggle().trigger('click')
    expect(toggle().attributes('aria-expanded')).toBe('false')
    expect(strip().attributes('data-collapsed')).toBe('true')
    expect(strip().attributes()).toHaveProperty('inert')
    expect(toggle().text()).toContain('3')
    expect(localStorage.getItem('mnema.talk.stripCollapsed')).toBe('true')

    w.unmount()
    const again = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(again.get<HTMLElement>('[data-testid="talk-strip"]').attributes('data-collapsed')).toBe('true')
    await again.get<HTMLElement>('[data-testid="talk-strip-toggle"]').trigger('click')
    expect(again.get<HTMLElement>('[data-testid="talk-strip"]').attributes('data-collapsed')).toBe('false')
    expect(localStorage.getItem('mnema.talk.stripCollapsed')).toBe(null)
  })
})
