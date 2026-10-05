// Closes a popover (menu, picker, palette) on a pointer press outside of it
// or on Escape. Listeners are only registered while the popover is open and
// are always removed again, also when the owning component goes away.
import { watch, toValue, getCurrentScope, onScopeDispose } from 'vue'

function asList(value) {
  if (!value) return []
  if (typeof value[Symbol.iterator] === 'function' && !(value instanceof Node)) {
    return [...value].filter(Boolean)
  }
  return [value]
}

function isInside(e, roots) {
  if (!roots.length) return false
  const path = typeof e.composedPath === 'function' ? e.composedPath() : []
  if (path.length) return roots.some(root => path.includes(root))
  return roots.some(root => root.contains?.(e.target))
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
export function useDismissable(target, onClose, { active = true, escape = true } = {}) {
  let listening = false

  function onPointerDown(e) {
    if (isInside(e, asList(toValue(target)))) return
    onClose(e, 'outside')
  }

  function onKeydown(e) {
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
