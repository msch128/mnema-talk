import { required } from '../store-test-support.fixture'
import type { Mock } from 'vitest'
import { userFixture } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick, effectScope, ref } from 'vue'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import PipHost from '../components/PipHost.vue'
import { usePictureInPicture, usePictureInPictureHost, isPictureInPictureSupported } from './usePictureInPicture'

// The browser's Picture-in-Picture API, faked: one element in the window,
// enter/leave events, and videos that load their metadata on play().
let pipElement: HTMLVideoElement | null
let requestPip: Mock<(this: HTMLVideoElement) => Promise<PictureInPictureWindow>>
let exitPip: Mock<() => Promise<void>>
function enterPip(element: HTMLVideoElement) {
  pipElement = element
  element.dispatchEvent(new Event('enterpictureinpicture'))
  return Promise.resolve(Object.assign(new EventTarget(), { width: 320, height: 180, onresize: null }))
}
function installPip({ enabled = true } = {}) {
  pipElement = null
  Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, get: () => enabled })
  Object.defineProperty(document, 'pictureInPictureElement', { configurable: true, get: () => pipElement })
  requestPip = vi.fn(function (this: HTMLVideoElement) {
    return enterPip(this)
  })
  exitPip = vi.fn(() => {
    const el = pipElement
    pipElement = null
    el?.dispatchEvent(new Event('leavepictureinpicture'))
    return Promise.resolve()
  })
  HTMLVideoElement.prototype.requestPictureInPicture = requestPip
  document.exitPictureInPicture = exitPip
  // A stream "plays" at once: metadata known.
  Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, get() { return this.srcObject ? 1 : 0 } })
  HTMLVideoElement.prototype.play = vi.fn(() => Promise.resolve())
}
function uninstallPip() {
  Reflect.deleteProperty(HTMLVideoElement.prototype, 'requestPictureInPicture')
  Reflect.deleteProperty(HTMLVideoElement.prototype, 'readyState')
  Reflect.deleteProperty(HTMLVideoElement.prototype, 'play')
  Reflect.deleteProperty(document, 'exitPictureInPicture')
  Reflect.deleteProperty(document, 'pictureInPictureEnabled')
  Reflect.deleteProperty(document, 'pictureInPictureElement')
}

let host: ReturnType<typeof mount> | null = null
function setup() {
  useAuthStore().user = userFixture({ id: 'me', username: 'me' })
  const voice = useVoiceStore()
  voice.setChannel('v1')
  host = mount(PipHost, { attachTo: document.body })
  const video = host.get('[data-testid="pip-video"]').element
  if (!(video instanceof HTMLVideoElement)) throw new Error('Expected PiP host video')
  return { voice, video, pip: usePictureInPicture() }
}
function cameraOnStage(voice: ReturnType<typeof useVoiceStore>, userId = 'a') {
  const stream = new MediaStream()
  voice.handleMediaState({ channel_id: 'v1', user_id: userId, camera: true })
  voice.setUserVideoStream(userId, stream)
  voice.focusCamera(userId)
  return stream
}

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  installPip()
})
afterEach(async () => {
  host?.unmount()
  host = null
  await flushPromises()
  uninstallPip()
  document.body.innerHTML = ''
})

describe('isPictureInPictureSupported', () => {
  it('needs the document flag and the video method', () => {
    expect(isPictureInPictureSupported()).toBe(true)
    installPip({ enabled: false })
    expect(isPictureInPictureSupported()).toBe(false)
    installPip()
    Reflect.deleteProperty(HTMLVideoElement.prototype, 'requestPictureInPicture')
    expect(isPictureInPictureSupported()).toBe(false)
  })
})

