import { describe, it, expect, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import {
  clamp, sanitizeWidth, loadWidth, saveWidth, storageKey,
  fitPanels, availableMax, dragWidth, keyboardDelta, useResizable
} from './useResizable'

const cfg = { defaultWidth: 240, min: 200, max: 360 }

function memoryStorage(initial = {}) {
  const data = { ...initial }
  return {
    data,
    getItem: k => (k in data ? data[k] : null),
    setItem: (k, v) => { data[k] = String(v) }
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
    const broken = {
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
    const vis = [panels[0], { ...panels[1], visible: false }, { ...panels[2], visible: false }]
    expect(fitPanels(vis, 900, 400)).toEqual([300, 0, 0])
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
  function setup(storage, visibleThread = ref(false)) {
    window.innerWidth = 1600
    const scope = effectScope()
    const panels = scope.run(() => useResizable([
      { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
      { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360 },
      { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => visibleThread.value }
    ], { centerMin: 400, storage }))
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
    panels.left.onKeydown({ key: 'ArrowRight', shiftKey: true, preventDefault })
    expect(panels.left.width).toBe(272)
    panels.members.onKeydown({ key: 'ArrowLeft', shiftKey: false, preventDefault })
    expect(panels.members.width).toBe(248)
    panels.left.onKeydown({ key: 'Home', shiftKey: false, preventDefault })
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
    const { chat } = scope.run(() => useResizable([
      { name: 'chat', side: 'bottom', defaultWidth: 300, min: 160, max: 1200 }
    ], { axis: 'y', centerMin: 300, storage }))
    expect(chat.axis).toBe('y')
    expect(chat.width).toBe(400)
    expect(chat.maxNow).toBe(600)

    const handle = document.createElement('div')
    handle.setPointerCapture = () => {}
    handle.releasePointerCapture = () => {}
    chat.startDrag({ button: 0, currentTarget: handle, pointerId: 1, clientX: 10, clientY: 500, preventDefault: () => {} })
    expect(document.body.style.cursor).toBe('row-resize')
    handle.dispatchEvent(Object.assign(new Event('pointerup'), { pointerId: 1, clientX: 10, clientY: 400 }))
    expect(chat.width).toBe(500)
    expect(storage.data[storageKey('chat', 'height')]).toBe('500')
    expect(storage.data[storageKey('chat')]).toBeUndefined()
    expect(document.body.style.cursor).toBe('')
    scope.stop()
  })
})
