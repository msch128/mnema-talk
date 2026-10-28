// Keys of a message composer, shared by the channel chat and the thread panel.
import { onMounted, onUnmounted } from 'vue'
import type { Ref } from 'vue'
import { isTypingEvent } from '../lib/typingTarget'

interface ComposerKeys {
  /** Handlers that may consume the key first (suggestion lists); each returns true when it did. */
  consumers: ((e: KeyboardEvent) => boolean)[]
  replying: () => boolean
  cancelReply: () => void
  send: () => void
}

/** Enter pressed while an IME composition is active (keyCode 229 on older WebKit). */
export function isComposing(e: KeyboardEvent): boolean {
  return e.isComposing || e.keyCode === 229
}

/** Escape leaves a reply; Enter sends, Shift+Enter breaks the line. */
export function handleComposerKeydown(e: KeyboardEvent, { consumers, replying, cancelReply, send }: ComposerKeys) {
  if (consumers.some(consume => consume(e))) return
  if (e.key === 'Escape' && replying()) {
    e.preventDefault()
    cancelReply()
    return
  }
  // Enter that confirms an IME candidate is part of the composition, not a send.
  if (e.key === 'Enter' && !e.shiftKey && !isComposing(e)) {
    e.preventDefault()
    send()
  }
}

interface EscapeMarksRead {
  composer: Ref<HTMLTextAreaElement | null>
  /** Closes an open menu; returns true when one was open (it keeps the key). */
  closeMenu: () => boolean
  /** True while something else owns Escape: a lightbox, an editor, a reply in the composer. */
  occupied: () => boolean
  draft: () => string
  markAllRead: () => boolean
}

function isExternalInputTarget(e: KeyboardEvent, composerElement: HTMLTextAreaElement | null): boolean {
  if (e.target === composerElement) return false
  return isTypingEvent(e)
}

function hasActiveDraftOrDialog(draftText: string): boolean {
  if (draftText.trim()) return true
  return Boolean(document.querySelector('[role="dialog"]'))
}

/** Escape anywhere in the channel marks it read, unless something else is using the key. */
export function useEscapeMarksRead({ composer, closeMenu, occupied, draft, markAllRead }: EscapeMarksRead) {
  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || closeMenu()) return
    if (e.defaultPrevented || occupied()) return
    if (hasActiveDraftOrDialog(draft())) return
    if (isExternalInputTarget(e, composer.value)) return
    if (markAllRead()) e.preventDefault()
  }
  onMounted(() => window.addEventListener('keydown', onKeydown))
  onUnmounted(() => window.removeEventListener('keydown', onKeydown))
}
