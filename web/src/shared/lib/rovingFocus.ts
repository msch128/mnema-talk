// Arrow-key behaviour shared by radio groups and tab lists: the selected item
// is the only tab stop, the arrows move between items (wrapping), Home/End jump.
import { nextTick } from 'vue'

/** The index an arrow/Home/End key moves to, or null when the key is not a navigation key. */
export function rovingTarget(key: string, index: number, count: number): number | null {
  if (count <= 0) return null
  switch (key) {
    case 'ArrowRight':
    case 'ArrowDown':
      return (index + 1) % count
    case 'ArrowLeft':
    case 'ArrowUp':
      return (index - 1 + count) % count
    case 'Home':
      return 0
    case 'End':
      return count - 1
    default:
      return null
  }
}

/** Handles a keydown on item `index` of a group: picks and focuses the target item. */
export function rovingKeydown(e: KeyboardEvent, index: number, count: number, pick: (target: number) => void) {
  const target = rovingTarget(e.key, index, count)
  if (target === null) return
  e.preventDefault()
  const group = (e.currentTarget as HTMLElement | null)?.parentElement
  pick(target)
  void nextTick(() => (group?.children[target] as HTMLElement | undefined)?.focus())
}
