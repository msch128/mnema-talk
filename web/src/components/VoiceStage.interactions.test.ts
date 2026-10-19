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
import StreamQualityMenu from './StreamQualityMenu.vue'
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

describe('VoiceStage account, sharing and viewport interactions', () => {
  function remoteStage() {
    const { voice, chat } = seed()
    voice.setChannel('v1')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true, camera: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    return { voice, chat }
  }
  function ownStage() {
    const { voice, chat } = seed()
    voice.setChannel('v1')
    voice.isScreenSharing = true
    voice.localScreenStream = new MediaStream()
    return { voice, chat }
  }
  async function menuAction(label: string) {
    const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(el => el.textContent.includes(label))
    requireValue(item).click()
    await flushPromises()
  }

  it('offers a disabled empty-state Join without a known channel and finds uncategorized rooms', async () => {
    const { chat } = seed()
    const empty = mountStage()
    expect(empty.get<HTMLElement>('[data-testid="talk-empty"] button').attributes('disabled')).toBeDefined()
    expect(empty.text()).toContain('No one in the Talk')
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
    empty.unmount()
    chat.categories = []
    chat.uncategorized = [channelFixture({ id: 'v1', name: 'Uncategorized lounge', type: 'voice' })]
    const room = mountStage({ channelId: 'v1' })
    expect(room.text()).toContain('Uncategorized lounge')
    const members = room.findAll<HTMLElement>('header button').at(-1)
    await requireValue(members).trigger('click')
    expect(chat.showMemberList).toBe(false)
    await requireValue(members).trigger('click')
    expect(chat.showMemberList).toBe(true)
  })

  it('opens the member menu from grid and strip tiles and routes their actions', async () => {
    const { voice, chat } = seed()
    voice.setChannel('v1')
    const w = mountStage({ channelId: 'v1' })
    let alice = requireValue(w.findAllComponents(ParticipantTile).find(tile => tile.props('user').id === 'a'))
    alice.vm.$emit('menu', new MouseEvent('contextmenu', { clientX: 20, clientY: 30 }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    await menuAction('View profile')
    expect(chat.selectedUserProfile?.id).toBe('a')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true, camera: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()
    alice = requireValue(w.findAllComponents(ParticipantTile).find(tile => tile.props('user').id === 'a'))
    alice.vm.$emit('menu', new MouseEvent('contextmenu', { clientX: 30, clientY: 40 }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    const hide = vi.spyOn(voice, 'toggleCameraHidden')
    const watch = vi.spyOn(voice, 'watchScreen')
    const unwatch = vi.spyOn(voice, 'unwatchScreen')
    alice.vm.$emit('toggle-camera')
    alice.vm.$emit('watch-stream')
    alice.vm.$emit('stop-watching')
    alice.vm.$emit('open-profile', alice.props('user'))
    await flushPromises()
    expect(hide).toHaveBeenCalledWith('a')
    expect(watch).toHaveBeenCalledWith('a')
    expect(unwatch).toHaveBeenCalledWith('a')
    expect(chat.selectedUserProfile?.id).toBe('a')
  })

  it('updates video resolution on metadata and resize and clears it when dimensions disappear', async () => {
    remoteStage()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const video = w.get<HTMLVideoElement>('[data-testid="stage"] video')
    Object.defineProperty(video.element, 'videoWidth', { configurable: true, value: 3840 })
    Object.defineProperty(video.element, 'videoHeight', { configurable: true, value: 2160 })
    await video.trigger('loadedmetadata')
    expect(w.text()).toContain('3840×2160')
    Object.defineProperty(video.element, 'videoWidth', { configurable: true, value: 1920 })
    Object.defineProperty(video.element, 'videoHeight', { configurable: true, value: 1200 })
    await video.trigger('resize')
    expect(w.text()).toContain('1920×1200')
    Object.defineProperty(video.element, 'videoWidth', { configurable: true, value: 0 })
    await video.trigger('resize')
    expect(w.text()).not.toContain('1920×1200')
  })

  it('pauses and restores the own preview on visibility, blur and focus', async () => {
    ownStage()
    let visible: DocumentVisibilityState = 'visible'
    let focused = true
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visible)
    const focus = vi.spyOn(document, 'hasFocus').mockImplementation(() => focused)
    try {
      const w = mountStage({ channelId: 'v1' })
      await flushPromises()
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(false)
      visible = 'hidden'
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(true)
      expect(w.get<HTMLVideoElement>('[data-testid="stage"] video').element.srcObject).toBeNull()
      visible = 'visible'
      focused = false
      window.dispatchEvent(new Event('blur'))
      await flushPromises()
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(true)
      focused = true
      window.dispatchEvent(new Event('focus'))
      await flushPromises()
      expect(w.find('[data-testid="own-stream-paused"]').exists()).toBe(false)
      expect(w.get<HTMLVideoElement>('[data-testid="stage"] video').element.srcObject).toBe(useVoiceStore().localScreenStream)
    } finally {
      visibility.mockRestore()
      focus.mockRestore()
    }
  })

  it('runs own-share and remote-share stage menu actions', async () => {
    const { voice } = ownStage()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get('[data-testid="stage"]').trigger('contextmenu')
    await menuAction('Stop sharing')
    expect(rtc.stopScreenShare).toHaveBeenCalledOnce()
    rtc.stopScreenShare.mockClear()
    const ownStop = w.findAll<HTMLElement>('[data-testid="stage"] button').find(el => el.attributes('aria-label')?.includes('Stop sharing'))
    await requireValue(ownStop).trigger('click')
    expect(rtc.stopScreenShare).toHaveBeenCalledOnce()
    voice.isScreenSharing = false
    voice.localScreenStream = null
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    await nextTick()
    await w.get('[data-testid="stage"]').trigger('contextmenu')
    await menuAction('Stop watching')
    expect(voice.watchedScreens['a']).toBeUndefined()
  })

  it('uses camera-strip focus/fullscreen actions and ignores unavailable cameras', async () => {
    const { voice } = remoteStage()
    voice.setUserVideoStream('a', new MediaStream())
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const alice = requireValue(w.findAllComponents(ParticipantTile).find(tile => tile.props('user').id === 'a'))
    alice.vm.$emit('focus-camera')
    await nextTick()
    expect(w.get('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:a')
    alice.vm.$emit('fullscreen-camera')
    await flushPromises()
    await w.get('[data-testid="stage-unfocus-camera"]').trigger('click')
    expect(voice.focusedCamera).toBeNull()
    voice.removeUserVideoStream('a')
    await nextTick()
    const focus = vi.spyOn(voice, 'focusCamera')
    requireValue(w.findAllComponents(ParticipantTile).find(tile => tile.props('user').id === 'a')).vm.$emit('fullscreen-camera')
    await flushPromises()
    expect(focus).toHaveBeenCalledWith('a')
    expect(w.get('[data-testid="stage"]').attributes('data-stage-source')).toBe('a')
  })

  it('shows fallback stage labels for missing sharer profiles and camera names', async () => {
    const { voice } = remoteStage()
    voice.channelUsers = { v1: {} }
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.text()).toContain('Shared screen')
    voice.setUserVideoStream('unknown', new MediaStream())
    voice.focusCamera('unknown')
    await nextTick()
    expect(w.text()).toContain('Camera')
    expect(w.get('[data-testid="stage"]').attributes('data-stage-source')).toBe('camera:unknown')
  })

  it('unsubscribes a pending screen card and routes grid stop-watching', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const card = w.get('[data-testid="screen-card"]')
    expect(card.get('[data-testid="screen-card-action"]').attributes('disabled')).toBeDefined()
    await requireValue(card.findAll<HTMLElement>('button').find(button => button.attributes('aria-label')?.includes('Stop watching'))).trigger('click')
    expect(voice.watchedScreens['a']).toBeUndefined()
    const unwatch = vi.spyOn(voice, 'unwatchScreen')
    requireValue(w.findAllComponents(ParticipantTile).find(tile => tile.props('user').id === 'a')).vm.$emit('stop-watching')
    expect(unwatch).toHaveBeenCalledWith('a')
  })
})

describe('VoiceStage measured layout and overlay behavior', () => {
  let observers: LayoutObserver[]
  class LayoutObserver implements ResizeObserver {
    element: Element | null = null
    constructor(readonly callback: ResizeObserverCallback) { observers.push(this) }
    observe(element: Element) { this.element = element }
    disconnect() { this.element = null }
    unobserve(element: Element) { if (this.element === element) this.element = null }
  }
  beforeEach(() => {
    observers = []
    vi.stubGlobal('ResizeObserver', LayoutObserver)
  })
  async function size(w: ReturnType<typeof mountStage>, width: number, height: number) {
    for (const observer of observers) {
      const el = observer.element
      if (!(el instanceof HTMLElement) || !w.element.contains(el) && el !== w.element) continue
      const id = el.dataset['testid']
      const measuredHeight = id === 'talk-area' || el.tagName === 'MAIN' ? height : id === 'talk-strip-region' ? 60 : id === 'talk-grid' ? height : 35
      Object.defineProperty(el, 'clientWidth', { configurable: true, value: width })
      Object.defineProperty(el, 'clientHeight', { configurable: true, value: measuredHeight })
      observer.callback([], observer)
    }
    await nextTick()
  }
  function sharing() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      me: voiceUserFixture({ id: 'me', username: 'me', display_name: 'Me' }),
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: '' })
    } }
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', screen: true })
    voice.watchScreen('a')
    voice.setRemoteScreen('a', new MediaStream())
    return voice
  }

  it('fits the stage to measured space, hides details on tiny views, and suppresses the narrow header timer', async () => {
    const voice = sharing()
    voice.roomStartedAt = { v1: new Date(Date.now() - 65_000).toISOString() }
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await size(w, 1200, 800)
    expect(w.get<HTMLElement>('[data-testid="stage"]').element.style.width).not.toBe('')
    expect(w.find('[data-testid="talk-timer"]').exists()).toBe(true)
    expect(w.text()).toContain('alice')
    const video = w.get<HTMLVideoElement>('[data-testid="stage"] video')
    Object.defineProperty(video.element, 'videoWidth', { configurable: true, value: 1920 })
    Object.defineProperty(video.element, 'videoHeight', { configurable: true, value: 1080 })
    await video.trigger('resize')
    expect(w.text()).toContain('1920×1080')
    await size(w, 510, 650)
    expect(w.find('[data-testid="talk-timer"]').exists()).toBe(false)
    expect(w.text()).not.toContain('1920×1080')
    expect(w.get('[data-testid="viewer-stream-volume-slider"]').classes()).toContain('w-12')
    await size(w, 390, 650)
    expect(w.find('[data-testid="viewer-stream-volume-slider"]').exists()).toBe(false)
    voice.isScreenSharing = true
    voice.localScreenStream = new MediaStream()
    await nextTick()
    expect(w.find('[data-testid="screen-card"]').exists()).toBe(true)
    await size(w, 1000, 800)
    expect(w.get<HTMLElement>('[data-testid="stage"]').element.style.height).not.toBe('')
  })

  it('keeps overlays visible for mouse hover and ignores touch hover, and lets quality menus pin controls', async () => {
    vi.useFakeTimers()
    try {
      const voice = sharing()
      voice.isScreenSharing = true
      voice.localScreenStream = new MediaStream()
      voice.focusOwnScreen()
      const w = mountStage({ channelId: 'v1' })
      await nextTick()
      const audio = w.get<HTMLElement>('[data-testid="streamer-audio-toggle"]')
      const overlay = requireValue(audio.element.parentElement)
      overlay.dispatchEvent(new PointerEvent('pointerenter', { pointerType: 'mouse' }))
      vi.advanceTimersByTime(5000)
      await nextTick()
      expect(w.get('[data-testid="talk-controls"]').attributes('data-visible')).toBe('true')
      overlay.dispatchEvent(new PointerEvent('pointerleave', { pointerType: 'mouse' }))
      overlay.dispatchEvent(new PointerEvent('pointerenter', { pointerType: 'touch' }))
      await nextTick()
      vi.advanceTimersByTime(3000)
      await nextTick()
      expect(w.get('[data-testid="talk-controls"]').attributes('data-visible')).toBe('false')
      expect(w.get('[data-testid="stage"]').classes()).toContain('cursor-none')
      const quality = w.findComponent(StreamQualityMenu)
      quality.vm.$emit('open-change', true)
      await nextTick()
      vi.advanceTimersByTime(5000)
      await nextTick()
      expect(w.get('[data-testid="talk-controls"]').attributes('data-visible')).toBe('true')
      quality.vm.$emit('open-change', false)
      voice.focusScreen('a')
      await nextTick()
      const cardQuality = w.getComponent(StreamQualityMenu)
      cardQuality.vm.$emit('open-change', true)
      await nextTick()
      vi.advanceTimersByTime(5000)
      await nextTick()
      expect(w.get('[data-testid="talk-controls"]').attributes('data-visible')).toBe('true')
      cardQuality.vm.$emit('open-change', false)
    } finally { vi.useRealTimers() }
  })

  it('handles blocked fullscreen promises and ignores context menus when fullscreen is already open', async () => {
    sharing()
    const request = vi.fn<() => Promise<void>>().mockRejectedValue(new Error('denied'))
    const exit = vi.fn<() => Promise<void>>().mockRejectedValue(new Error('denied'))
    let fullscreen: Element | null = null
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => fullscreen })
    HTMLElement.prototype.requestFullscreen = request
    document.exitFullscreen = exit
    try {
      const w = mountStage({ channelId: 'v1' })
      await nextTick()
      await w.get('[data-testid="stage-fullscreen"]').trigger('click')
      await flushPromises()
      expect(request).toHaveBeenCalledOnce()
      fullscreen = w.get('[data-testid="stage"]').element
      document.dispatchEvent(new Event('fullscreenchange'))
      await nextTick()
      await w.get('[data-testid="stage"]').trigger('contextmenu')
      expect(document.querySelector('[role="menu"]')).toBeNull()
      await w.get('[data-testid="stage-fullscreen"]').trigger('click')
      await flushPromises()
      expect(exit).toHaveBeenCalledOnce()
      fullscreen = null
      document.dispatchEvent(new Event('fullscreenchange'))
      Reflect.deleteProperty(HTMLElement.prototype, 'requestFullscreen')
      await w.get('[data-testid="stage-fullscreen"]').trigger('click')
      await flushPromises()
      expect(request).toHaveBeenCalledOnce()
      const input = document.createElement('input')
      input.setAttribute('role', 'dialog')
      w.element.append(input)
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'f', bubbles: true }))
      expect(request).toHaveBeenCalledOnce()
    } finally {
      Reflect.deleteProperty(HTMLElement.prototype, 'requestFullscreen')
      Reflect.deleteProperty(document, 'exitFullscreen')
      Reflect.deleteProperty(document, 'fullscreenElement')
    }
  })

  it('retains a usable scrolling camera grid when the area cannot fit all participant tiles', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.localCameraStream = new MediaStream()
    voice.channelUsers = { v1: Object.fromEntries(Array.from({ length: 12 }, (_, i) => [`u${i}`, voiceUserFixture({ id: `u${i}`, username: `person${i}` })])) }
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await size(w, 350, 90)
    expect(w.get('[data-testid="talk-grid"]').classes()).toContain('overflow-y-auto')
    expect(w.findAll('[data-participant-tile]')).toHaveLength(13)
  })
})


