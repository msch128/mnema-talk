import type { Ref } from 'vue'
import type { VoiceStore } from '../stores/voice'
import type { TalkParticipant } from '../components/presentationTypes'
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useVoiceStore } from '../stores/voice'

// A double-click on a camera puts it on the stage in full screen. Its first
// click already put it on the stage (or took it off) without waiting, and
// moved the tiles: the second click and the dblclick may land elsewhere, so
// the camera clicked last decides.
const DOUBLE_CLICK_MS = 600

function isTypingTarget(el: EventTarget | null) {
  return el instanceof HTMLElement && (el.isContentEditable || !!el.closest?.('input, textarea, select, [contenteditable]:not([contenteditable="false"])'))
}

interface Options {
  /** The element that goes full screen (the stage), once there is one. */
  container: () => HTMLElement | null
  /** The Talk view: the F key only works while the focus is in it (or nowhere). */
  root: Ref<HTMLElement | null>
  connected: () => boolean
  stage: () => VoiceStore['stage']
  toggleCameraFocus: (user: TalkParticipant) => void
}

/** Full screen of the Talk's stage: its button, a double-click and the F key. */
export function useStageFullscreen({ container, root, connected, stage, toggleCameraFocus }: Options) {
  const voiceStore = useVoiceStore()
  const isFullscreen = ref(false)

  function updateFullscreen() {
    isFullscreen.value = !!document.fullscreenElement && document.fullscreenElement === container()
  }

  async function enterFullscreen() {
    await nextTick() // the stage may only just have appeared
    const el = container()
    if (!el || document.fullscreenElement === el || typeof el.requestFullscreen !== 'function') return
    Promise.resolve(el.requestFullscreen()).catch(() => {})
  }

  function toggleFullscreen() {
    if (document.fullscreenElement) Promise.resolve(document.exitFullscreen?.()).catch(() => {})
    else enterFullscreen()
  }

  let lastCameraClick: { userId: string; at: number } | null = null
  function onTileFocusCamera(user: TalkParticipant) {
    lastCameraClick = { userId: user.id, at: Date.now() }
    toggleCameraFocus(user)
  }
  function recentCameraClick() {
    return lastCameraClick && Date.now() - lastCameraClick.at < DOUBLE_CLICK_MS ? lastCameraClick.userId : null
  }
  function fullscreenCamera(userId: string) {
    const id = recentCameraClick() || userId
    lastCameraClick = null
    if (!connected() || !id) return
    const shown = stage()
    if (!(shown?.kind === 'camera' && shown.userId === id)) voiceStore.focusCamera(id)
    const now = stage()
    if (now?.kind === 'camera' && now.userId === id) enterFullscreen()
  }

  // Double-clicks in the Talk area: on the stage they toggle full screen.
  function onAreaDblclick(e: MouseEvent) {
    if ((e.target instanceof Element && e.target.closest('button, input, a, [data-testid="screen-viewers"]'))) return
    const recent = recentCameraClick()
    if (recent) {
      fullscreenCamera(recent)
      return
    }
    if (e.target instanceof Node && container()?.contains(e.target)) toggleFullscreen()
  }

  // F toggles the stage's full screen, like in a video player: while the
  // focus is in the Talk view (or nowhere), not while typing, with a dialog or
  // menu open, or when F is the push-to-talk key.
  function inTalkView(el: EventTarget | null) {
    return !el || el === document.body || el === document.documentElement || el instanceof Node && !!root.value?.contains(el)
  }
  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'f' && e.key !== 'F') return
    if (e.ctrlKey || e.metaKey || e.altKey || e.repeat || e.isComposing || e.defaultPrevented) return
    if (!inTalkView(e.target) || isTypingTarget(e.target)) return
    if ((e.target instanceof Element && e.target.closest('[role="dialog"], [role="menu"]')) || document.querySelector('[aria-modal="true"]')) return
    if (voiceStore.inputMode === 'ptt' && voiceStore.pttKey === e.code) return
    if (!container()) return
    e.preventDefault()
    toggleFullscreen()
  }

  onMounted(() => {
    document.addEventListener('fullscreenchange', updateFullscreen)
    document.addEventListener('keydown', onKeydown)
  })
  onUnmounted(() => {
    document.removeEventListener('fullscreenchange', updateFullscreen)
    document.removeEventListener('keydown', onKeydown)
  })

  return { isFullscreen, enterFullscreen, toggleFullscreen, onTileFocusCamera, fullscreenCamera, onAreaDblclick }
}