describe('Picture-in-Picture of the stage', () => {
  it('opens the window with the stage stream on the hidden host video', async () => {
    const { voice, video, pip } = setup()
    const stream = cameraOnStage(voice)
    expect(await pip.enter()).toBe(true)
    expect(requestPip).toHaveBeenCalledTimes(1)
    expect(requestPip.mock.contexts[0]).toBe(video)
    expect(video.srcObject).toBe(stream)
    expect(video.muted).toBe(true)
    expect(pip.active.value).toBe(true)
  })

  it('does nothing without a stage or without a host', async () => {
    const { pip } = setup()
    expect(await pip.enter()).toBe(false)
    required(host).unmount()
    host = null
    const voice = useVoiceStore()
    cameraOnStage(voice)
    expect(await pip.enter()).toBe(false)
    expect(requestPip).not.toHaveBeenCalled()
  })

  it('follows the stage when it switches and closes when it empties', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice, 'a')
    await pip.enter()
    const screen = new MediaStream()
    voice.handleMediaState({ channel_id: 'v1', user_id: 'b', screen: true })
    voice.watchScreen('b')
    voice.setRemoteScreen('b', screen)
    await nextTick()
    expect(video.srcObject).toBe(screen)
    expect(pip.active.value).toBe(true)

    voice.unwatchScreen('b')
    await flushPromises()
    expect(exitPip).toHaveBeenCalledTimes(1)
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBe(null)
  })

  it('closes when the call ends', async () => {
    const { voice, pip } = setup()
    cameraOnStage(voice)
    await pip.enter()
    voice.disconnect()
    await flushPromises()
    expect(exitPip).toHaveBeenCalled()
    expect(pip.active.value).toBe(false)
  })

  it('the window closed by the user (its own X) ends the state and the decoding', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    await pip.enter()
    await document.exitPictureInPicture()
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBe(null)
  })

  it('toggle opens and closes', async () => {
    const { voice, pip } = setup()
    cameraOnStage(voice)
    await pip.toggle()
    expect(pip.active.value).toBe(true)
    await pip.toggle()
    expect(pip.active.value).toBe(false)
    expect(exitPip).toHaveBeenCalledTimes(1)
  })

  it('a refused request shows an error and leaves nothing open', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    requestPip.mockImplementationOnce(() => Promise.reject(new DOMException('denied', 'NotAllowedError')))
    expect(await pip.enter()).toBe(false)
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBe(null)
    expect(useToastStore().toasts.at(-1)?.text).toBe('Could not open picture-in-picture')
  })

  it('waits for the video size before asking for the window', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    let loaded = false
    Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, get: () => (loaded ? 1 : 0) })
    const opening = pip.enter()
    await flushPromises()
    expect(requestPip).not.toHaveBeenCalled()
    loaded = true
    video.dispatchEvent(new Event('loadedmetadata'))
    expect(await opening).toBe(true)
    expect(requestPip).toHaveBeenCalledTimes(1)
  })

  it('closes the window when the host goes away (logout)', async () => {
    const { voice, pip } = setup()
    cameraOnStage(voice)
    await pip.enter()
    required(host).unmount()
    host = null
    await flushPromises()
    expect(exitPip).toHaveBeenCalled()
    expect(pip.active.value).toBe(false)
  })
})

