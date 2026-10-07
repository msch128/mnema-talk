import { requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import ContextMenu from './ContextMenu.vue'
import ContextMenuRow from './ContextMenuRow.vue'
import { Check } from '@lucide/vue'

let w!: ReturnType<typeof mount<typeof ContextMenu>>
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

function items(actions: { a?: () => void; g?: () => void } = {}): ContextMenuItem[] {
  return [
    { label: 'Alpha', ...(actions.a ? { action: actions.a } : {}) },
    { type: 'separator' },
    { label: 'Beta', disabled: true },
    { label: 'Gamma', ...(actions.g ? { action: actions.g } : {}) },
    { type: 'label', label: 'Level' },
    { type: 'radio', label: 'All', checked: true },
    { type: 'radio', label: 'Mute', checked: false }
  ]
}

async function openMenu(props = {}) {
  const opener = document.createElement('button')
  document.body.appendChild(opener)
  opener.focus()
  w = mount(ContextMenu, { props: { modelValue: false, x: 10, y: 10, items: items(), ...props }, attachTo: document.body })
  await w.setProps({ modelValue: true })
  await nextTick()
  await nextTick()
  return opener
}

const menu = () => requireValue(document.querySelector<HTMLElement>('[role="menu"]'))
const rows = () => [...document.querySelectorAll('[data-menu-nav]')]
const key = (k: string, init: KeyboardEventInit = {}) => requireValue(menu()).dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, ...init }))

