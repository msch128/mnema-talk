import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { useContextMenuTrigger, LONG_PRESS_MS } from './useContextMenuTrigger'
import { requireValue } from '../test-fixtures.fixture'

let wrapper: VueWrapper | undefined
let trigger!: ReturnType<typeof useContextMenuTrigger>
const build = vi.fn(() => [{ label: 'One' }])

function mountButton(options: Parameters<typeof useContextMenuTrigger>[1] = {}) {
  const Host = defineComponent({
    setup() {
      trigger = useContextMenuTrigger(build, options)
      return () => h('button', { type: 'button', ...trigger.on }, 'x')
    }
  })
  wrapper = mount(Host, { attachTo: document.body })
  return wrapper.element as HTMLButtonElement
}

function touch(type: string, x = 5, y = 5) {
  return new PointerEvent(type, { pointerType: 'touch', isPrimary: true, clientX: x, clientY: y, bubbles: true })
}

beforeEach(() => { vi.useFakeTimers(); build.mockClear() })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); document.body.innerHTML = '' })

describe('useContextMenuTrigger', () => {
  it('opens at the pointer on a right click and keeps the event from the browser menu', async () => {
    const button = mountButton()
    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 40, clientY: 60 })
    button.dispatchEvent(event)
    await nextTick(); await nextTick()
    expect(event.defaultPrevented).toBe(true)
    expect(trigger.menu.state).toMatchObject({ open: true, x: 40, y: 60 })
    expect(trigger.menu.items.value).toEqual([{ label: 'One' }])
  })

  it('opens at the button for the keyboard (Menu key, Shift+F10), which carries no coordinates', async () => {
    const button = mountButton()
    button.getBoundingClientRect = () => new DOMRect(10, 20, 30, 40)
    button.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await nextTick(); await nextTick()
    expect(trigger.menu.state).toMatchObject({ open: true, x: 10, y: 60 })
    expect(trigger.menu.state.anchor).toBe(button)
  })

  it('opens after a long press with a finger and swallows the click that follows', async () => {
    const button = mountButton()
    const click = vi.fn()
    button.addEventListener('click', click)
    button.dispatchEvent(touch('pointerdown', 12, 34))
    vi.advanceTimersByTime(LONG_PRESS_MS - 1)
    expect(trigger.menu.state.open).toBe(false)
    vi.advanceTimersByTime(1)
    await nextTick(); await nextTick()
    expect(trigger.menu.state).toMatchObject({ open: true, x: 12, y: 34 })
    button.dispatchEvent(touch('pointerup'))
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    expect(click).not.toHaveBeenCalled()
    // Only that one click: the next press works normally.
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    expect(click).toHaveBeenCalledTimes(1)
  })

  it('does not open when the finger lifts early or moves away (a tap or a scroll)', () => {
    const button = mountButton()
    button.dispatchEvent(touch('pointerdown'))
    vi.advanceTimersByTime(LONG_PRESS_MS - 10)
    button.dispatchEvent(touch('pointerup'))
    vi.advanceTimersByTime(LONG_PRESS_MS)
    button.dispatchEvent(touch('pointerdown'))
    button.dispatchEvent(touch('pointermove', 5, 60))
    vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    expect(trigger.menu.state.open).toBe(false)
  })

  it('ignores a mouse press for the long press', () => {
    const button = mountButton()
    button.dispatchEvent(new PointerEvent('pointerdown', { pointerType: 'mouse', isPrimary: true, bubbles: true }))
    vi.advanceTimersByTime(LONG_PRESS_MS * 2)
    expect(trigger.menu.state.open).toBe(false)
  })

  it('stays out of the way while it has nothing to offer', () => {
    const button = mountButton({ enabled: () => false })
    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 1, clientY: 1 })
    button.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    expect(trigger.menu.state.open).toBe(false)
  })

  it('lets the click through when a long press has no menu to open', () => {
    const button = mountButton({ enabled: () => false })
    const click = vi.fn()
    button.addEventListener('click', click)
    button.dispatchEvent(touch('pointerdown'))
    vi.advanceTimersByTime(LONG_PRESS_MS)
    button.dispatchEvent(touch('pointerup'))
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    expect(trigger.menu.state.open).toBe(false)
    expect(click).toHaveBeenCalledTimes(1)
  })

  it('tells the owner when it opens, and closes on request', async () => {
    const onOpen = vi.fn()
    const button = mountButton({ onOpen })
    button.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 1, clientY: 1 }))
    await nextTick(); await nextTick()
    expect(onOpen).toHaveBeenCalledTimes(1)
    requireValue(trigger).close()
    expect(trigger.menu.state.open).toBe(false)
  })
})
