import { computed, ref } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { toValue } from 'vue'
import { useAutoHide } from './useAutoHide'
import { useCompactLayout } from './useCompactLayout'
import { readItem, writeItem } from '../lib/voicePlayback'

const HINT_KEY = 'mnema_talk_controls_hint'
/** How many times the "move the mouse" hint is shown before the member knows. */
export const MOVE_HINT_SESSIONS = 3

/**
 * The Talk's floating controls (call bar, stage overlays) fade out after 3 s
 * without activity in the Talk view; never in a preview, never while a menu
 * from them is open or the pointer or keyboard focus is on them.
 *
 * Alone in the call there is nothing to look at but the bar, so it stays. With
 * others, a hint says how to get hidden controls back: a tap on a phone, and on
 * a desktop a few times (until it is known) that moving the mouse does.
 */
export function useTalkControls({ connected, others, menuOpen }: {
  connected: MaybeRefOrGetter<boolean>
  /** How many people are in the call, the member included. */
  others: MaybeRefOrGetter<number>
  /** A context menu of the stage is open. */
  menuOpen: MaybeRefOrGetter<boolean>
}) {
  const barPinned = ref(false)
  const overlayHover = ref(false)
  const qualityMenuOpen = ref(false)
  const { isCompact } = useCompactLayout()
  const alone = computed(() => toValue(others) <= 1)
  const controls = useAutoHide({
    enabled: () => toValue(connected) && !alone.value,
    pinned: () => barPinned.value || overlayHover.value || qualityMenuOpen.value || toValue(menuOpen)
  })
  const controlsShown = computed(() => controls.visible.value)
  const hiding = computed(() => toValue(connected) && !controlsShown.value)
  const showTapHint = computed(() => isCompact.value && hiding.value)
  // Counted once per page load, when the hint first appears.
  const hintSessions = Number.parseInt(readItem(HINT_KEY) ?? '0', 10) || 0
  let counted = false
  const showMoveHint = computed(() => {
    if (isCompact.value || !hiding.value || hintSessions >= MOVE_HINT_SESSIONS) return false
    if (!counted) {
      counted = true
      writeItem(HINT_KEY, String(hintSessions + 1))
    }
    return true
  })

  // A tap on a tile while the controls are hidden only brings them back: it
  // must not also open the profile under them. Pointerdown (capture) notes
  // that they were hidden, before the activity shows them.
  let tapRevealed = false
  function onTapStart(e: PointerEvent) {
    tapRevealed = e.pointerType === 'touch' && toValue(connected) && !controlsShown.value
  }
  function onTapCapture(e: MouseEvent) {
    if (!tapRevealed) return
    tapRevealed = false
    if (e.target instanceof Element && e.target.closest('[data-participant-tile]')) {
      e.preventDefault()
      e.stopPropagation()
    }
  }
  return { barPinned, overlayHover, qualityMenuOpen, controlsShown, showTapHint, showMoveHint, onActivity: () => controls.show(), onTapStart, onTapCapture }
}
