import { playSoundEffect } from '../../lib/soundEffects'
import type { useVoiceStore } from '../../stores/voice'
import { isTypingEvent } from '../../lib/typingTarget'

type VoiceStore = ReturnType<typeof useVoiceStore>

interface PushToTalkHooks {
  /** The gaming microphone lease follows the key (see lib/gamingMicLease). */
  lease: (pressed: boolean) => void
  /** The page became visible again; inCall: this member is connected to a voice channel. */
  wake: (inCall: boolean) => void
}

function matchesPttKey(e: KeyboardEvent, pttKey: string): boolean {
  return (e.code || e.key) === pttKey
}

function shouldHandlePtt(store: VoiceStore | null, e: KeyboardEvent): boolean {
  if (!store) return false
  if (store.inputMode !== 'ptt') return false
  if (e.repeat) return false
  if (isTypingEvent(e)) return false
  return matchesPttKey(e, store.pttKey)
}

function playPttSound(store: VoiceStore, effect: 'ptt_start' | 'ptt_stop') {
  if (store.isConnected || store.isMicTesting) {
    playSoundEffect(effect)
  }
}

/**
 * Push-to-talk key handling. The listeners are on the window, so one instance
 * serves the whole page and any component can detach it.
 */
export function createPushToTalk({ lease, wake }: PushToTalkHooks) {
  let store: VoiceStore | null = null

  function onKeyDown(e: KeyboardEvent) {
    if (!store || !shouldHandlePtt(store, e)) return
    lease(true)
    if (store.isPttPressed) return
    store.isPttPressed = true
    playPttSound(store, 'ptt_start')
  }

  function onKeyUp(e: KeyboardEvent) {
    if (!store || store.inputMode !== 'ptt') return
    if (!matchesPttKey(e, store.pttKey) || !store.isPttPressed) return
    store.isPttPressed = false
    lease(false)
    playPttSound(store, 'ptt_stop')
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
