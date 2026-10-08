// Closes a popover (menu, picker, palette) on a pointer press outside of it
// or on Escape. Listeners are only registered while the popover is open and
// are always removed again, also when the owning component goes away.
import { watch, toValue, getCurrentScope, onScopeDispose } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

type DismissTarget = Element | Iterable<Element | null> | null | undefined
type DismissReason = 'outside' | 'escape'

function asList(value: DismissTarget): Element[] {
  if (!value) return []
  if (value instanceof Element) return [value]
  return [...value].filter((item): item is Element => item instanceof Element)
}

function isInside(e: Event, roots: Element[]) {
  if (!roots.length) return false
  const path = typeof e.composedPath === 'function' ? e.composedPath() : []
  if (path.length) return roots.some(root => path.includes(root))
  return roots.some(root => e.target instanceof Node && root.contains(e.target))
}

/**
 * @param target  ref, getter or element(s) that count as "inside": an
 *                Element, a list of Elements, or null.
 * @param onClose called with (event, reason) where reason is 'outside' or 'escape'.
 * @param options.active  ref/getter: listen only while truthy (default: always).
 * @param options.escape  close on Escape (default true). Escape is taken in the
 *                        capture phase so one press closes only the popover,
 *                        not whatever sits behind it.
 */
export function useDismissable(target: MaybeRefOrGetter<DismissTarget>, onClose: (event: Event, reason: DismissReason) => void, { active = true, escape = true }: { active?: MaybeRefOrGetter<boolean>; escape?: boolean } = {}) {
  let listening = false

  function onPointerDown(e: PointerEvent) {
    if (isInside(e, asList(toValue(target)))) return
    onClose(e, 'outside')
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape' || e.isComposing) return
    e.preventDefault()
    e.stopPropagation()
    onClose(e, 'escape')
  }

  function start() {
    if (listening || typeof document === 'undefined') return
    listening = true
    document.addEventListener('pointerdown', onPointerDown, true)
    if (escape) document.addEventListener('keydown', onKeydown, true)
  }

  function stop() {
    if (!listening) return
    listening = false
    document.removeEventListener('pointerdown', onPointerDown, true)
    document.removeEventListener('keydown', onKeydown, true)
  }

  const stopWatch = watch(() => !!toValue(active), on => (on ? start() : stop()), { immediate: true, flush: 'sync' })

  if (getCurrentScope()) {
    onScopeDispose(() => {
      stopWatch()
      stop()
    })
  }

  return { stop: () => { stopWatch(); stop() } }
}