describe('ContextMenu accessibility', () => {
  it('exposes menu, menuitem and menuitemradio roles', async () => {
    await openMenu()
    expect(menu()).toBeTruthy()
    expect(document.querySelectorAll('[role="menuitem"]').length).toBe(3)
    const radios = document.querySelectorAll('[role="menuitemradio"]')
    expect(radios.length).toBe(2)
    expect(requireValue(radios[0]).getAttribute('aria-checked')).toBe('true')
    expect(requireValue(radios[1]).getAttribute('aria-checked')).toBe('false')
  })

  it('moves focus with arrows, Home and End, skipping disabled rows', async () => {
    await openMenu()
    key('ArrowDown')
    expect(requireValue(document.activeElement).textContent).toContain('Alpha')
    key('ArrowDown')
    expect(requireValue(document.activeElement).textContent).toContain('Gamma')
    key('End')
    expect(requireValue(document.activeElement).textContent).toContain('Mute')
    key('ArrowDown')
    expect(requireValue(document.activeElement).textContent).toContain('Alpha')
    key('ArrowUp')
    expect(requireValue(document.activeElement).textContent).toContain('Mute')
    key('Home')
    expect(requireValue(document.activeElement).textContent).toContain('Alpha')
  })

  it('type-ahead jumps to the matching row', async () => {
    await openMenu()
    key('g')
    expect(requireValue(document.activeElement).textContent).toContain('Gamma')
  })

  it('Escape closes and returns focus to the opener', async () => {
    const opener = await openMenu()
    key('ArrowDown')
    key('Escape')
    expect(requireValue(w.emitted('update:modelValue')).at(-1)).toEqual([false])
    await w.setProps({ modelValue: false })
    await nextTick()
    await nextTick()
    expect(document.activeElement).toBe(opener)
  })

  it('Tab closes the menu', async () => {
    await openMenu()
    key('Tab')
    expect(w.emitted('close')).toBeTruthy()
  })

  it('activating a row runs its action and closes', async () => {
    const a = vi.fn()
    await openMenu({ items: items({ a }) })
    key('ArrowDown')
    requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).click()
    expect(a).toHaveBeenCalled()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('opens at an anchor element instead of x/y', async () => {
    const anchor = document.createElement('div')
    anchor.getBoundingClientRect = () => new DOMRect(50, 70, 30, 20)
    await openMenu({ anchor })
    expect(requireValue(menu()).style.left).toBe('50px')
    expect(requireValue(menu()).style.top).toBe('90px')
  })

  it('stays backwards compatible with x/y and plain items', async () => {
    await openMenu({ x: 33, y: 44 })
    expect(requireValue(menu()).style.left).toBe('33px')
    expect(requireValue(menu()).style.top).toBe('44px')
    expect(rows().length).toBe(5)
  })
})

describe('ContextMenu submenu and lifecycle', () => {
  it('opens and navigates submenus by keyboard, closing only the nested panel on Escape or Left', async () => {
    const opened = vi.fn(), closed = vi.fn(), action = vi.fn()
    await openMenu({ ariaLabel: 'Settings', items: [{ type: 'submenu', id: 'sub', label: 'Audio', value: 'On', minWidth: 220, onOpen: opened, onClose: closed, items: [{ label: 'Keep open', keepOpen: true, action }] }] })
    const trigger = requireValue(document.querySelector<HTMLButtonElement>('[data-submenu-trigger]'))
    trigger.focus(); key('ArrowRight'); await nextTick(); await nextTick()
    expect(opened).toHaveBeenCalledTimes(1)
    expect(document.activeElement?.textContent).toBe('Keep open')
    requireValue(document.activeElement instanceof HTMLElement ? document.activeElement : null).click()
    expect(action).toHaveBeenCalled()
    expect(w.emitted('close')).toBeUndefined()
    key('Escape'); await nextTick()
    expect(document.activeElement).toBe(trigger)
    expect(document.querySelector('[data-submenu-panel]')).toBeNull()
    trigger.click(); await nextTick(); await nextTick()
    key('ArrowLeft'); await nextTick()
    expect(closed).toHaveBeenCalledTimes(2)
    expect(w.emitted('close')).toBeUndefined()
  })

  it('supports hover opening, cancellation and delayed submenu dismissal', async () => {
    vi.useFakeTimers()
    try {
      await openMenu({ items: [{ type: 'submenu', id: 's', label: 'Sub', items: [{ label: 'Nested' }] }, { label: 'Root' }, { type: 'submenu', id: 'disabled', label: 'Disabled', disabled: true, items: [] }] })
      const trigger = requireValue(document.querySelector('[data-submenu-trigger="s"]'))
      requireValue(trigger.parentElement).dispatchEvent(new MouseEvent('mouseenter'))
      await nextTick()
      expect(trigger.getAttribute('aria-expanded')).toBe('true')
      trigger.dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true })); await nextTick()
      requireValue(document.querySelector<HTMLButtonElement>('[data-menu-item], [data-menu-panel] > button')).dispatchEvent(new MouseEvent('mouseenter'))
      requireValue(document.querySelector('[data-submenu-panel]')).dispatchEvent(new MouseEvent('mouseenter'))
      await vi.advanceTimersByTimeAsync(300)
      expect(document.querySelector('[data-submenu-panel]')).not.toBeNull()
      const root = [...document.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === 'Root')
      requireValue(root).dispatchEvent(new MouseEvent('mouseenter'))
      await vi.advanceTimersByTimeAsync(300)
      expect(document.querySelector('[data-submenu-panel]')).toBeNull()
      requireValue(document.querySelector('[data-submenu-trigger="disabled"]')?.parentElement).dispatchEvent(new MouseEvent('mouseenter'))
      await nextTick()
      expect(document.querySelector('[data-submenu-panel]')).toBeNull()
    } finally { vi.useRealTimers() }
  })

  it('repositions from plain anchors, clips to viewport and closes on outside events', async () => {
    await openMenu({ anchor: { x: 25, y: 35, height: 10 }, items: [] })
    expect(menu().style.left).toBe('25px')
    expect(menu().style.top).toBe('45px')
    key('ArrowDown'); key('ArrowUp'); key('Home'); key('End'); key('z', { ctrlKey: true })
    await w.setProps({ anchor: { left: window.innerWidth + 10, bottom: window.innerHeight + 10 } }); await nextTick()
    await vi.waitFor(() => expect(Number.parseFloat(menu().style.left)).toBeLessThan(window.innerWidth))
    expect(Number.parseFloat(menu().style.top)).toBeLessThan(window.innerHeight)
    menu().dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    menu().dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(w.emitted('close')).toBeUndefined()
    document.body.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(1)
    window.dispatchEvent(new Event('scroll'))
    expect(w.emitted('close')).toHaveLength(2)
    await w.setProps({ modelValue: false })
    window.dispatchEvent(new Event('scroll'))
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    expect(w.emitted('close')).toHaveLength(2)
  })

  it('keeps action focus and closes an open submenu when hidden or unmounted', async () => {
    const target = document.createElement('input'); document.body.appendChild(target)
    const closed = vi.fn()
    await openMenu({ items: [{ type: 'submenu', id: 's', label: 'Sub', onClose: closed, items: [] }, { label: 'Focus', action: () => target.focus() }] })
    const trigger = requireValue(document.querySelector<HTMLButtonElement>('[data-submenu-trigger]'))
    trigger.click(); await nextTick()
    await w.setProps({ modelValue: false })
    expect(closed).toHaveBeenCalledTimes(1)
    await w.setProps({ modelValue: true }); await nextTick()
    const focus = requireValue([...document.querySelectorAll<HTMLButtonElement>('button')].find(b => b.textContent === 'Focus'))
    focus.click(); await nextTick()
    expect(document.activeElement).toBe(target)
    trigger.click(); await nextTick()
    w.unmount()
    expect(closed).toHaveBeenCalledTimes(2)
  })
})

