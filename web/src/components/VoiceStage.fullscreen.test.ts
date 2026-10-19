import type { VueWrapper } from '@vue/test-utils'
import type { ComponentPublicInstance } from 'vue'
import { requireValue } from '../test-fixtures.fixture'
import { userFixture, categoryFixture, channelFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale, t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import VoiceStage from './VoiceStage.vue'
import PipHost from './PipHost.vue'

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

describe('VoiceStage full screen', () => {
  let fsEl: HTMLElement | null = null
  let requestFullscreen = vi.fn<(this: HTMLElement) => Promise<void>>()
  let exitFullscreen = vi.fn<() => Promise<void>>()
  beforeEach(() => {
    fsEl = null
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => fsEl })
    requestFullscreen = vi.fn(function (this: HTMLElement) {
      // Native fullscreen records the exact element receiving the call.
      // eslint-disable-next-line @typescript-eslint/no-this-alias
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
    Reflect.deleteProperty(HTMLElement.prototype, 'requestFullscreen')
    Reflect.deleteProperty(document, 'exitFullscreen')
    Reflect.deleteProperty(document, 'fullscreenElement')
  })

  function withCameras() {
    const { voice } = seed()
    voice.setChannel('v1')
    voice.channelUsers = { v1: {
      a: voiceUserFixture({ id: 'a', username: 'alice', display_name: 'Alice' }),
      b: voiceUserFixture({ id: 'b', username: 'bob', display_name: 'Bob' })
    } }
    voice.handleMediaState({ channel_id: 'v1', user_id: 'a', camera: true })
    voice.setUserVideoStream('a', new MediaStream())
    return voice
  }
  const stageEl = (w: ReturnType<typeof mount<typeof VoiceStage>>) => w.find<HTMLElement>('[data-testid="stage"]')
  const tileOf = (w: ReturnType<typeof mount<typeof VoiceStage>>, id: string) => w.find<HTMLElement>(`[data-participant-tile][data-user-id="${id}"]`)
  const press = (key: string, init: KeyboardEventInit = {}, target: HTMLElement = document.body) => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init }))
    return flushPromises()
  }

  it('the button and a double-click on the stage toggle full screen', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const button = w.get<HTMLElement>('[data-testid="stage-fullscreen"]')
    expect(button.attributes('aria-pressed')).toBe('false')
    await button.trigger('click')
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
    expect(button.attributes('aria-pressed')).toBe('true')
    expect(button.attributes('aria-label')).toBe('Exit full screen')

    await stageEl(w).find<HTMLVideoElement>('video').trigger('dblclick')
    await flushPromises()
    expect(fsEl).toBe(null)
    await stageEl(w).find<HTMLVideoElement>('video').trigger('dblclick')
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
  })

  it('a double-click on a stage button does not toggle full screen', async () => {
    const voice = withCameras()
    voice.focusCamera('a')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get<HTMLElement>('[data-testid="stage-unfocus-camera"]').trigger('dblclick')
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
    await stageEl(w).find<HTMLVideoElement>('video').trigger('click', { detail: 2 })
    await stageEl(w).find<HTMLVideoElement>('video').trigger('dblclick')
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
    await press('f', {}, w.get<HTMLElement>('[data-testid="stage-fullscreen"]').element)
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
    const items = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')]
    expect(items.map(i => i.textContent.trim())).toEqual(['Full screenF', 'Back to everyone'])
    requireValue(items[0]).click()
    await flushPromises()
    expect(fsEl).toBe(stageEl(w).element)
  })
})


describe('VoiceStage picture-in-picture', () => {
  let pipElement: HTMLVideoElement | null = null
  let requestPip = vi.fn<(this: HTMLVideoElement) => Promise<PictureInPictureWindow>>()
  beforeEach(() => {
    pipElement = null
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, get: () => true })
    Object.defineProperty(document, 'pictureInPictureElement', { configurable: true, get: () => pipElement })
    requestPip = vi.fn(function (this: HTMLVideoElement) {
      // Native PiP records the exact video receiving the call.
      // eslint-disable-next-line @typescript-eslint/no-this-alias
      pipElement = this
      return Promise.resolve(Object.assign(new EventTarget(), { width: 320, height: 180, onresize: null }))
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
    Reflect.deleteProperty(HTMLVideoElement.prototype, 'requestPictureInPicture')
    Reflect.deleteProperty(HTMLVideoElement.prototype, 'readyState')
    Reflect.deleteProperty(document, 'exitPictureInPicture')
    Reflect.deleteProperty(document, 'pictureInPictureEnabled')
    Reflect.deleteProperty(document, 'pictureInPictureElement')
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
    Reflect.deleteProperty(document, 'pictureInPictureEnabled')
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    expect(w.find<HTMLElement>('[data-testid="stage-pip"]').exists()).toBe(false)
  })

  it('moves the stage into the window, which keeps playing when the Talk view goes', async () => {
    const { stream } = cameraOnStage()
    const host = mount(PipHost, { attachTo: document.body })
    mounted.push(host)
    const pipVideo = host.get<HTMLVideoElement>('video').element
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    const button = w.get<HTMLElement>('[data-testid="stage-pip"]')
    expect(button.attributes('aria-label')).toBe('Picture-in-picture')
    await button.trigger('click')
    await flushPromises()
    expect(requestPip.mock.contexts[0]).toBe(pipVideo)
    expect(pipVideo.srcObject).toBe(stream)
    expect(button.attributes('aria-pressed')).toBe('true')
    // The stage says where the video went and decodes nothing meanwhile.
    expect(w.find<HTMLElement>('[data-testid="stage-in-pip"]').text()).toContain('Playing in the picture-in-picture window')
    expect(w.get<HTMLVideoElement>('[data-testid="stage"] video').element.srcObject).toBe(null)

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
    await w.get<HTMLElement>('[data-testid="stage-pip"]').trigger('click')
    await flushPromises()
    await w.get<HTMLElement>('[data-testid="stage-in-pip"] button').trigger('click')
    await flushPromises()
    expect(pipElement).toBe(null)
    expect(w.find<HTMLElement>('[data-testid="stage-in-pip"]').exists()).toBe(false)
    expect(w.get<HTMLVideoElement>('[data-testid="stage"] video').element.srcObject).toBe(stream)
  })

  it('the stage menu has it too', async () => {
    cameraOnStage()
    mounted.push(mount(PipHost, { attachTo: document.body }))
    const w = mountStage({ channelId: 'v1' })
    await nextTick()
    await w.get<HTMLElement>('[data-testid="stage"]').trigger('contextmenu')
    await flushPromises()
    const item = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(i => i.textContent.includes('Picture-in-picture'))
    requireValue(item).click()
    await flushPromises()
    expect(requestPip).toHaveBeenCalledTimes(1)
    await w.get<HTMLElement>('[data-testid="stage"]').trigger('contextmenu')
    await flushPromises()
    const exitItem = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(i => i.textContent.includes(t('talk.pipExit')))
    requireValue(exitItem).click()
    await flushPromises()
    expect(document.exitPictureInPicture).toHaveBeenCalledTimes(1)
  })
})
