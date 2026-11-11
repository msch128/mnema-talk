import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { growHeight } from '../lib/composerFiles'

/** The height of one line of the textarea: line height plus padding, whatever its content. */
function lineBox(area: HTMLTextAreaElement) {
  const style = getComputedStyle(area)
  const line = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.2
  return (line || 0) + (parseFloat(style.paddingTop) || 0) + (parseFloat(style.paddingBottom) || 0)
}

/**
 * Lets a textarea grow with its text, up to about half the window, and scroll
 * inside beyond that. It shrinks again when the text does (after sending, a
 * loaded draft). `multiline` is true once it is taller than its first line.
 */
export function useAutoGrow(el: Ref<HTMLTextAreaElement | null>, text: Ref<string>) {
  const multiline = ref(false)
  let width = 0

  function fit() {
    const area = el.value
    if (!area) return
    area.style.height = 'auto'
    const { height, scrolls } = growHeight(area.scrollHeight, window.innerHeight)
    const oneLine = lineBox(area)
    area.style.height = `${height}px`
    area.style.overflowY = scrolls ? 'auto' : 'hidden'
    multiline.value = oneLine > 0 && height > oneLine * 1.5
  }

  // Wrapping changes with the width (a panel opens, a sidebar resizes); our own
  // height changes must not loop, so only a new width refits.
  let observer: ResizeObserver | undefined
  onMounted(() => {
    fit()
    window.addEventListener('resize', fit)
    if (!el.value || typeof ResizeObserver === 'undefined') return
    observer = new ResizeObserver(([entry]) => {
      const next = Math.round(entry?.contentRect.width ?? 0)
      if (next === width) return
      width = next
      fit()
    })
    observer.observe(el.value)
  })
  onBeforeUnmount(() => {
    window.removeEventListener('resize', fit)
    observer?.disconnect()
  })
  // After the DOM took the new value, so scrollHeight is the new one.
  watch(text, fit, { flush: 'post' })
  return { multiline, fit }
}
