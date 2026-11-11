import { ref, watch, toValue, onScopeDispose, getCurrentScope } from 'vue'
import type { MaybeRefOrGetter } from 'vue'

export const AUTO_HIDE_DELAY = 3000

/**
 * Controls that fade out after `delay` ms without activity, like the Talk's
 * floating control bar (Discord's call controls).
 *
 * - `show()` is the activity signal (mouse move, touch, keyboard focus): the
 *   controls appear and the countdown restarts.
 * - While `enabled` is false (e.g. a preview, nobody connected) they never hide.
 * - While `pinned` is true (a menu from them is open, keyboard focus or the
 *   pointer is on them) they never hide; the countdown restarts once unpinned.
 *
 * `enabled` and `pinned` are values, refs or getters.
 */
export function useAutoHide({ delay = AUTO_HIDE_DELAY, enabled = true, pinned = false }: { delay?: MaybeRefOrGetter<number>; enabled?: MaybeRefOrGetter<boolean>; pinned?: MaybeRefOrGetter<boolean> } = {}) {
  const visible = ref(true)
  let timer: ReturnType<typeof setTimeout> | null = null

  function clear() {
    if (timer !== null) clearTimeout(timer)
    timer = null
  }

  function arm() {
    clear()
    if (!toValue(enabled)) return
    timer = setTimeout(() => {
      timer = null
      if (!toValue(enabled)) return
      // Pinned: keep showing and look again later (pinned may not be reactive).
      if (toValue(pinned)) {
        arm()
        return
      }
      visible.value = false
    }, toValue(delay))
  }

  function show() {
    visible.value = true
    arm()
  }

  watch(() => !!toValue(enabled), on => {
    visible.value = true
    if (on) arm()
    else clear()
  }, { immediate: true })

  watch(() => !!toValue(pinned), on => {
    if (on) {
      visible.value = true
      clear()
    } else {
      arm()
    }
  })

  if (getCurrentScope()) onScopeDispose(clear)

  return { visible, show }
}
