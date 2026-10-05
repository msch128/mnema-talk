import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import PipHost from '../components/PipHost.vue'
import { usePictureInPicture, isPictureInPictureSupported } from './usePictureInPicture'

// The browser's Picture-in-Picture API, faked: one element in the window,
// enter/leave events, and videos that load their metadata on play().
let pipElement
let requestPip
let exitPip
function installPip({ enabled = true } = {}) {
  pipElement = null
  Object.defineProperty(document, 'pictureInPictureEnabled', { configurable: true, get: () => enabled })
  Object.defineProperty(document, 'pictureInPictureElement', { configurable: true, get: () => pipElement })
  requestPip = vi.fn(function () {
    pipElement = this
    this.dispatchEvent(new Event('enterpictureinpicture'))
    return Promise.resolve({ width: 320, height: 180 })
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
  delete HTMLVideoElement.prototype.requestPictureInPicture
  delete HTMLVideoElement.prototype.readyState
  delete HTMLVideoElement.prototype.play
  delete document.exitPictureInPicture
  delete document.pictureInPictureEnabled
  delete document.pictureInPictureElement
}

let host
function setup() {
  useAuthStore().user = { id: 'me', username: 'me' }
  const voice = useVoiceStore()
  voice.setChannel('v1')
  host = mount(PipHost, { attachTo: document.body })
  const video = host.get('[data-testid="pip-video"]').element
  return { voice, video, pip: usePictureInPicture() }
}
function cameraOnStage(voice, userId = 'a') {
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
    delete HTMLVideoElement.prototype.requestPictureInPicture
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
    host.unmount()
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
    host.unmount()
    host = null
    await flushPromises()
    expect(exitPip).toHaveBeenCalled()
    expect(pip.active.value).toBe(false)
  })
})
