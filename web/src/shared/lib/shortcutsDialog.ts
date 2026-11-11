import { onBeforeUnmount, onMounted, ref } from 'vue'
import { isTypingEvent } from './typingTarget'

/** Whether the keyboard shortcuts dialog is open; the account menu and the keys open it, App.vue shows it. */
export const shortcutsOpen = ref(false)

export function openShortcuts() {
  shortcutsOpen.value = true
}

/**
 * Ctrl/Cmd+/ opens (and closes) the list from anywhere, "?" opens it when no
 * text is being typed. Layouts that type "/" with Shift or AltGr still match
 * because the key itself is read, not its position.
 */
export function isShortcutsKey(e: KeyboardEvent): boolean {
  if (e.isComposing) return false
  if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key === '/') return true
  return e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey && !isTypingEvent(e)
}

export function useShortcutsKey(enabled: () => boolean) {
  function onKeydown(e: KeyboardEvent) {
    if (!enabled() || !isShortcutsKey(e)) return
    // A bare "?" does not stack the list on top of another dialog.
    if (e.key === '?' && document.querySelector('[role="dialog"]')) return
    e.preventDefault()
    shortcutsOpen.value = e.key === '?' ? true : !shortcutsOpen.value
  }
  onMounted(() => window.addEventListener('keydown', onKeydown))
  onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
}