describe('pending picture-in-picture openings', () => {
  function waitForMetadata() {
    Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, get: () => 0 })
  }
  it('opens with stage B when A is replaced while metadata loads', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice, 'a')
    waitForMetadata()
    const opening = pip.enter()
    await flushPromises()
    const streamB = cameraOnStage(voice, 'b')
    Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, get: () => 1 })
    video.dispatchEvent(new Event('loadedmetadata'))
    expect(await opening).toBe(true)
    expect(video.srcObject).toBe(streamB)
    expect(requestPip).toHaveBeenCalledOnce()
  })
  it.each(['stage-empty', 'disconnect', 'host-unmount', 'explicit-exit'] as const)('does not resurrect an opening after %s while metadata loads', async cancel => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    waitForMetadata()
    const opening = pip.enter()
    await flushPromises()
    if (cancel === 'stage-empty') voice.userVideoStreams = {}
    if (cancel === 'disconnect') voice.disconnect()
    if (cancel === 'host-unmount') { required(host).unmount(); host = null }
    if (cancel === 'explicit-exit') await pip.exit()
    await nextTick()
    video.dispatchEvent(new Event('loadedmetadata'))
    expect(await opening).toBe(false)
    expect(requestPip).not.toHaveBeenCalled()
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBeNull()
    expect(useToastStore().toasts).toEqual([])
  })
  it('closes a browser window when permission arrives after disconnect', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    let allow: (() => void) | undefined
    requestPip.mockImplementationOnce(() => new Promise<PictureInPictureWindow>(resolve => {
      allow = () => { void enterPip(video).then(resolve) }
    }))
    const opening = pip.enter()
    await flushPromises()
    voice.disconnect()
    await nextTick()
    required(allow)()
    expect(await opening).toBe(false)
    expect(exitPip).toHaveBeenCalledOnce()
    expect(pip.active.value).toBe(false)
    expect(pipElement).toBeNull()
  })
  it('times out missing metadata with an error and removes the listener', async () => {
    vi.useFakeTimers()
    try {
      const { voice, video, pip } = setup()
      cameraOnStage(voice)
      waitForMetadata()
      const opening = pip.enter()
      await vi.advanceTimersByTimeAsync(3000)
      expect(await opening).toBe(false)
      expect(video.srcObject).toBeNull()
      expect(requestPip).not.toHaveBeenCalled()
      expect(useToastStore().toasts.at(-1)?.text).toBe('Could not open picture-in-picture')
      video.dispatchEvent(new Event('loadedmetadata'))
      expect(requestPip).not.toHaveBeenCalled()
    } finally { vi.useRealTimers() }
  })
  it('handles browser close failure without leaving stale active state', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    await pip.enter()
    exitPip.mockRejectedValueOnce(new Error('already closing'))
    await pip.exit()
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBeNull()
  })
  it('safely returns false when the browser support getter throws', () => {
    Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, get() { throw new Error('blocked API') } })
    expect(isPictureInPictureSupported()).toBe(false)
  })
})

describe('picture-in-picture host boundaries', () => {
  it('does not open for an unsupported browser or a disconnected preview', async () => {
    const { voice, pip } = setup()
    cameraOnStage(voice)
    installPip({ enabled: false })
    expect(await pip.enter()).toBe(false)
    installPip()
    voice.currentChannelId = null
    voice.isConnected = false
    voice.localScreenStream = new MediaStream()
    expect(await pip.enter()).toBe(false)
    expect(requestPip).not.toHaveBeenCalled()
  })
  it('catches play rejection while keeping the window source and tolerates duplicate enter', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    HTMLVideoElement.prototype.play = vi.fn(async () => { throw new Error('autoplay') })
    expect(await pip.enter()).toBe(true)
    expect(await pip.enter()).toBe(true)
    expect(video.srcObject).toBe(voice.stage?.stream)
  })
  it('leaves an already-open window intact when a repeated enter is refused', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    await pip.enter()
    requestPip.mockRejectedValueOnce(new Error('window already open'))
    expect(await pip.enter()).toBe(false)
    expect(video.srcObject).toBe(voice.stage?.stream)
    expect(pip.active.value).toBe(true)
  })
  it('does not show a failure toast when a canceled metadata request times out', async () => {
    vi.useFakeTimers()
    try {
      const { voice, pip } = setup()
      cameraOnStage(voice)
      Object.defineProperty(HTMLVideoElement.prototype, 'readyState', { configurable: true, value: 0 })
      const opening = pip.enter()
      await pip.exit()
      await vi.advanceTimersByTimeAsync(3000)
      expect(await opening).toBe(false)
      expect(useToastStore().toasts).toEqual([])
    } finally { vi.useRealTimers() }
  })
  it('disposes an absent host and does not clear another host installed later', () => {
    const first = effectScope()
    const second = effectScope()
    const firstRef = ref<HTMLVideoElement | null>(null)
    const secondRef = ref<HTMLVideoElement | null>(document.createElement('video'))
    first.run(() => usePictureInPictureHost(firstRef))
    second.run(() => usePictureInPictureHost(secondRef))
    first.stop()
    second.stop()
    expect(usePictureInPicture().active.value).toBe(false)
  })
  it('replaces its host element and removes listeners from the old element', () => {
    const scope = effectScope()
    const first = document.createElement('video')
    const next = document.createElement('video')
    const videoRef = ref<HTMLVideoElement | null>(first)
    scope.run(() => usePictureInPictureHost(videoRef))
    videoRef.value = next
    first.dispatchEvent(new Event('enterpictureinpicture'))
    expect(usePictureInPicture().active.value).toBe(false)
    next.dispatchEvent(new Event('enterpictureinpicture'))
    expect(usePictureInPicture().active.value).toBe(true)
    next.dispatchEvent(new Event('leavepictureinpicture'))
    videoRef.value = null
    scope.stop()
  })
  it('can be installed outside a scope and detects missing DOM globals', () => {
    const videoRef = ref<HTMLVideoElement | null>(null)
    usePictureInPictureHost(videoRef)
    vi.stubGlobal('HTMLVideoElement', undefined)
    expect(isPictureInPictureSupported()).toBe(false)
    vi.stubGlobal('document', undefined)
    expect(isPictureInPictureSupported()).toBe(false)
    vi.unstubAllGlobals()
  })
})

