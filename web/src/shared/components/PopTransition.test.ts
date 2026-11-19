import { describe, expect, it } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import PopTransition from './PopTransition.vue'

const open = ref(true)
const Host = defineComponent({
  setup: () => () => h(PopTransition, null, { default: () => (open.value ? h('div', { role: 'menu', class: 'pop-in' }, 'Menu') : null) })
})

describe('PopTransition', () => {
  it('lets a closing menu leave towards its trigger, hidden and out of reach', async () => {
    open.value = true
    const w = mount(Host, { attachTo: document.body, global: { stubs: { transition: false } } })
    const menu = w.get('[role="menu"]').element as HTMLElement
    expect(menu.getAttribute('aria-hidden')).toBeNull()

    open.value = false
    await nextTick()
    // Still on screen while it leaves, but no longer a menu anyone can reach.
    expect(menu.isConnected).toBe(true)
    expect(menu.classList.contains('pop-out')).toBe(true)
    expect(menu.getAttribute('aria-hidden')).toBe('true')
    expect(menu.inert).toBe(true)
    w.unmount()
  })

  it('shows the menu again at once when it reopens', async () => {
    open.value = true
    const w = mount(Host, { attachTo: document.body, global: { stubs: { transition: false } } })
    open.value = false
    await nextTick()
    open.value = true
    await nextTick()
    const menus = w.findAll('[role="menu"]').filter(m => m.attributes('aria-hidden') !== 'true')
    expect(menus).toHaveLength(1)
    w.unmount()
  })
})