describe('VoiceStage residual normal interactions', () => {
  it('keeps the switch confirmation usable when the previewed channel disappears', async () => {
    const { chat, voice } = seed()
    voice.setChannel('v2')
    voice.warnSwitchChannel = true
    const w = mountStage({ channelId: 'v1' })
    chat.categories = []
    await nextTick()
    expect(w.text()).not.toContain('Lounge')
    const joinButton = requireValue(w.findAll('button').find(button => button.text().includes('Join')))
    await joinButton.trigger('click')
    expect(pendingConfirm.value).not.toBeNull()
    requireValue(pendingConfirm.value).resolve(false)
    await flushPromises()
    expect(rtc.joinVoiceChannel).not.toHaveBeenCalled()
    expect(w.emitted('join')).toBeUndefined()
  })

  it('resolves an uncategorized Talk and displays an empty room preview', async () => {
    const { chat } = seed()
    chat.uncategorized = [channelFixture({ id: 'uncategorized', name: 'Uncategorized Talk', type: 'voice' })]
    const w = mountStage({ channelId: 'uncategorized' })
    await nextTick()
    expect(w.text()).toContain('Uncategorized Talk')
    expect(w.find('[data-testid="talk-empty"]').exists()).toBe(true)
    expect(w.findAllComponents(ParticipantTile)).toHaveLength(0)
  })
  it('takes a focused camera off through its context menu', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get('[data-testid="stage"]').trigger('contextmenu')
    await flushPromises()
    const item = requireValue([...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(row => row.textContent.includes('Back to everyone')))
    item.click()
    await nextTick()
    expect(voice.focusedCamera).toBeNull()
    expect(w.find('[data-testid="stage"]').exists()).toBe(false)
  })
  it('ignores a double click in the surrounding Talk area', async () => {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.isScreenSharing = true
    voice.localScreenStream = new MediaStream()
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const fullscreen = vi.fn<() => Promise<void>>().mockResolvedValue()
    const stage = w.get<HTMLElement>('[data-testid="stage"]').element
    stage.requestFullscreen = fullscreen
    await w.get('[data-testid="talk-area"]').trigger('dblclick')
    await flushPromises()
    expect(fullscreen).not.toHaveBeenCalled()
  })
})
