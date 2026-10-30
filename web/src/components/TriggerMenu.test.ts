import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import TriggerMenu from './TriggerMenu.vue'
import { useContextMenuTrigger } from '../composables/useContextMenuTrigger'
import { requireValue } from '../test-fixtures.fixture'

let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = '' })

describe('TriggerMenu', () => {
  it('shows the menu of its trigger where it was asked, runs a row and closes', async () => {
    const action = vi.fn()
    let trigger!: ReturnType<typeof useContextMenuTrigger>
    wrapper = mount(defineComponent({
      setup() {
        trigger = useContextMenuTrigger(() => [{ id: 'row', label: 'Row', action }])
        return () => [h('button', { ...trigger.on }, 'target'), h(TriggerMenu, { trigger, ariaLabel: 'Things' })]
      }
    }), { attachTo: document.body })
    expect(document.querySelector('[role="menu"]')).toBeNull()

    wrapper.find('button').element.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 30, clientY: 40 }))
    await nextTick(); await nextTick()
    const menu = requireValue(document.querySelector<HTMLElement>('[role="menu"]'))
    expect(menu.getAttribute('aria-label')).toBe('Things')
    expect(menu.style.left).toBe('30px')

    requireValue(document.querySelector<HTMLButtonElement>('[data-menu-item="row"]')).click()
    await nextTick()
    expect(action).toHaveBeenCalledTimes(1)
    expect(trigger.menu.state.open).toBe(false)
    expect(document.querySelector('[role="menu"]')).toBeNull()
  })

  it('closes the trigger when the menu is dismissed', async () => {
    let trigger!: ReturnType<typeof useContextMenuTrigger>
    wrapper = mount(defineComponent({
      setup() {
        trigger = useContextMenuTrigger(() => [{ label: 'Row' }])
        return () => [h('button', { ...trigger.on }, 'target'), h(TriggerMenu, { trigger })]
      }
    }), { attachTo: document.body })
    wrapper.find('button').element.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 1, clientY: 1 }))
    await nextTick(); await nextTick()
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    await nextTick()
    expect(trigger.menu.state.open).toBe(false)
  })
})
