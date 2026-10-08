// Picture-in-Picture for the Talk's stage: whatever is on the stage (a
// screen share or a camera) plays in the browser's floating window and keeps
// playing while the user reads a text channel. The Talk view (VoiceStage) may
// unmount then, so the video in that window is not the stage's own: an
// app-level host (PipHost.vue) owns a hidden <video> that this module feeds
// with the current stage stream. Only an explicit click opens the window.
import { ref, watch, onScopeDispose, getCurrentScope } from 'vue'
import type { Ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// The window shows the stage right now (shared by the host and the stage).
const active = ref(false)
let hostVideo: HTMLVideoElement | null = null
// Exit, disconnect and host disposal also invalidate an opening still waiting
// for metadata or the browser's permission result.
let openingGeneration = 0
let latestOpening = 0

/** The browser can open a Picture-in-Picture window for a <video>. */
export function isPictureInPictureSupported() {
  try {
    return typeof document !== 'undefined' &&
      document.pictureInPictureEnabled === true &&
      typeof HTMLVideoElement !== 'undefined' &&
      typeof HTMLVideoElement.prototype.requestPictureInPicture === 'function'
  } catch {
    return false
  }
}

// requestPictureInPicture needs the video's size: wait until it is known.
function metadataLoaded(video: HTMLVideoElement, timeoutMs = 3000): Promise<void> {
  if (video.readyState >= 1) return Promise.resolve()
  return new Promise<void>((resolve, reject) => {
    const done = (ok: boolean) => {
      clearTimeout(timer)
      video.removeEventListener('loadedmetadata', onLoaded)
      if (ok) resolve()
      else reject(new Error('no video metadata'))
    }
    const onLoaded = () => done(true)
    const timer = setTimeout(() => done(false), timeoutMs)
    video.addEventListener('loadedmetadata', onLoaded)
  })
}

function setSource(video: HTMLVideoElement, stream: MediaStream) {
  if (video.srcObject === stream) return
  video.srcObject = stream
  Promise.resolve(video.play()).catch(() => {})
}

function inWindow() {
  return !!hostVideo && typeof document !== 'undefined' && document.pictureInPictureElement === hostVideo
}

/** Opens the window with the stage. Resolves to whether it opened. */
async function enter() {
  const voice = useVoiceStore()
  const video = hostVideo
  if (!video || !voice.stage?.stream || !isPictureInPictureSupported()) return false
  const generation = ++openingGeneration
  latestOpening = generation
  const currentStream = (): MediaStream | null => generation === openingGeneration && hostVideo === video && voice.isConnected
    ? voice.stage?.stream || null : null
  try {
    video.muted = true // the call's audio plays elsewhere
    // The stage may change while metadata loads. Open with the current stage,
    // and wait again if changing its source reset the browser's metadata.
    let stream = currentStream()
    if (!stream) return false
    while (true) {
      setSource(video, stream)
      await metadataLoaded(video)
      const latest = currentStream()
      if (!latest) return false
      if (latest === stream) break
      stream = latest
    }
    await video.requestPictureInPicture()
    const latest = currentStream()
    if (!latest) {
      // Permission may resolve after the host or call disappeared. Close that
      // late window without undoing a newer opening on the same host.
      if (latestOpening === generation) {
        if (typeof document !== 'undefined' && document.pictureInPictureElement === video) {
          try { await document.exitPictureInPicture() } catch { /* Already closed. */ }
        }
        active.value = false
        video.srcObject = null
      }
      return false
    }
    // A stage switch during the browser request follows the stage too.
    setSource(video, latest)
    active.value = true
    return true
  } catch {
    if (!currentStream()) return false
    if (!inWindow()) {
      active.value = false
      video.srcObject = null
    }
    useToastStore().error(t('talk.pipFailed'))
    return false
  }
}

/** Closes the window (the stage shows the video again). */
async function exit() {
  openingGeneration++
  if (inWindow()) {
    try {
      await document.exitPictureInPicture()
    } catch {
      // Closed already.
    }
  }
  active.value = false
  if (hostVideo) hostVideo.srcObject = null
}

function toggle() {
  return active.value ? exit() : enter()
}

/** For the stage's controls: support, state and the actions. */
export function usePictureInPicture() {
  return { supported: isPictureInPictureSupported(), active, enter, exit, toggle }
}

/**
 * The app-level owner of the window's <video> (`videoRef`, a ref). Keeps it
 * on the current stage stream while the window is open and closes the
 * window when the stage empties, the call ends or the host goes away.
 */
export function usePictureInPictureHost(videoRef: Ref<HTMLVideoElement | null>) {
  const voice = useVoiceStore()
  let video: HTMLVideoElement | null = null

  function onLeave() {
    active.value = false
    if (video) video.srcObject = null
  }
  function onEnter() {
    active.value = true
  }

  watch(videoRef, el => {
    openingGeneration++
    if (video) {
      video.removeEventListener('leavepictureinpicture', onLeave)
      video.removeEventListener('enterpictureinpicture', onEnter)
      video.srcObject = null
    }
    video = el || null
    hostVideo = video
    if (video) {
      video.addEventListener('leavepictureinpicture', onLeave)
      video.addEventListener('enterpictureinpicture', onEnter)
    }
  }, { immediate: true, flush: 'sync' })

  // The stage switched to another share or camera: the window follows; it
  // closes once nothing is on the stage.
  watch(() => voice.stage?.stream || null, stream => {
    if (!stream) {
      openingGeneration++
      if (video) video.srcObject = null
    }
    if (!active.value || !video) return
    if (stream) setSource(video, stream)
    else exit()
  })

  watch(() => voice.isConnected, connected => {
    if (!connected) {
      openingGeneration++
      if (active.value) exit()
      else if (video) video.srcObject = null
    }
  })

  if (getCurrentScope()) {
    onScopeDispose(() => {
      openingGeneration++
      if (active.value) exit()
      if (video) {
        video.removeEventListener('leavepictureinpicture', onLeave)
        video.removeEventListener('enterpictureinpicture', onEnter)
        video.srcObject = null
      }
      if (hostVideo === video) hostVideo = null
      video = null
    })
  }

  return { active }
}
