import { describe, it, expect, vi, afterEach } from 'vitest'
import { effectScope, ref, nextTick } from 'vue'
import { useDismissable } from './useDismissable'

function el() {
  const node = document.createElement('div')
  document.body.appendChild(node)
  return node
}

const press = node => node.dispatchEvent(new Event('pointerdown', { bubbles: true, composed: true }))
const escape = (node = document.body) => {
  const ev = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
  node.dispatchEvent(ev)
  return ev
}

afterEach(() => { document.body.innerHTML = '' })

describe('useDismissable', () => {
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
    expect(add).toHaveBeenCalledTimes(2)
    press(el())
    expect(onClose).toHaveBeenCalledTimes(1)

    open.value = false
    await nextTick()
    expect(remove).toHaveBeenCalledTimes(2)
    press(el())
    expect(onClose).toHaveBeenCalledTimes(1)

    open.value = true
    scope.stop()
    expect(remove).toHaveBeenCalledTimes(4)
    press(el())
    escape()
    expect(onClose).toHaveBeenCalledTimes(1)
    add.mockRestore()
    remove.mockRestore()
  })
})
