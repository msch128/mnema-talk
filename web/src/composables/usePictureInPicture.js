// Picture-in-Picture for the Talk's stage: whatever is on the stage (a
// screen share or a camera) plays in the browser's floating window and keeps
// playing while the user reads a text channel. The Talk view (VoiceStage) may
// unmount then, so the video in that window is not the stage's own: an
// app-level host (PipHost.vue) owns a hidden <video> that this module feeds
// with the current stage stream. Only an explicit click opens the window.
import { ref, watch, onScopeDispose, getCurrentScope } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

// The window shows the stage right now (shared by the host and the stage).
const active = ref(false)
let hostVideo = null

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
function metadataLoaded(video, timeoutMs = 3000) {
  if (video.readyState >= 1) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const done = ok => {
      clearTimeout(timer)
      video.removeEventListener('loadedmetadata', onLoaded)
      ok ? resolve() : reject(new Error('no video metadata'))
    }
    const onLoaded = () => done(true)
    const timer = setTimeout(() => done(false), timeoutMs)
    video.addEventListener('loadedmetadata', onLoaded)
  })
}

function setSource(video, stream) {
  if (video.srcObject === stream) return
  video.srcObject = stream
  if (stream) Promise.resolve(video.play?.()).catch(() => {})
}

function inWindow() {
  return !!hostVideo && typeof document !== 'undefined' && document.pictureInPictureElement === hostVideo
}

/** Opens the window with the stage. Resolves to whether it opened. */
async function enter() {
  const voice = useVoiceStore()
  const stream = voice.stage?.stream
  const video = hostVideo
  if (!video || !stream || !isPictureInPictureSupported()) return false
  try {
    video.muted = true // the call's audio plays elsewhere
    setSource(video, stream)
    await metadataLoaded(video)
    await video.requestPictureInPicture()
    active.value = true
    return true
  } catch {
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
export function usePictureInPictureHost(videoRef) {
  const voice = useVoiceStore()
  let video = null

  function onLeave() {
    active.value = false
    if (video) video.srcObject = null
  }
  function onEnter() {
    active.value = true
  }

  watch(videoRef, el => {
    if (video) {
      video.removeEventListener('leavepictureinpicture', onLeave)
      video.removeEventListener('enterpictureinpicture', onEnter)
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
    if (!active.value || !video) return
    if (stream) setSource(video, stream)
    else exit()
  })

  watch(() => voice.isConnected, connected => {
    if (!connected && active.value) exit()
  })

  if (getCurrentScope()) {
    onScopeDispose(() => {
      if (active.value) exit()
      if (video) {
        video.removeEventListener('leavepictureinpicture', onLeave)
        video.removeEventListener('enterpictureinpicture', onEnter)
      }
      if (hostVideo === video) hostVideo = null
      video = null
    })
  }

  return { active }
}
