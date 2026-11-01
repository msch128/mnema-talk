import { onBeforeUnmount, onMounted } from 'vue'
import type { Ref } from 'vue'
import { useVoiceStore } from '../stores/voice'

/**
 * Tells the voice store which participants' tiles are on screen in the Talk
 * view, so only their cameras are received (see stores/voice/cameraVisibility).
 * A tile counts while any part of it is visible, wherever the view scrolls or
 * clips it. Without IntersectionObserver nothing is reported and every camera
 * is received. Leaving the view while in the call reports nobody: the cameras
 * of a view that is not shown are not worth the bandwidth.
 */
export function useVisibleCameras(root: Ref<HTMLElement | null>) {
  const voice = useVoiceStore()
  const seen = new Map<Element, boolean>()
  let observer: IntersectionObserver | null = null
  let mutations: MutationObserver | null = null
  let reported: string | null = null

  function report() {
    const ids = new Set<string>()
    for (const [tile, visible] of seen) {
      const id = tile instanceof HTMLElement ? tile.dataset['userId'] : undefined
      if (visible && id) ids.add(id)
    }
    // The view changes all the time (chat, reactions); only a changed set is news.
    const key = [...ids].sort().join(',')
    if (key === reported) return
    reported = key
    voice.setVisibleCameras([...ids])
  }

  // Follows the tiles that come and go.
  function scan() {
    const tiles = new Set(root.value?.querySelectorAll('[data-participant-tile]') ?? [])
    for (const tile of seen.keys()) {
      if (tiles.has(tile)) continue
      observer?.unobserve(tile)
      seen.delete(tile)
    }
    for (const tile of tiles) {
      if (seen.has(tile)) continue
      seen.set(tile, false)
      observer?.observe(tile)
    }
    report()
  }

  onMounted(() => {
    if (!root.value || typeof IntersectionObserver === 'undefined') return
    observer = new IntersectionObserver(entries => {
      for (const entry of entries) if (seen.has(entry.target)) seen.set(entry.target, entry.isIntersecting)
      report()
    })
    mutations = new MutationObserver(scan)
    mutations.observe(root.value, { childList: true, subtree: true })
    scan()
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    mutations?.disconnect()
    seen.clear()
    reported = null
    // Still in the call, but not looking at it: nothing is on screen.
    voice.setVisibleCameras(observer && voice.isConnected ? [] : null)
  })
}