describe('empty host state changes', () => {
  it('cancels a browser request resolved immediately before the stage disappears', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    let resolveRequest: ((value: PictureInPictureWindow) => void) | undefined
    requestPip.mockImplementationOnce(() => new Promise<PictureInPictureWindow>(resolve => { resolveRequest = resolve }))
    const opening = pip.enter()
    await flushPromises()
    const window = await enterPip(video)
    required(resolveRequest)(window)
    voice.removeUserVideoStream('a')
    expect(await opening).toBe(false)
    expect(pip.active.value).toBe(false)
    expect(video.srcObject).toBeNull()
  })
  it('handles connecting and disconnecting without a mounted video', async () => {
    const scope = effectScope()
    const voice = useVoiceStore()
    scope.run(() => usePictureInPictureHost(ref<HTMLVideoElement | null>(null)))
    voice.setChannel('v1')
    await nextTick()
    voice.localScreenStream = new MediaStream()
    await nextTick()
    voice.localScreenStream = null
    await nextTick()
    voice.disconnect()
    await nextTick()
    expect(usePictureInPicture().active.value).toBe(false)
    scope.stop()
  })
  it('does not close a newer opening when an older browser request resolves late', async () => {
    const { voice, video, pip } = setup()
    cameraOnStage(voice)
    let resolveFirst: ((value: PictureInPictureWindow) => void) | undefined
    requestPip.mockImplementationOnce(() => new Promise<PictureInPictureWindow>(resolve => { resolveFirst = resolve }))
    const first = pip.enter()
    await flushPromises()
    const second = pip.enter()
    expect(await second).toBe(true)
    required(resolveFirst)(await enterPip(video))
    expect(await first).toBe(false)
    expect(pip.active.value).toBe(true)
    expect(exitPip).not.toHaveBeenCalled()
  })
  it('does not attempt to close a canceled request that resolved without a browser window', async () => {
    const { voice, pip } = setup()
    cameraOnStage(voice)
    let resolveRequest: ((value: PictureInPictureWindow) => void) | undefined
    requestPip.mockImplementationOnce(() => new Promise<PictureInPictureWindow>(resolve => { resolveRequest = resolve }))
    const opening = pip.enter()
    await flushPromises()
    await pip.exit()
    required(resolveRequest)(Object.assign(new EventTarget(), { width: 320, height: 180, onresize: null }))
    expect(await opening).toBe(false)
    expect(exitPip).not.toHaveBeenCalled()
  })
})
