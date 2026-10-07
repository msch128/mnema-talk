import { required } from '../store-test-support.fixture'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { defineComponent, effectScope, h, nextTick, reactive, ref } from 'vue'
import { mount } from '@vue/test-utils'
import type { PanelDefinition } from './useResizable'
import {
  clamp, sanitizeWidth, loadWidth, saveWidth, storageKey,
  fitPanels, availableMax, dragWidth, keyboardDelta, useResizable
} from './useResizable'

const cfg = { defaultWidth: 240, min: 200, max: 360 }

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.style.userSelect = ''
  document.body.style.cursor = ''
})

function memoryStorage(initial: Record<string, string> = {}) {
  const data = { ...initial }
  return {
    data,
    get length() { return Object.keys(data).length },
    key: (index: number) => Object.keys(data)[index] ?? null,
    clear: () => { for (const key of Object.keys(data)) delete data[key] },
    removeItem: (key: string) => { delete data[key] },
    getItem: (k: string) => data[k] ?? null,
    setItem: (k: string, v: string) => { data[k] = String(v) }
  }
}

describe('clamp / sanitizeWidth', () => {
  it('clamps into range and tolerates max < min', () => {
    expect(clamp(100, 200, 360)).toBe(200)
    expect(clamp(500, 200, 360)).toBe(360)
    expect(clamp(250, 200, 360)).toBe(250)
    expect(clamp(250, 200, 150)).toBe(200)
  })

  it('falls back to the default for missing or garbage values', () => {
    expect(sanitizeWidth(null, cfg)).toBe(240)
    expect(sanitizeWidth('', cfg)).toBe(240)
    expect(sanitizeWidth('abc', cfg)).toBe(240)
    expect(sanitizeWidth('Infinity', cfg)).toBe(240)
    expect(sanitizeWidth('9999', cfg)).toBe(360)
    expect(sanitizeWidth('271.6', cfg)).toBe(272)
  })
})

describe('persistence', () => {
  it('round-trips through storage', () => {
    const s = memoryStorage()
    expect(saveWidth('left', 300.4, s)).toBe(true)
    expect(s.data[storageKey('left')]).toBe('300')
    expect(loadWidth('left', cfg, s)).toBe(300)
  })

  it('survives throwing or missing storage', () => {
    const broken = { ...memoryStorage(),
      getItem: () => { throw new Error('denied') },
      setItem: () => { throw new Error('quota') }
    }
    expect(loadWidth('left', cfg, broken)).toBe(240)
    expect(saveWidth('left', 300, broken)).toBe(false)
    expect(loadWidth('left', cfg, null)).toBe(240)
    expect(saveWidth('left', 300, null)).toBe(false)
  })
})

describe('fitPanels / availableMax', () => {
  const panels = [
    { width: 300, min: 200, visible: true },
    { width: 300, min: 200, visible: true },
    { width: 500, min: 320, visible: true }
  ]

  it('keeps preferred widths when there is room', () => {
    expect(fitPanels(panels, 2000, 400)).toEqual([300, 300, 500])
  })

  it('shrinks the last panels first, never below min', () => {
    // total 1100 + 400 center = 1500; viewport 1300 -> 200 overflow:
    // thread gives 180 (down to its 320 min), member list the remaining 20
    expect(fitPanels(panels, 1300, 400)).toEqual([300, 280, 320])
    expect(fitPanels(panels, 1000, 400)).toEqual([200, 200, 320])
  })

  it('ignores hidden panels', () => {
    const vis = [required(panels[0]), { ...required(panels[1]), visible: false }, { ...required(panels[2]), visible: false }]
    expect(fitPanels(vis, 900, 400)).toEqual([300, 0, 0])
  })

  it('skips hidden panels during squeezing and tolerates an already-minimum panel', () => {
    expect(fitPanels([
      { width: 300, min: 200, visible: true },
      { width: 200, min: 200, visible: true },
      { width: 600, min: 300, visible: false }
    ], 700, 400)).toEqual([200, 200, 0])
  })

  it('limits a panel by the remaining space', () => {
    expect(availableMax(0, [240, 240, 0], cfg, 1000, 400)).toBe(360)
    expect(availableMax(0, [240, 240, 0], cfg, 900, 400)).toBe(260)
    expect(availableMax(0, [240, 240, 0], cfg, 700, 400)).toBe(200)
  })
})

