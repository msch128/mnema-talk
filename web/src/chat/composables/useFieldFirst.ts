import { ref, watch, onScopeDispose, getCurrentScope } from 'vue'
import type { Ref } from 'vue'

/**
 * Whether the stylesheet draws `field` first in its flex row (a container
 * query gives it `order-first` when the row wraps). The composer then puts
 * the field first in the DOM as well, so Tab and a screen reader meet it in
 * the order it is seen. Checked again whenever the row changes size.
 */
export function useFieldFirst(row: Ref<HTMLElement | null>, field: Ref<HTMLElement | null>) {
  const fieldFirst = ref(false)
  let observer: ResizeObserver | null = null

  function check() {
    const el = field.value
    fieldFirst.value = !!el && Number.parseInt(getComputedStyle(el).order, 10) < 0
  }

  watch(row, el => {
    observer?.disconnect()
    observer = null
    if (!el) return
    check()
    if (typeof ResizeObserver !== 'function') return
    observer = new ResizeObserver(check)
    observer.observe(el)
  }, { immediate: true, flush: 'post' })

  if (getCurrentScope()) onScopeDispose(() => observer?.disconnect())
  return fieldFirst
}
