import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { useDialog, focusableIn } from './useDialog'

const mounted: (() => void)[] = []
function setup(html: string, options: Parameters<typeof useDialog>[1] = {}) {
  const root = ref<HTMLElement | null>(null)
  const close = vi.fn()
  const Component = defineComponent({ setup() { useDialog(root, { onClose: close, ...options }); return () => h('div', { ref: root, tabindex: -1, innerHTML: html }) } })
  const wrapper = mount(Component, { attachTo: document.body })
  mounted.push(() => wrapper.unmount())
  return { wrapper, root, close }
}
function key(key: string, shiftKey = false) {
  const e = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true })
  document.dispatchEvent(e)
  return e
}
afterEach(() => { mounted.splice(0).reverse().forEach(unmount => unmount()); document.body.innerHTML = '' })

describe('useDialog', () => {
  it('filters inert and hidden controls, focuses the first content control and restores the opener', async () => {
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const { root, wrapper, close } = setup('<button data-dialog-close>close</button><button inert>inert</button><button aria-hidden="true">hidden</button><button id="content">content</button>')
    await nextTick()
    expect(document.activeElement?.id).toBe('content')
    expect(focusableIn(wrapper.element)).toHaveLength(2)
    expect(key('Escape').defaultPrevented).toBe(true)
    expect(close).toHaveBeenCalledOnce()
    expect(key('x').defaultPrevented).toBe(false)
    expect(root.value).toBe(wrapper.element)
    wrapper.unmount()
    expect(document.activeElement).toBe(opener)
  })

  it('wraps Tab at both ends and when focus is outside, leaving interior traversal to the browser', async () => {
    const { wrapper } = setup('<button id="first">first</button><button id="middle">middle</button><button id="last">last</button>')
    await nextTick()
    expect(key('Tab', true).defaultPrevented).toBe(true)
    expect(document.activeElement?.id).toBe('last')
    expect(key('Tab').defaultPrevented).toBe(true)
    expect(document.activeElement?.id).toBe('first')
    wrapper.find<HTMLButtonElement>('#middle').element.focus()
    expect(key('Tab').defaultPrevented).toBe(false)
    expect(key('Tab', true).defaultPrevented).toBe(false)
    document.body.tabIndex = -1
    document.body.focus()
    key('Tab')
    expect(document.activeElement?.id).toBe('first')
    document.body.focus()
    key('Tab', true)
    expect(document.activeElement?.id).toBe('last')
  })

  it('focuses the container with no controls and ignores keys after its ref disappears', async () => {
    const { root, close, wrapper } = setup('plain')
    await nextTick()
    expect(document.activeElement).toBe(wrapper.element)
    expect(key('Tab').defaultPrevented).toBe(true)
    key('Escape')
    expect(close).toHaveBeenCalledOnce()
    root.value = null
    expect(key('Escape').defaultPrevented).toBe(false)
  })

  it('honors selector and callback focus, falls back to autofocus, and only closes the top dialog', async () => {
    const outer = setup('<button data-autofocus id="auto">auto</button><button id="selected">selected</button>', { initialFocus: '#selected' })
    await nextTick()
    expect(document.activeElement?.id).toBe('selected')
    const inner = setup('<input data-autofocus id="inner">', { initialFocus: root => root.querySelector<HTMLElement>('#inner') })
    await nextTick()
    key('Escape')
    expect(inner.close).toHaveBeenCalledOnce()
    expect(outer.close).not.toHaveBeenCalled()
    inner.wrapper.unmount()
    key('Escape')
    expect(outer.close).toHaveBeenCalledOnce()
    const fallback = setup('<input data-autofocus id="fallback">', { initialFocus: '#missing' })
    await nextTick()
    expect(document.activeElement?.id).toBe('fallback')
    fallback.wrapper.unmount()
  })

  it('tolerates an absent root on mount and a disconnected opener on unmount', async () => {
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const root = ref<HTMLElement | null>(null)
    const wrapper = mount(defineComponent({ setup() { useDialog(root); return () => h('span') } }), { attachTo: document.body })
    mounted.push(() => wrapper.unmount())
    await nextTick()
    opener.remove()
    wrapper.unmount()
    expect(document.activeElement).toBe(document.body)
  })
})
