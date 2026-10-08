import { ref, watch, toValue, onScopeDispose, getCurrentScope } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

/**
 * The inner size (clientWidth × clientHeight) of `target`, a ref to an element
 * that may come and go (v-if). { width: 0, height: 0 } while there is none.
 */
export function useElementSize(target: MaybeRefOrGetter<HTMLElement | null>) {
  const size = ref({ width: 0, height: 0 })
  let observer: ResizeObserver | null = null

  function measure(el: HTMLElement) {
    const width = el.clientWidth || 0
    const height = el.clientHeight || 0
    if (width !== size.value.width || height !== size.value.height) size.value = { width, height }
  }

  watch(() => toValue(target), el => {
    observer?.disconnect()
    observer = null
    if (!el) {
      size.value = { width: 0, height: 0 }
      return
    }
    measure(el)
    if (typeof ResizeObserver !== 'function') return
    observer = new ResizeObserver(() => measure(el))
    observer.observe(el)
  }, { immediate: true, flush: 'post' })

  if (getCurrentScope()) onScopeDispose(() => observer?.disconnect())
  return size
}
