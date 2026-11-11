import { ref, watch } from 'vue'

/**
 * A text draft shared with a parent through v-model. Typing updates it at
 * once (suggestions read it in the same tick, which a plain model prop
 * cannot do); the parent follows and may also set it.
 */
export function useDraftModel(get: () => string, set: (text: string) => void) {
  const text = ref(get())
  watch(get, value => { text.value = value })
  watch(text, set, { flush: 'sync' })
  return text
}
