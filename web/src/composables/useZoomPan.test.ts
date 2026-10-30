import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { MAX_SCALE, useZoomPan } from './useZoomPan'

function stage(width = 200, height = 100) {
  const el = document.createElement('div')
  el.getBoundingClientRect = () => ({ left: 0, top: 0, width, height, right: width, bottom: height, x: 0, y: 0, toJSON: () => ({}) })
  el.setPointerCapture = vi.fn()
  return el
}
const pointer = (x: number, y = 0, id = 1, target: EventTarget | null = null) =>
  ({ clientX: x, clientY: y, pointerId: id, button: 0, target: target ?? document.createElement('img') }) as unknown as PointerEvent

describe('useZoomPan', () => {
  it('zooms in and out in steps and never past the limits', () => {
    const zoom = useZoomPan(ref(stage()))
    zoom.zoomIn()
    expect(zoom.scale.value).toBe(1.5)
    for (let i = 0; i < 20; i++) zoom.zoomIn()
    expect(zoom.scale.value).toBe(MAX_SCALE)
    for (let i = 0; i < 20; i++) zoom.zoomOut()
    expect(zoom.scale.value).toBe(1)
    expect(zoom.zoomed.value).toBe(false)
  })

  it('zooms the wheel toward the pointer and double-click toggles', () => {
    const zoom = useZoomPan(ref(stage()))
    zoom.onWheel({ deltaY: -300, clientX: 150, clientY: 50 } as WheelEvent)
    expect(zoom.scale.value).toBeGreaterThan(1)
    expect(zoom.style.value.transform).toContain('scale(')
    zoom.reset()
    zoom.onDoubleClick({ clientX: 100, clientY: 50 } as MouseEvent)
    expect(zoom.scale.value).toBe(2.5)
    zoom.onDoubleClick({ clientX: 100, clientY: 50 } as MouseEvent)
    expect(zoom.scale.value).toBe(1)
  })

  it('zooms around the middle when there is no stage to measure', () => {
    const zoom = useZoomPan(ref(null))
    zoom.onDoubleClick({ clientX: 80, clientY: 80 } as MouseEvent)
    expect(zoom.scale.value).toBe(2.5)
  })

  it('pans a zoomed picture by dragging, and captures only once it moves', () => {
    const el = stage()
    const zoom = useZoomPan(ref(el))
    zoom.onPointerDown(pointer(0))
    zoom.onPointerMove(pointer(50))
    expect(zoom.dragging.value).toBe(false)
    zoom.zoomIn()
    zoom.onPointerDown(pointer(0))
    zoom.onPointerMove(pointer(2))
    expect(el.setPointerCapture).not.toHaveBeenCalled()
    expect(zoom.wasDragged()).toBe(false)
    zoom.onPointerMove(pointer(40, 10))
    expect(el.setPointerCapture).toHaveBeenCalledWith(1)
    expect(zoom.dragging.value).toBe(true)
    zoom.onPointerMove(pointer(60, 20))
    expect(zoom.style.value.transform).toContain('translate3d(60px, 20px')
    zoom.onPointerMove(pointer(999, 999, 2))
    expect(zoom.style.value.transform).toContain('translate3d(60px, 20px')
    zoom.onPointerEnd(pointer(60, 20, 2))
    expect(zoom.dragging.value).toBe(true)
    zoom.onPointerEnd(pointer(60, 20))
    expect(zoom.dragging.value).toBe(false)
    expect(zoom.wasDragged()).toBe(true)
  })

  it('ignores presses on buttons and other mouse buttons', () => {
    const zoom = useZoomPan(ref(stage()))
    zoom.zoomIn()
    const button = document.createElement('button')
    zoom.onPointerDown(pointer(0, 0, 1, button))
    zoom.onPointerMove(pointer(50))
    expect(zoom.dragging.value).toBe(false)
    zoom.onPointerDown({ ...pointer(0), button: 2 } as PointerEvent)
    zoom.onPointerMove(pointer(50))
    expect(zoom.dragging.value).toBe(false)
  })
})
