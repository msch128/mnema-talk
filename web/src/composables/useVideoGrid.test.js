import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref, nextTick, effectScope } from 'vue'
import { useVideoGrid } from './useVideoGrid'

// A ResizeObserver the test drives: resize(el, w, h) reports a new size.
let observers
class FakeResizeObserver {
  constructor(cb) {
    this.cb = cb
    this.els = new Set()
    observers.push(this)
  }
  observe(el) { this.els.add(el) }
  unobserve(el) { this.els.delete(el) }
  disconnect() { this.els.clear(); this.disconnected = true }
}
function resize(el, width, height) {
  for (const o of observers) {
    if (o.els.has(el)) o.cb([{ target: el, contentRect: { width, height } }])
  }
}
function box(width = 0, height = 0) {
  const el = document.createElement('div')
  Object.defineProperty(el, 'clientWidth', { configurable: true, get: () => width })
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => height })
  return el
}

function setup(el, opts) {
  const scope = effectScope()
  const container = ref(el)
  const grid = scope.run(() => useVideoGrid(container, opts))
  return { scope, container, grid }
}

beforeEach(() => {
  observers = []
  vi.stubGlobal('ResizeObserver', FakeResizeObserver)
})
afterEach(() => vi.unstubAllGlobals())

describe('useVideoGrid', () => {
  it('measures the container at once and lays out the tiles', async () => {
    const { grid } = setup(box(1600, 900), { count: 4, gap: 0 })
    await nextTick()
    expect(grid.layout.value).toMatchObject({ cols: 2, rows: 2, tileWidth: 800, tileHeight: 450 })
    expect(grid.tileStyle.value).toEqual({ width: '800px', height: '450px' })
    expect(grid.gridStyle.value).toEqual({ width: '1600px', gap: '0px' })
  })

  it('follows the container size', async () => {
    const el = box(1600, 900)
    const { grid } = setup(el, { count: 2, gap: 12 })
    await nextTick()
    expect(grid.layout.value).toMatchObject({ cols: 2, rows: 1, tileWidth: 794 })
    resize(el, 600, 1200)
    expect(grid.layout.value).toMatchObject({ cols: 1, rows: 2, tileWidth: 600 })
    resize(el, 2000, 400)
    expect(grid.layout.value).toMatchObject({ cols: 2, rows: 1, tileWidth: 711, tileHeight: 399 })
    expect(grid.tileStyle.value).toEqual({ width: '711px', height: '399px' })
  })

  it('follows the tile count and the options given as getters', async () => {
    const count = ref(1)
    const cap = ref(Infinity)
    const { grid } = setup(box(1600, 900), { count, gap: 0, maxTileWidth: () => cap.value })
    await nextTick()
    expect(grid.layout.value.tileWidth).toBe(1600)
    cap.value = 640
    expect(grid.layout.value.tileWidth).toBe(640)
    count.value = 3
    expect(grid.layout.value.cols * grid.layout.value.rows).toBeGreaterThanOrEqual(3)
  })

  it('has no sizes before the container is laid out', async () => {
    const { grid } = setup(box(0, 0), { count: 3 })
    await nextTick()
    expect(grid.layout.value.tileWidth).toBe(0)
    expect(grid.tileStyle.value).toEqual({})
    expect(grid.gridStyle.value).toEqual({})
  })

  it('observes a container that appears later and forgets one that goes', async () => {
    const { container, grid } = setup(null, { count: 1, gap: 0 })
    await nextTick()
    expect(grid.layout.value.tileWidth).toBe(0)
    const el = box(800, 450)
    container.value = el
    await nextTick()
    expect(grid.layout.value.tileWidth).toBe(800)
    const first = observers[observers.length - 1]
    container.value = null
    await nextTick()
    expect(first.disconnected).toBe(true)
    expect(grid.size.value).toEqual({ width: 0, height: 0 })
  })

  it('stops observing with its scope', async () => {
    const { scope } = setup(box(800, 450), { count: 1 })
    await nextTick()
    scope.stop()
    expect(observers.every(o => o.disconnected)).toBe(true)
  })

  it('works without ResizeObserver (measures once)', async () => {
    vi.stubGlobal('ResizeObserver', undefined)
    const { grid } = setup(box(800, 450), { count: 1, gap: 0 })
    await nextTick()
    expect(grid.layout.value.tileWidth).toBe(800)
  })
})
