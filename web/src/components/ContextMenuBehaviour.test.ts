import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ContextMenu from './ContextMenu.vue'
import type { ContextMenuItem } from './menuTypes'
import { requireValue } from '../test-fixtures.fixture'

let w!: ReturnType<typeof mount<typeof ContextMenu>>
afterEach(() => { w?.unmount(); document.body.innerHTML = '' })

async function open(items: ContextMenuItem[], props: Record<string, unknown> = {}) {
  w = mount(ContextMenu, { props: { modelValue: false, x: 10, y: 10, items, ...props }, attachTo: document.body })
  await w.setProps({ modelValue: true })
  await nextTick(); await nextTick()
}
const menu = () => requireValue(document.querySelector<HTMLElement>('[role="menu"]'))

describe('ContextMenu placement', () => {
  it('flips above and aligns to the right edge near the bottom right corner, never leaving the window', async () => {
    const original = HTMLElement.prototype.getBoundingClientRect
    HTMLElement.prototype.getBoundingClientRect = function () {
      return this.hasAttribute('role') ? new DOMRect(0, 0, 200, 150) : original.call(this)
    }
    try {
      await open([{ label: 'A' }], { x: window.innerWidth - 5, y: window.innerHeight - 5 })
      const left = parseFloat(menu().style.left)
      const top = parseFloat(menu().style.top)
      expect(left + 200).toBeLessThanOrEqual(window.innerWidth - 8)
      expect(top + 150).toBeLessThanOrEqual(window.innerHeight)
      expect(left).toBeGreaterThanOrEqual(8)
      expect(top).toBeGreaterThanOrEqual(8)
    } finally {
      HTMLElement.prototype.getBoundingClientRect = original
    }
  })
})

describe('ContextMenu submenu placement', () => {
  const original = HTMLElement.prototype.getBoundingClientRect
  afterEach(() => { HTMLElement.prototype.getBoundingClientRect = original })
  const submenu: ContextMenuItem = { type: 'submenu', id: 's', label: 'Devices', items: [{ label: 'One' }] }

  function layout(rowLeft: number) {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    HTMLElement.prototype.getBoundingClientRect = function () {
      if (this.hasAttribute('data-submenu-panel')) return new DOMRect(240, 100, 200, 100)
      if (this.classList.contains('relative')) return new DOMRect(rowLeft, 90, 260, 36)
      return original.call(this)
    }
  }
  const panel = () => requireValue(document.querySelector<HTMLElement>('[data-submenu-panel]'))
  async function openSub() {
    await open([submenu])
    requireValue(document.querySelector<HTMLElement>('[data-submenu-trigger]')).click()
    await nextTick(); await nextTick()
  }

  it('opens to the left of its row when that fits', async () => {
    layout(300)
    await openSub()
    expect(panel().className).toContain('right-full')
  })

  it('overlaps its parent on a narrow screen instead of leaving the window', async () => {
    layout(10)
    await openSub()
    expect(panel().className).toContain('left-full')
    expect(panel().style.transform).toBe('translateX(-58px)')
  })
})

describe('ContextMenu rows', () => {
  it('keeps a row with a reason reachable but inert, and explains it', async () => {
    let ran = false
    await open([{ id: 'x', label: 'Change', disabled: true, disabledReason: 'Not while sharing', action: () => { ran = true } }])
    const row = requireValue(document.querySelector<HTMLButtonElement>('[data-menu-item="x"]'))
    expect(row.getAttribute('aria-disabled')).toBe('true')
    expect(row.disabled).toBe(false)
    row.focus()
    row.click()
    expect(ran).toBe(false)
    expect(w.emitted('close')).toBeUndefined()
  })

  it('renders a live level as an ARIA meter with a threshold mark', async () => {
    await open([{ type: 'meter', label: 'Level', value: 42, threshold: 30 }])
    const meter = requireValue(document.querySelector('[role="meter"]'))
    expect(meter.getAttribute('aria-valuenow')).toBe('42')
    expect(requireValue(meter.firstElementChild).getAttribute('style')).toContain('width: 42%')
    expect(requireValue(meter.lastElementChild).getAttribute('style')).toContain('left: 30%')
  })

  it('marks checkboxes and shows a right-aligned shortcut', async () => {
    await open([{ type: 'checkbox', label: 'Noise', checked: true }, { label: 'Mute', shortcut: 'M' }])
    expect(requireValue(document.querySelector('[role="menuitemcheckbox"]')).getAttribute('aria-checked')).toBe('true')
    expect(menu().textContent).toContain('M')
  })
})

describe('ContextMenu dismissal', () => {
  it('closes when the window loses focus, and not when it is already closed', async () => {
    await open([{ label: 'A' }])
    window.dispatchEvent(new Event('blur'))
    expect(w.emitted('close')).toHaveLength(1)
    await w.setProps({ modelValue: false })
    window.dispatchEvent(new Event('blur'))
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('closes on an outside press and on a scroll', async () => {
    await open([{ label: 'A' }])
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(1)
    await w.setProps({ modelValue: false }); await w.setProps({ modelValue: true })
    await new Promise<void>(r => requestAnimationFrame(() => requestAnimationFrame(() => r())))
    window.dispatchEvent(new Event('scroll'))
    expect(w.emitted('close')).toHaveLength(2)
  })

  it('attaches to the full-screen element, as the page behind it is not drawn', async () => {
    const stage = document.createElement('div')
    document.body.appendChild(stage)
    Object.defineProperty(document, 'fullscreenElement', { configurable: true, get: () => stage })
    try {
      await open([{ label: 'A' }])
      expect(stage.querySelector('[role="menu"]')).toBeTruthy()
    } finally {
      Reflect.deleteProperty(document, 'fullscreenElement')
    }
  })
})
