// How good a layer my picture of a screen share can use: the stage's size in
// device pixels (CSS pixels times the pixel ratio, so fullscreen on a 4K
// screen asks for the source) against the layers the sharer has. The server
// keeps Auto at or below that layer and lets it climb when the picture grows.
// A share I watch but that is not on the stage needs only the smallest layer.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { fitLayer } from '../lib/screenLayers'
import { useElementSize } from './useElementSize'

/** A size change must hold this long before it is sent, so resizing a window is one message. */
export const FIT_SETTLE_MS = 400

export function useScreenFit(stageUserId: () => string | null, stageEl: Ref<HTMLElement | null>) {
  const voice = useVoiceStore()
  const size = useElementSize(stageEl)
  // The pixel ratio changes with the browser zoom and when the window moves to another screen.
  const pixelRatio = ref(window.devicePixelRatio || 1)
  const syncRatio = () => { pixelRatio.value = window.devicePixelRatio || 1 }
  onMounted(() => window.addEventListener('resize', syncRatio))
  onUnmounted(() => window.removeEventListener('resize', syncRatio))

  const wanted = computed(() => {
    const out: Record<string, number> = {}
    const onStage = stageUserId()
    for (const id of Object.keys(voice.watchedScreens)) {
      const layers = voice.screenLayers[id] ?? []
      out[id] = id === onStage
        ? fitLayer(layers, size.value.height, pixelRatio.value)
        : Math.max(layers.length - 1, 0)
    }
    return out
  })

  let timer: ReturnType<typeof setTimeout> | null = null
  let sentOnce = false
  function send() {
    timer = null
    sentOnce = true
    for (const [id, fit] of Object.entries(wanted.value)) voice.setScreenFit(id, fit)
  }
  watch(wanted, () => {
    if (timer) clearTimeout(timer)
    // The first measurement goes out at once; later changes settle first.
    timer = setTimeout(send, sentOnce ? FIT_SETTLE_MS : 0)
  }, { immediate: true, deep: true })
  onUnmounted(() => { if (timer) clearTimeout(timer) })
  return { wanted }
}
