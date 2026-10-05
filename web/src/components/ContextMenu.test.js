import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import ContextMenu from './ContextMenu.vue'

let w
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

function items(actions = {}) {
  return [
    { label: 'Alpha', action: actions.a },
    { type: 'separator' },
    { label: 'Beta', disabled: true },
    { label: 'Gamma', action: actions.g },
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

const menu = () => document.querySelector('[role="menu"]')
const rows = () => [...document.querySelectorAll('[data-menu-nav]')]
const key = (k, init = {}) => menu().dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, ...init }))

describe('ContextMenu accessibility', () => {
  it('exposes menu, menuitem and menuitemradio roles', async () => {
    await openMenu()
    expect(menu()).toBeTruthy()
    expect(document.querySelectorAll('[role="menuitem"]').length).toBe(3)
    const radios = document.querySelectorAll('[role="menuitemradio"]')
    expect(radios.length).toBe(2)
    expect(radios[0].getAttribute('aria-checked')).toBe('true')
    expect(radios[1].getAttribute('aria-checked')).toBe('false')
  })

  it('moves focus with arrows, Home and End, skipping disabled rows', async () => {
    await openMenu()
    key('ArrowDown')
    expect(document.activeElement.textContent).toContain('Alpha')
    key('ArrowDown')
    expect(document.activeElement.textContent).toContain('Gamma')
    key('End')
    expect(document.activeElement.textContent).toContain('Mute')
    key('ArrowDown')
    expect(document.activeElement.textContent).toContain('Alpha')
    key('ArrowUp')
    expect(document.activeElement.textContent).toContain('Mute')
    key('Home')
    expect(document.activeElement.textContent).toContain('Alpha')
  })

  it('type-ahead jumps to the matching row', async () => {
    await openMenu()
    key('g')
    expect(document.activeElement.textContent).toContain('Gamma')
  })

  it('Escape closes and returns focus to the opener', async () => {
    const opener = await openMenu()
    key('ArrowDown')
    key('Escape')
    expect(w.emitted('update:modelValue').at(-1)).toEqual([false])
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
    document.activeElement.click()
    expect(a).toHaveBeenCalled()
    expect(w.emitted('close')).toBeTruthy()
  })

  it('opens at an anchor element instead of x/y', async () => {
    const anchor = document.createElement('div')
    anchor.getBoundingClientRect = () => ({ left: 50, bottom: 90, top: 70, right: 80, width: 30, height: 20 })
    await openMenu({ anchor })
    expect(menu().style.left).toBe('50px')
    expect(menu().style.top).toBe('90px')
  })

  it('stays backwards compatible with x/y and plain items', async () => {
    await openMenu({ x: 33, y: 44 })
    expect(menu().style.left).toBe('33px')
    expect(menu().style.top).toBe('44px')
    expect(rows().length).toBe(5)
  })
})
