import { computed, ref } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { toValue } from 'vue'
import { useAutoHide } from './useAutoHide'
import { useCompactLayout } from './useCompactLayout'

/**
 * The Talk's floating controls (call bar, stage overlays) fade out after 3 s
 * without activity in the Talk view; never in a preview, never while a menu
 * from them is open or the pointer or keyboard focus is on them.
 *
 * On a phone only a tap brings hidden controls back, so alone in the call (when
 * there is nothing to look at but the bar) they stay, and while hidden a hint
 * says how to get them back.
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
  const aloneOnPhone = computed(() => isCompact.value && toValue(others) <= 1)
  const controls = useAutoHide({
    enabled: () => toValue(connected) && !aloneOnPhone.value,
    pinned: () => barPinned.value || overlayHover.value || qualityMenuOpen.value || toValue(menuOpen)
  })
  const controlsShown = computed(() => controls.visible.value)
  const showTapHint = computed(() => isCompact.value && toValue(connected) && !controlsShown.value)
  return { barPinned, overlayHover, qualityMenuOpen, controlsShown, showTapHint, onActivity: () => controls.show() }
}