it('flips overflowing submenus, shifts them upward and resets placement on a new submenu', async () => {
  const bounds = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    return this.hasAttribute('data-submenu-panel') ? new DOMRect(window.innerWidth - 20, window.innerHeight - 20, 100, 100) : new DOMRect(0, 0, 50, 50)
  })
  try {
    await openMenu({ anchor: {}, items: [
      { type: 'submenu', id: 'a', label: 'First', icon: Check, items: [{ label: 'Child' }] },
      { type: 'submenu', id: 'b', label: 'Second', items: [] }
    ] })
    expect(menu().style.left).toBe('0px'); expect(menu().style.top).toBe('0px')
    requireValue(document.querySelector<HTMLButtonElement>('[data-submenu-trigger="a"]')).click()
    await nextTick(); await nextTick(); await nextTick()
    await vi.waitFor(() => expect(requireValue(document.querySelector('[data-submenu-panel]')).classList.contains('right-full')).toBe(true))
    expect(Number.parseFloat(requireValue(document.querySelector<HTMLElement>('[data-submenu-panel]')).style.top)).toBeLessThan(-6)
    requireValue(document.querySelector<HTMLButtonElement>('[data-submenu-trigger="b"]')).click()
    await nextTick(); await nextTick()
    key('ArrowUp'); key('ArrowDown')
    expect(document.querySelector('[data-submenu="b"]')).not.toBeNull()
  } finally { bounds.mockRestore() }
})

it('handles nonmatching typeahead and disabled row selection without closing', async () => {
  await openMenu()
  key('ArrowUp')
  expect(document.activeElement?.textContent).toContain('Mute')
  key('z'); key('a', { metaKey: true }); key('a', { altKey: true }); key(' ')
  expect(document.activeElement?.textContent).toContain('Mute')
  const row = w.findAllComponents(ContextMenuRow).find(row => row.props('item').disabled)
  requireValue(row).vm.$emit('select', { label: 'Disabled', disabled: true })
  expect(w.emitted('select')).toBeUndefined()
  expect(w.emitted('close')).toBeUndefined()
})

it('restores the opener after an action removes focus and ignores disconnected openers', async () => {
  const opener = await openMenu({ items: [{ label: 'Blur', action: () => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur() } }] })
  const button = requireValue(document.querySelector<HTMLButtonElement>('[role="menuitem"]'))
  button.focus(); button.click(); await nextTick()
  expect(document.activeElement).toBe(opener)
  await w.setProps({ modelValue: false }); await w.setProps({ modelValue: true }); await nextTick()
  opener.remove(); key('Escape'); await nextTick()
  expect(document.activeElement).not.toBe(opener)
})

it('supports top-only anchors and closes on outside mouse presses', async () => {
  await openMenu({ anchor: { top: 20 } })
  expect(menu().style.top).toBe('20px')
  await w.setProps({ x: 25, y: 30, anchor: null }); await nextTick(); await nextTick()
  await vi.waitFor(() => expect(menu().style.left).toBe('25px'))
  document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  expect(w.emitted('close')).toHaveLength(1)
})

it('ignores hover dismissal with no submenu and handles queued keys after the menu was removed', async () => {
  await openMenu()
  const root = requireValue(document.querySelector<HTMLButtonElement>('[role="menuitem"]'))
  root.dispatchEvent(new MouseEvent('mouseenter'))
  expect(w.emitted('close')).toBeUndefined()
  const oldMenu = menu()
  await w.setProps({ modelValue: false })
  oldMenu.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  expect(w.emitted('close')).toBeUndefined()
})

it('cancels submenu placement when closed before the DOM update', async () => {
  await openMenu({ items: [{ type: 'submenu', id: 's', label: 'Sub', items: [{ label: 'Child' }] }] })
  requireValue(document.querySelector<HTMLButtonElement>('[data-submenu-trigger]')).dispatchEvent(new MouseEvent('click', { detail: 1, bubbles: true }))
  key('Escape')
  await nextTick()
  expect(document.querySelector('[data-submenu-panel]')).toBeNull()
  expect(w.emitted('close')).toHaveLength(1)
})
