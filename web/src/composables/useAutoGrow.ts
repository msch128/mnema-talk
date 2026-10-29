import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { growHeight } from '../lib/composerFiles'

/**
 * Lets a textarea grow with its text, up to about half the window, and scroll
 * inside beyond that. It shrinks again when the text does (after sending, a
 * loaded draft). `multiline` is true once it is taller than its first line.
 */
export function useAutoGrow(el: Ref<HTMLTextAreaElement | null>, text: Ref<string>) {
  const multiline = ref(false)
  let oneLine = 0

  function fit() {
    const area = el.value
    if (!area) return
    area.style.height = 'auto'
    const { height, scrolls } = growHeight(area.scrollHeight, window.innerHeight)
    if (!oneLine && height) oneLine = height
    area.style.height = `${height}px`
    area.style.overflowY = scrolls ? 'auto' : 'hidden'
    multiline.value = oneLine > 0 && height > oneLine + 2
  }

  onMounted(() => {
    fit()
    window.addEventListener('resize', fit)
  })
  onBeforeUnmount(() => window.removeEventListener('resize', fit))
  // After the DOM took the new value, so scrollHeight is the new one.
  watch(text, fit, { flush: 'post' })
  return { multiline, fit }
}
