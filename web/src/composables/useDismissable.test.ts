import { describe, it, expect, vi, afterEach } from 'vitest'
import { effectScope, ref, nextTick, defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { useDialog } from './useDialog'
import { shieldEscape, useDismissable } from './useDismissable'
import { required } from '../store-test-support.fixture'

function el() {
  const node = document.createElement('div')
  document.body.appendChild(node)
  return node
}

const press = (node: Element) => node.dispatchEvent(new Event('pointerdown', { bubbles: true, composed: true }))
const escape = (node = document.body) => {
  const ev = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
  node.dispatchEvent(ev)
  return ev
}

afterEach(() => { document.body.innerHTML = '' })

describe('shieldEscape', () => {
  it('lets something inside take the Escape before a popover closes, until it is removed', () => {
    const close = vi.fn()
    const scope = effectScope()
    scope.run(() => { useDismissable(el(), close); useDismissable(el(), close) })
    let used = true
    const remove = shieldEscape(() => used)
    const taken = escape()
    expect(close).not.toHaveBeenCalled()
    expect(taken.defaultPrevented).toBe(true)
    used = false
    escape()
    expect(close).toHaveBeenCalledTimes(2)
    remove()
    used = true
    escape()
    expect(close).toHaveBeenCalledTimes(4)
    scope.stop()
  })
})

describe('useDismissable', () => {
  it('supports a render environment without a document and permits stopping twice', () => {
    vi.stubGlobal('document', undefined)
    try {
      const dismissable = useDismissable(null, vi.fn())
      dismissable.stop()
      dismissable.stop()
    } finally { vi.unstubAllGlobals() }
  })
  it('falls back to target containment when composedPath is absent or empty', () => {
    const root = el()
    const child = document.createElement('span')
    root.append(child)
    const close = vi.fn()
    const add = vi.spyOn(document, 'addEventListener')
    const d = useDismissable(() => [null, root], close)
    const listener = required(add.mock.calls.find(call => call[0] === 'pointerdown')?.[1])
    add.mockRestore()
    const inside = new Event('pointerdown', { bubbles: true })
    Object.defineProperty(inside, 'composedPath', { value: undefined })
    Object.defineProperty(inside, 'target', { value: child })
    if (typeof listener === 'function') listener(inside)
    else listener.handleEvent(inside)
    expect(close).not.toHaveBeenCalled()
    const outside = new Event('pointerdown', { bubbles: true })
    Object.defineProperty(outside, 'composedPath', { value: () => [] })
    if (typeof listener === 'function') listener(outside)
    else listener.handleEvent(outside)
    expect(close).toHaveBeenCalledOnce()
    d.stop()
    d.stop()
    press(root)
    expect(close).toHaveBeenCalledOnce()
  })

  it('handles no root and ignores ordinary or composing Escape keys', () => {
    const close = vi.fn()
    const d = useDismissable(null, close)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'x' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', isComposing: true }))
    expect(close).not.toHaveBeenCalled()
    press(el())
    expect(close).toHaveBeenCalledOnce()
    d.stop()
  })
  it('closes on a press outside, not inside', () => {
    const root = el()
    const inner = document.createElement('button')
    root.appendChild(inner)
    const outside = el()
    const onClose = vi.fn()
    const scope = effectScope()
    scope.run(() => useDismissable(ref(root), onClose))
    press(inner)
    expect(onClose).not.toHaveBeenCalled()
    press(outside)
    expect(onClose).toHaveBeenCalledWith(expect.any(Event), 'outside')
    scope.stop()
  })

  it('treats every element of a list as inside', () => {
    const a = el()
    const b = el()
    const onClose = vi.fn()
    const scope = effectScope()
    scope.run(() => useDismissable(() => [a, b], onClose))
    press(a)
    press(b)
    expect(onClose).not.toHaveBeenCalled()
    press(el())
    expect(onClose).toHaveBeenCalledTimes(1)
    scope.stop()
  })

  it('closes on Escape and keeps the key from reaching handlers behind it', () => {
    const root = el()
    const onClose = vi.fn()
    const behind = vi.fn()
    window.addEventListener('keydown', behind)
    const scope = effectScope()
    scope.run(() => useDismissable(root, onClose))
    const ev = escape()
    expect(onClose).toHaveBeenCalledWith(ev, 'escape')
    expect(ev.defaultPrevented).toBe(true)
    expect(behind).not.toHaveBeenCalled()
    window.removeEventListener('keydown', behind)
    scope.stop()
  })

  it('ignores Escape when escape is off', () => {
    const onClose = vi.fn()
    const scope = effectScope()
    scope.run(() => useDismissable(el(), onClose, { escape: false }))
    escape()
    expect(onClose).not.toHaveBeenCalled()
    scope.stop()
  })

  it('listens only while active and removes its listeners again', async () => {
    const root = el()
    const open = ref(false)
    const onClose = vi.fn()
    const add = vi.spyOn(document, 'addEventListener')
    const remove = vi.spyOn(document, 'removeEventListener')
    const scope = effectScope()
    scope.run(() => useDismissable(root, onClose, { active: open }))
    press(el())
    escape()
    expect(onClose).not.toHaveBeenCalled()
    expect(add).not.toHaveBeenCalled()

    open.value = true
    await nextTick()
    expect(add).toHaveBeenCalledTimes(1) // pointerdown; Escape listens on window
    press(el())
    expect(onClose).toHaveBeenCalledTimes(1)

    open.value = false
    await nextTick()
    expect(remove).toHaveBeenCalledTimes(1)
    press(el())
    expect(onClose).toHaveBeenCalledTimes(1)

    open.value = true
    scope.stop()
    expect(remove).toHaveBeenCalledTimes(2)
    press(el())
    escape()
    expect(onClose).toHaveBeenCalledTimes(1)
    add.mockRestore()
    remove.mockRestore()
  })
})

describe('useDismissable inside a dialog', () => {
  it('takes Escape before an open dialog, so one press closes only the popover', () => {
    const root = ref<HTMLElement | null>(null)
    const closeDialog = vi.fn()
    const closePopover = vi.fn()
    const Dialog = defineComponent({ setup() { useDialog(root, { onClose: closeDialog }); return () => h('div', { ref: root, tabindex: -1 }) } })
    const Popover = defineComponent({ setup() { useDismissable(null, closePopover); return () => h('span') } })
    const dialog = mount(Dialog, { attachTo: document.body })
    const popover = mount(Popover, { attachTo: document.body })
    escape()
    expect(closePopover).toHaveBeenCalledTimes(1)
    expect(closeDialog).not.toHaveBeenCalled()
    popover.unmount()
    escape()
    expect(closeDialog).toHaveBeenCalledTimes(1)
    dialog.unmount()
  })
})
