import { playSoundEffect } from '../../lib/soundEffects'
import type { useVoiceStore } from '../../stores/voice'

type VoiceStore = ReturnType<typeof useVoiceStore>

interface PushToTalkHooks {
  /** The gaming microphone lease follows the key (see lib/gamingMicLease). */
  lease: (pressed: boolean) => void
  /** The page became visible again; inCall: this member is connected to a voice channel. */
  wake: (inCall: boolean) => void
}

function isTypingTarget(el: EventTarget | null) {
  if (!(el instanceof HTMLElement)) return false
  return ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable
}

/**
 * Push-to-talk key handling. The listeners are on the window, so one instance
 * serves the whole page and any component can detach it.
 */
export function createPushToTalk({ lease, wake }: PushToTalkHooks) {
  let store: VoiceStore | null = null

  function onKeyDown(e: KeyboardEvent) {
    if (!store || store.inputMode !== 'ptt' || e.repeat) return
    // Holding the key while typing in the composer must not open the mic.
    if (isTypingTarget(e.target)) return
    if ((e.code || e.key) !== store.pttKey) return
    lease(true)
    if (store.isPttPressed) return
    store.isPttPressed = true
    if (store.isConnected || store.isMicTesting) playSoundEffect('ptt_start')
  }

  function onKeyUp(e: KeyboardEvent) {
    if (!store || store.inputMode !== 'ptt') return
    if ((e.code || e.key) !== store.pttKey || !store.isPttPressed) return
    store.isPttPressed = false
    lease(false)
    if (store.isConnected || store.isMicTesting) playSoundEffect('ptt_stop')
  }

  // A keyup that happens while the window is in the background never arrives;
  // release the key so the mic does not stay open.
  function release() {
    if (store) store.isPttPressed = false
    lease(false)
  }

  function onVisibilityChange() {
    if (document.visibilityState === 'hidden') release()
    else if (document.visibilityState === 'visible') wake(!!(store?.isConnected && store.currentChannelId))
  }

  function detach() {
    window.removeEventListener('keydown', onKeyDown)
    window.removeEventListener('keyup', onKeyUp)
    window.removeEventListener('blur', release)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    release()
    store = null
  }

  function attach(voiceStore: VoiceStore) {
    detach()
    store = voiceStore
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    window.addEventListener('blur', release)
    document.addEventListener('visibilitychange', onVisibilityChange)
  }

  return { attach, detach, attached: () => store !== null }
}
