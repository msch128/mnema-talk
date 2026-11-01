// How big my picture of someone's camera is (the tile or the stage showing
// it), in device pixels, so the server sends the smallest layer of their
// camera that still has the pixels the picture shows. A person is shown in
// several places; the store keeps the largest (see stores/voice/cameraFit).
import { onMounted, onUnmounted, ref, watch } from 'vue'
import type { Ref } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { pictureHeightPx } from '../lib/cameraLayers'
import { useElementSize } from './useElementSize'

let places = 0

/**
 * `userId` is whose camera the element shows (null: none, or my own, which
 * nobody sends back); `element` is what shows it; every call needs a `place`
 * of its own, e.g. "tile" and "stage".
 */
export function useCameraFit(userId: () => string | null, element: Ref<HTMLElement | null>, place: string) {
  const voice = useVoiceStore()
  const size = useElementSize(element)
  // The pixel ratio changes with the browser zoom and when the window moves to another screen.
  const pixelRatio = ref(window.devicePixelRatio || 1)
  const syncRatio = () => { pixelRatio.value = window.devicePixelRatio || 1 }
  onMounted(() => window.addEventListener('resize', syncRatio))
  const key = `${place}-${++places}`

  let reported: string | null = null
  function report(id: string | null, pixels: number) {
    if (reported && reported !== id) voice.setCameraSize(reported, key, 0)
    reported = pixels > 0 ? id : null
    if (id) voice.setCameraSize(id, key, pixels)
  }

  // The picture the element shows is at most as tall as the element and as its width allows.
  function shownHeight(): number {
    const el = element.value
    const aspect = el instanceof HTMLVideoElement && el.videoWidth > 0 ? el.videoHeight / el.videoWidth : 9 / 16
    return Math.min(size.value.height, size.value.width * aspect)
  }

  watch([userId, size, pixelRatio], () => {
    const id = userId()
    report(id, id ? pictureHeightPx(shownHeight(), pixelRatio.value) : 0)
  }, { immediate: true, deep: true })

  onUnmounted(() => {
    window.removeEventListener('resize', syncRatio)
    report(null, 0)
  })
}
