import { ref, computed, watch, toValue, onScopeDispose, getCurrentScope } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { videoGridLayout, videoGridSize } from '../lib/videoGrid'

/**
 * The Talk's participant grid: watches the size of `container` (a ref to the
 * element the grid fills) and lays out `count` 16:9 tiles as large as fit.
 * `count`, `gap`, `maxTileWidth` and `minTileWidth` are values, refs or
 * getters. Returns the layout plus ready-made styles for the grid and a tile;
 * both are empty until the container has a size (CSS fallbacks apply).
 */
export function useVideoGrid(container: MaybeRefOrGetter<HTMLElement | null>, { count = 0, gap = 12, maxTileWidth = Infinity, minTileWidth = 0 }: { count?: MaybeRefOrGetter<number>; gap?: MaybeRefOrGetter<number>; maxTileWidth?: MaybeRefOrGetter<number>; minTileWidth?: MaybeRefOrGetter<number> } = {}) {
  const size = ref({ width: 0, height: 0 })
  let observer: ResizeObserver | null = null

  function measure(el: HTMLElement | null) {
    if (!el) return
    const width = el.clientWidth || 0
    const height = el.clientHeight || 0
    if (width !== size.value.width || height !== size.value.height) size.value = { width, height }
  }

  function observe(el: HTMLElement | null) {
    observer?.disconnect()
    observer = null
    if (!el) {
      size.value = { width: 0, height: 0 }
      return
    }
    measure(el)
    if (typeof ResizeObserver !== 'function') return
    observer = new ResizeObserver(entries => {
      const box = entries[entries.length - 1]?.contentRect
      if (box) {
        const width = Math.floor(box.width)
        const height = Math.floor(box.height)
        if (width !== size.value.width || height !== size.value.height) size.value = { width, height }
      } else {
        measure(el)
      }
    })
    observer.observe(el)
  }

  // The element comes and goes with v-if (grid vs. stage).
  watch(() => toValue(container), observe, { immediate: true, flush: 'post' })
  if (getCurrentScope()) onScopeDispose(() => observer?.disconnect())

  const layout = computed(() => videoGridLayout({
    count: toValue(count),
    width: size.value.width,
    height: size.value.height,
    gap: toValue(gap),
    maxTileWidth: toValue(maxTileWidth),
    minTileWidth: toValue(minTileWidth)
  }))

  const gridStyle = computed(() => {
    const l = layout.value
    if (!l.tileWidth) return {}
    const g = toValue(gap)
    // As wide as `cols` tiles: the flex row wraps after them and centers
    // the last, shorter row like Discord does.
    return { width: `${videoGridSize(l, g).width}px`, gap: `${g}px` }
  })

  const tileStyle = computed(() => {
    const l = layout.value
    if (!l.tileWidth) return {}
    return { width: `${l.tileWidth}px`, height: `${l.tileHeight}px` }
  })

  return { size, layout, gridStyle, tileStyle }
}
