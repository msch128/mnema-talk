import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { ContextMenuItem } from './menuTypes'
import ContextMenu from './ContextMenu.vue'

// jsdom does no layout: each case states how tall the browser would draw the menu.
let w: ReturnType<typeof mount<typeof ContextMenu>> | undefined
afterEach(() => { w?.unmount(); vi.restoreAllMocks(); document.body.replaceChildren() })

const items: ContextMenuItem[] = [
  { label: 'Reply' },
  { type: 'submenu', id: 'more', label: 'More', items: [{ label: 'Forward' }, { label: 'Copy link' }] },
  { label: 'Delete' },
]

async function openWithHeight(height: number) {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => new DOMRect(10, 10, 200, height))
  w = mount(ContextMenu, { props: { modelValue: false, x: 10, y: 10, items }, attachTo: document.body })
  await w.setProps({ modelValue: true })
  await nextTick()
  await nextTick()
  return document.querySelector<HTMLElement>('[role="menu"]')
}

describe('ContextMenu on a window shorter than the menu', () => {
  it('floats beside the pointer while it fits', async () => {
    const menu = await openWithHeight(200)
    expect(menu?.hasAttribute('data-menu-sheet')).toBe(false)
    expect(menu?.style.left).toBe('10px')
  })

  it('opens as a sheet along the bottom edge, with submenus in place', async () => {
    const menu = await openWithHeight(window.innerHeight + 100)
    expect(menu?.hasAttribute('data-menu-sheet')).toBe(true)
    expect(menu?.className).toContain('bottom-0')
    expect(menu?.style.left).toBe('')
    const trigger = document.querySelector<HTMLElement>('[data-submenu-trigger="more"]')
    trigger?.click()
    await nextTick()
    await nextTick()
    const sub = document.querySelector<HTMLElement>('[data-submenu-panel]')
    expect(sub?.className).not.toContain('absolute')
    expect(sub?.textContent).toContain('Forward')
  })
})
