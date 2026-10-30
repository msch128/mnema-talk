// The state of one picture: shown, gone (deleted from storage) or failed (try
// again). Bind `onError` to the <img>, render `state`, and use `attempt` as the
// element's key so a retry loads it anew.
import { computed, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { classifyMediaFailure, knownGone } from '../lib/mediaStatus'

export type MediaState = 'ok' | 'gone' | 'failed' | 'none'

export function useMediaImage(src: MaybeRefOrGetter<string | null | undefined>, deleted: MaybeRefOrGetter<boolean> = false) {
  const failure = ref<'gone' | 'failed' | 'none' | null>(null)
  const attempt = ref(0)
  // The address the failure belongs to: a late answer for an earlier picture is dropped.
  let asked = ''

  watch(() => toValue(src), () => {
    failure.value = null
    asked = ''
  })

  const state = computed<MediaState>(() => {
    const url = toValue(src)
    if (toValue(deleted) || (url && knownGone(url))) return 'gone'
    return failure.value ?? 'ok'
  })

  async function onError() {
    const url = toValue(src)
    if (!url || state.value !== 'ok') return
    asked = url
    const result = await classifyMediaFailure(url)
    if (asked === url && toValue(src) === url) failure.value = result
  }

  function retry() {
    failure.value = null
    asked = ''
    attempt.value += 1
  }

  return { state, attempt, onError, retry }
}