describe('drag & keyboard math', () => {
  it('grows left panels to the right and right panels to the left', () => {
    expect(dragWidth(240, 100, 150, 'left')).toBe(290)
    expect(dragWidth(240, 100, 150, 'right')).toBe(190)
  })

  it('maps arrow keys to 8px / 32px steps', () => {
    expect(keyboardDelta('ArrowRight', false, 'left')).toBe(8)
    expect(keyboardDelta('ArrowLeft', true, 'left')).toBe(-32)
    expect(keyboardDelta('ArrowLeft', false, 'right')).toBe(8)
    expect(keyboardDelta('ArrowRight', true, 'right')).toBe(-32)
    expect(keyboardDelta('Enter', false, 'left')).toBe(0)
  })
})

describe('useResizable', () => {
  function setup(storage: Storage, visibleThread = ref(false)) {
    window.innerWidth = 1600
    const scope = effectScope()
    const panels = required(scope.run(() => useResizable([
      { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
      { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360 },
      { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => visibleThread.value }
    ], { centerMin: 400, storage })))
    return { scope, panels, visibleThread }
  }

  it('restores persisted widths and resets on double-click', () => {
    const storage = memoryStorage({ [storageKey('left')]: '320', [storageKey('thread')]: 'junk' })
    const { scope, panels } = setup(storage)
    expect(panels.left.width).toBe(320)
    expect(panels.members.width).toBe(240)
    expect(panels.thread.width).toBe(0)
    panels.left.reset()
    expect(panels.left.width).toBe(240)
    expect(storage.data[storageKey('left')]).toBe('240')
    scope.stop()
  })

  it('resizes with arrow keys and persists', () => {
    const storage = memoryStorage()
    const { scope, panels } = setup(storage)
    const preventDefault = vi.fn()
    panels.left.onKeydown(Object.assign(new KeyboardEvent('keydown', { key: 'ArrowRight', shiftKey: true }), { preventDefault }))
    expect(panels.left.width).toBe(272)
    panels.members.onKeydown(Object.assign(new KeyboardEvent('keydown', { key: 'ArrowLeft', shiftKey: false }), { preventDefault }))
    expect(panels.members.width).toBe(248)
    panels.left.onKeydown(Object.assign(new KeyboardEvent('keydown', { key: 'Home', shiftKey: false }), { preventDefault }))
    expect(panels.left.width).toBe(200)
    expect(preventDefault).toHaveBeenCalledTimes(3)
    expect(storage.data[storageKey('members')]).toBe('248')
    scope.stop()
  })

  it('squeezes the thread panel first when it opens on a narrow window', async () => {
    window.innerWidth = 1600
    const { scope, panels, visibleThread } = setup(memoryStorage({ [storageKey('thread')]: '640' }))
    window.innerWidth = 1300
    window.dispatchEvent(new Event('resize'))
    await new Promise(r => requestAnimationFrame(r))
    visibleThread.value = true
    await nextTick()
    // 240 + 240 + 400 center = 880 -> 420 left for the thread (preferred 640)
    expect(panels.thread.width).toBe(420)
    expect(panels.thread.maxNow).toBe(420)
    scope.stop()
  })
})

describe('vertical panels (axis y)', () => {
  it('grows a bottom panel upwards and a top panel downwards', () => {
    expect(dragWidth(300, 500, 450, 'bottom')).toBe(350)
    expect(dragWidth(300, 500, 450, 'top')).toBe(250)
  })

  it('uses Up/Down for top and bottom panels and ignores Left/Right', () => {
    expect(keyboardDelta('ArrowUp', false, 'bottom')).toBe(8)
    expect(keyboardDelta('ArrowDown', true, 'bottom')).toBe(-32)
    expect(keyboardDelta('ArrowDown', false, 'top')).toBe(8)
    expect(keyboardDelta('ArrowLeft', false, 'bottom')).toBe(0)
    // Side panels keep ignoring Up/Down.
    expect(keyboardDelta('ArrowUp', false, 'left')).toBe(0)
  })

  it('shares the window height, persists the height and drags on the y axis', () => {
    window.innerHeight = 900
    const storage = memoryStorage({ [storageKey('chat', 'height')]: '400' })
    const scope = effectScope()
    const { chat } = required(scope.run(() => useResizable([
      { name: 'chat', side: 'bottom', defaultWidth: 300, min: 160, max: 1200 }
    ], { axis: 'y', centerMin: 300, storage })))
    expect(chat.axis).toBe('y')
    expect(chat.width).toBe(400)
    expect(chat.maxNow).toBe(600)

    const handle = document.createElement('div')
    handle.setPointerCapture = () => {}
    handle.releasePointerCapture = () => {}
    handle.addEventListener('pointerdown', event => chat.startDrag(event))
    handle.dispatchEvent(new PointerEvent('pointerdown', { button: 0, pointerId: 1, clientX: 10, clientY: 500 }))
    expect(document.body.style.cursor).toBe('row-resize')
    handle.dispatchEvent(Object.assign(new Event('pointerup'), { pointerId: 1, clientX: 10, clientY: 400 }))
    expect(chat.width).toBe(500)
    expect(storage.data[storageKey('chat', 'height')]).toBe('500')
    expect(storage.data[storageKey('chat')]).toBeUndefined()
    expect(document.body.style.cursor).toBe('')
    scope.stop()
  })
})

describe('resize interaction lifecycle', () => {
  it('fits a panel definition added reactively without losing the existing preferred width', () => {
    window.innerWidth = 900
    const definitions = reactive<PanelDefinition<'panel' | 'later'>[]>([{ name: 'panel', side: 'left', ...cfg }])
    const scope = effectScope()
    const handles = required(scope.run(() => useResizable(definitions, { storage: memoryStorage() })))
    expect(handles.panel.width).toBe(240)
    expect(handles.panel.maxNow).toBe(360)
    definitions.push({ name: 'later', side: 'right', ...cfg })
    expect(handles.panel.width).toBe(240)
    expect(handles.panel.maxNow).toBe(260)
    definitions.pop()
    expect(handles.panel.width).toBe(240)
    expect(handles.panel.maxNow).toBe(360)
    scope.stop()
  })
  it('finishes the previous drag when another separator press begins and restores the original styles', () => {
    const frames = controlledFrames()
    const { scope, panel, pointer, save } = setupOne()
    document.body.style.userSelect = 'text'
    document.body.style.cursor = 'crosshair'
    pointer('pointerdown', 100)
    pointer('pointermove', 130)
    pointer('pointerdown', 150, 0, 9)
    expect(frames.frames.size).toBe(0)
    expect(save).toHaveBeenCalledOnce()
    expect(panel.width).toBe(270)
    expect(panel.dragging).toBe(true)
    pointer('pointerup', 180, 0, 9)
    expect(panel.width).toBe(300)
    expect(save).toHaveBeenCalledTimes(2)
    expect(document.body.style.userSelect).toBe('text')
    expect(document.body.style.cursor).toBe('crosshair')
    scope.stop()
  })
  it('restores prior body styles and removes active drag listeners when its component unmounts', () => {
    const frames = controlledFrames()
    const storage = memoryStorage()
    const save = vi.spyOn(storage, 'setItem')
    document.body.style.userSelect = 'text'
    document.body.style.cursor = 'crosshair'
    let resizePanel: ReturnType<typeof useResizable<'panel'>>['panel'] | undefined
    const wrapper = mount(defineComponent({ setup() {
      const { panel } = useResizable([{ name: 'panel', side: 'left', ...cfg }], { storage })
      resizePanel = panel
      return () => h('div', { onPointerdown: (event: PointerEvent) => panel.startDrag(event) })
    } }), { attachTo: document.body })
    const handle = wrapper.element
    if (!(handle instanceof HTMLElement)) throw new Error('Expected resize handle')
    handle.setPointerCapture = vi.fn()
    handle.hasPointerCapture = vi.fn(() => true)
    handle.releasePointerCapture = vi.fn()
    handle.dispatchEvent(new PointerEvent('pointerdown', { button: 0, pointerId: 4, clientX: 100 }))
    handle.dispatchEvent(new PointerEvent('pointermove', { pointerId: 4, clientX: 130 }))
    expect(required(resizePanel).dragging).toBe(true)
    expect(document.body.style.userSelect).toBe('none')
    expect(document.body.style.cursor).toBe('col-resize')
    expect(frames.frames.size).toBe(1)
    wrapper.unmount()
    expect(required(resizePanel).dragging).toBe(false)
    expect(document.body.style.userSelect).toBe('text')
    expect(document.body.style.cursor).toBe('crosshair')
    expect(handle.releasePointerCapture).toHaveBeenCalledWith(4)
    expect(frames.frames.size).toBe(0)
    expect(save).toHaveBeenCalledOnce()
    expect(storage.data[storageKey('panel')]).toBe('270')
    handle.dispatchEvent(new PointerEvent('pointermove', { pointerId: 4, clientX: 400 }))
    handle.dispatchEvent(new PointerEvent('pointerup', { pointerId: 4, clientX: 400 }))
    expect(save).toHaveBeenCalledOnce()
    expect(frames.frames.size).toBe(0)
  })
  function controlledFrames() {
    let nextId = 0
    const frames = new Map<number, FrameRequestCallback>()
    const request = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation(callback => {
      frames.set(++nextId, callback)
      return nextId
    })
    const cancel = vi.spyOn(globalThis, 'cancelAnimationFrame').mockImplementation(id => {
      frames.delete(id)
    })
    function flush() {
      const pending = [...frames]
      frames.clear()
      for (const [, callback] of pending) callback(0)
    }
    return { frames, request, cancel, flush }
  }

  function setupOne(side: 'left' | 'right' | 'top' | 'bottom' = 'left') {
    window.innerWidth = 1000
    window.innerHeight = 1000
    const storage = memoryStorage()
    const save = vi.spyOn(storage, 'setItem')
    const scope = effectScope()
    const { panel } = required(scope.run(() => useResizable([
      { name: 'panel', side, ...cfg }
    ], { storage, centerMin: 400, axis: side === 'top' || side === 'bottom' ? 'y' : 'x' })))
    const handle = document.createElement('div')
    const captured = new Set<number>()
    handle.setPointerCapture = vi.fn((id: number) => { captured.add(id) })
    handle.hasPointerCapture = vi.fn((id: number) => captured.has(id))
    handle.releasePointerCapture = vi.fn((id: number) => { captured.delete(id) })
    handle.addEventListener('pointerdown', event => panel.startDrag(event))
    function pointer(type: string, x: number, y = 0, pointerId = 4, button = 0) {
      const event = new PointerEvent(type, { pointerId, button, clientX: x, clientY: y, cancelable: true })
      handle.dispatchEvent(event)
      return event
    }
    return { scope, panel, storage, save, handle, pointer }
  }

  it('coalesces moves, ignores other pointers, and saves the final pointerup position once', () => {
    const frames = controlledFrames()
    const { scope, panel, save, handle, pointer } = setupOne()
    document.body.style.userSelect = 'text'
    document.body.style.cursor = 'crosshair'
    const down = pointer('pointerdown', 100)
    expect(down.defaultPrevented).toBe(true)
    expect(handle.setPointerCapture).toHaveBeenCalledWith(4)
    expect(panel.dragging).toBe(true)
    expect(document.body.style.userSelect).toBe('none')
    expect(document.body.style.cursor).toBe('col-resize')
    pointer('pointermove', 800, 0, 9)
    pointer('pointerup', 800, 0, 9)
    expect(frames.request).not.toHaveBeenCalled()
    expect(panel.dragging).toBe(true)
    pointer('pointermove', 110)
    pointer('pointermove', 130)
    expect(frames.request).toHaveBeenCalledTimes(1)
    expect(panel.width).toBe(240)
    expect(save).not.toHaveBeenCalled()
    frames.flush()
    expect(panel.width).toBe(270)
    pointer('pointermove', 140)
    pointer('pointerup', 160)
    expect(frames.cancel).toHaveBeenCalledTimes(1)
    expect(panel.width).toBe(300)
    expect(save).toHaveBeenCalledExactlyOnceWith(storageKey('panel'), '300')
    expect(panel.dragging).toBe(false)
    expect(document.body.style.userSelect).toBe('text')
    expect(document.body.style.cursor).toBe('crosshair')
    expect(handle.releasePointerCapture).toHaveBeenCalledWith(4)
    pointer('pointermove', 500)
    pointer('pointerup', 500)
    expect(save).toHaveBeenCalledTimes(1)
    expect(frames.frames.size).toBe(0)
    scope.stop()
  })

  it.each(['pointercancel', 'lostpointercapture'])('finishes %s at the last move rather than the cancel-event coordinates', end => {
    const frames = controlledFrames()
    const { scope, panel, save, pointer } = setupOne()
    pointer('pointerdown', 100)
    pointer('pointermove', 125)
    pointer(end, 999)
    expect(panel.width).toBe(265)
    expect(panel.dragging).toBe(false)
    expect(save).toHaveBeenCalledExactlyOnceWith(storageKey('panel'), '265')
    expect(frames.cancel).toHaveBeenCalledTimes(1)
    frames.flush()
    expect(panel.width).toBe(265)
    scope.stop()
  })

  it('clamps drags at both limits and rounds fractional movement', () => {
    const { scope, panel, pointer } = setupOne()
    pointer('pointerdown', 100)
    pointer('pointerup', 999)
    expect(panel.width).toBe(360)
    pointer('pointerdown', 100)
    pointer('pointerup', -999)
    expect(panel.width).toBe(200)
    pointer('pointerdown', 100)
    pointer('pointerup', 120.6)
    expect(panel.width).toBe(221)
    scope.stop()
  })

  it('uses the remaining viewport as the drag limit', () => {
    const frames = controlledFrames()
    const { scope, panel, pointer, storage } = setupOne()
    window.innerWidth = 700
    window.dispatchEvent(new Event('resize'))
    frames.flush()
    expect(panel.maxNow).toBe(300)
    pointer('pointerdown', 100)
    pointer('pointerup', 400)
    expect(panel.width).toBe(300)
    expect(storage.data[storageKey('panel')]).toBe('300')
    scope.stop()
  })

  it('ignores secondary buttons and direct calls without a current element', () => {
    const { scope, panel, pointer, save, handle } = setupOne()
    expect(pointer('pointerdown', 100, 0, 4, 2).defaultPrevented).toBe(false)
    panel.startDrag(new PointerEvent('pointerdown', { button: 0, pointerId: 4 }))
    expect(handle.setPointerCapture).not.toHaveBeenCalled()
    expect(panel.dragging).toBe(false)
    expect(save).not.toHaveBeenCalled()
    scope.stop()
  })

  it('restores styling and persistence even when capture and release throw', () => {
    const { scope, panel, handle, pointer, storage } = setupOne()
    handle.setPointerCapture = () => { throw new Error('capture unavailable') }
    handle.hasPointerCapture = () => true
    handle.releasePointerCapture = () => { throw new Error('already released') }
    pointer('pointerdown', 100)
    expect(panel.dragging).toBe(true)
    pointer('pointerup', 150)
    expect(panel.dragging).toBe(false)
    expect(panel.width).toBe(290)
    expect(document.body.style.userSelect).toBe('')
    expect(storage.data[storageKey('panel')]).toBe('290')
    scope.stop()
  })

  it('does not release capture that has already been lost', () => {
    const { scope, handle, pointer } = setupOne()
    handle.hasPointerCapture = () => false
    pointer('pointerdown', 100)
    pointer('lostpointercapture', 200)
    expect(handle.releasePointerCapture).not.toHaveBeenCalled()
    scope.stop()
  })

  it('grows right panels when the pointer moves left', () => {
    const { scope, panel, pointer } = setupOne('right')
    pointer('pointerdown', 200)
    pointer('pointerup', 150)
    expect(panel.width).toBe(290)
    scope.stop()
  })

  it('coalesces vertical window resizing, restores preferences, and cancels pending work on disposal', () => {
    const frames = controlledFrames()
    const { scope, panel, storage } = setupOne('bottom')
    panel.onKeydown(new KeyboardEvent('keydown', { key: 'End', cancelable: true }))
    expect(panel.width).toBe(360)
    window.innerHeight = 650
    window.dispatchEvent(new Event('resize'))
    window.dispatchEvent(new Event('resize'))
    expect(frames.request).toHaveBeenCalledTimes(1)
    frames.flush()
    expect(panel.width).toBe(250)
    expect(storage.data[storageKey('panel', 'height')]).toBe('360')
    window.innerHeight = 1000
    window.dispatchEvent(new Event('resize'))
    frames.flush()
    expect(panel.width).toBe(360)
    window.innerHeight = 600
    window.dispatchEvent(new Event('resize'))
    scope.stop()
    expect(frames.cancel).toHaveBeenCalledTimes(1)
    expect(frames.frames.size).toBe(0)
    window.dispatchEvent(new Event('resize'))
    expect(frames.request).toHaveBeenCalledTimes(3)
  })

  it('ignores unrelated keys and clamps repeated keyboard steps before persisting', () => {
    const { scope, panel, save } = setupOne('top')
    const ignored = new KeyboardEvent('keydown', { key: 'ArrowLeft', cancelable: true })
    panel.onKeydown(ignored)
    expect(ignored.defaultPrevented).toBe(false)
    expect(save).not.toHaveBeenCalled()
    const end = new KeyboardEvent('keydown', { key: 'End', cancelable: true })
    panel.onKeydown(end)
    expect(end.defaultPrevented).toBe(true)
    panel.onKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown', shiftKey: true }))
    expect(panel.width).toBe(360)
    panel.onKeydown(new KeyboardEvent('keydown', { key: 'Home' }))
    panel.onKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp', shiftKey: true }))
    expect(panel.width).toBe(200)
    expect(save).toHaveBeenLastCalledWith(storageKey('panel', 'height'), '200')
    scope.stop()
  })

  it('makes retained handles safe when their panel definition is removed', () => {
    window.innerWidth = 1000
    const definitions = reactive<PanelDefinition<'panel'>[]>([{ name: 'panel', side: 'left', ...cfg }])
    const storage = memoryStorage()
    const save = vi.spyOn(storage, 'setItem')
    const scope = effectScope()
    const { panel } = required(scope.run(() => useResizable(definitions, { storage })))
    expect(panel.width).toBe(240)
    definitions.splice(0)
    expect(panel.width).toBe(240)
    expect(panel.maxNow).toBe(0)
    const key = new KeyboardEvent('keydown', { key: 'End', cancelable: true })
    panel.onKeydown(key)
    panel.reset()
    const handle = document.createElement('div')
    handle.addEventListener('pointerdown', event => panel.startDrag(event))
    const down = new PointerEvent('pointerdown', { button: 0, cancelable: true })
    handle.dispatchEvent(down)
    expect(key.defaultPrevented).toBe(false)
    expect(down.defaultPrevented).toBe(false)
    expect(panel.dragging).toBe(false)
    expect(save).not.toHaveBeenCalled()
    expect(document.body.style.cursor).toBe('')
    scope.stop()
  })
})

describe('storage and non-browser resize setup', () => {
  it('uses default storage when available and survives an inaccessible storage getter', () => {
    const storage = memoryStorage({ [storageKey('panel')]: '280' })
    vi.stubGlobal('localStorage', storage)
    expect(loadWidth('panel', cfg)).toBe(280)
    expect(saveWidth('panel', 290)).toBe(true)
    expect(storage.data[storageKey('panel')]).toBe('290')
    vi.unstubAllGlobals()
    vi.spyOn(globalThis, 'localStorage', 'get').mockImplementation(() => { throw new Error('private mode') })
    expect(loadWidth('panel', cfg)).toBe(240)
    expect(saveWidth('panel', 300)).toBe(false)
  })

  it('uses null storage safely when the browser does not expose localStorage', () => {
    vi.stubGlobal('localStorage', undefined)
    expect(loadWidth('panel', cfg)).toBe(240)
    expect(saveWidth('panel', 300)).toBe(false)
    expect(sanitizeWidth(undefined, cfg)).toBe(240)
  })

  it.each([['x', 'left', 360], ['y', 'top', 360]] as const)('has a stable %s viewport fallback without a window', (axis, side, max) => {
    vi.stubGlobal('window', undefined)
    const scope = effectScope()
    const { panel } = required(scope.run(() => useResizable([{ name: 'panel', side, ...cfg }], { axis, storage: null })))
    expect(panel.width).toBe(240)
    expect(panel.maxNow).toBe(max)
    scope.stop()
  })
})
