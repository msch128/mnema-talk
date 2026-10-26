import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { defineComponent, h, nextTick, ref } from 'vue'
import { useRovingMessages } from './useRovingMessages'

enableAutoUnmount(afterEach)

// Three rows, each with a name button, a link and a row-level tab stop of its own.
const List = defineComponent({
  props: { ids: { type: Array as () => string[], required: true } },
  setup(props, { expose }) {
    const el = ref<HTMLElement | null>(null)
    const { stopId } = useRovingMessages(el, () => props.ids)
    expose({ stopId })
    return () => h('div', { ref: el }, props.ids.map(id => h('div', { key: id, 'data-msg-id': id, tabindex: stopId.value === id ? 0 : -1 }, [
      h('button', { 'data-name': id }, 'name'),
      h('a', { href: '#', 'data-link': id }, 'link'),
      h('button', { tabindex: '0', 'data-explicit': id }, 'x')
    ])))
  }
})

interface ListApi { stopId: string | null }
const mountList = (ids = ['a', 'b', 'c']) => mount(List, { props: { ids }, attachTo: document.body })
const stopOf = (w: ReturnType<typeof mountList>) => (w.vm as unknown as ListApi).stopId
const tabindexOf = (w: ReturnType<typeof mountList>, selector: string) => w.get(selector).attributes('tabindex')

describe('useRovingMessages', () => {
  it('makes the newest row the one Tab stop to begin with', async () => {
    const w = mountList()
    await nextTick()
    expect(stopOf(w)).toBe('c')
    expect(w.get('[data-msg-id="c"]').attributes('tabindex')).toBe('0')
    expect(w.get('[data-msg-id="a"]').attributes('tabindex')).toBe('-1')
  })

  it('takes the controls inside the other rows out of the Tab order, and leaves the stop row alone', async () => {
    const w = mountList()
    await nextTick()
    expect(tabindexOf(w, '[data-name="a"]')).toBe('-1')
    expect(tabindexOf(w, '[data-link="a"]')).toBe('-1')
    expect(tabindexOf(w, '[data-explicit="a"]')).toBe('-1')
    expect(tabindexOf(w, '[data-name="c"]')).toBeUndefined()
    expect(tabindexOf(w, '[data-explicit="c"]')).toBe('0')
  })

  it('moves the stop to the row that takes focus and gives the controls back to it', async () => {
    const w = mountList()
    await nextTick()
    await w.get('[data-name="a"]').trigger('focusin')
    await nextTick()
    await nextTick()
    expect(stopOf(w)).toBe('a')
    expect(tabindexOf(w, '[data-name="a"]')).toBeUndefined()
    expect(tabindexOf(w, '[data-link="a"]')).toBeUndefined()
    expect(tabindexOf(w, '[data-explicit="a"]')).toBe('0')
    expect(tabindexOf(w, '[data-name="c"]')).toBe('-1')
    expect(tabindexOf(w, '[data-explicit="c"]')).toBe('-1')
  })

  it('also covers rows and controls that arrive later', async () => {
    const w = mountList(['a', 'b'])
    await nextTick()
    await w.setProps({ ids: ['a', 'b', 'c'] })
    await nextTick()
    await nextTick()
    expect(stopOf(w)).toBe('c')
    expect(tabindexOf(w, '[data-name="b"]')).toBe('-1')
    expect(tabindexOf(w, '[data-name="c"]')).toBeUndefined()
  })

  it('falls back to the newest row when the focused one is gone, and to none for an empty list', async () => {
    const w = mountList()
    await nextTick()
    await w.get('[data-name="a"]').trigger('focusin')
    await w.setProps({ ids: ['b', 'c'] })
    expect(stopOf(w)).toBe('c')
    await w.setProps({ ids: [] })
    expect(stopOf(w)).toBeNull()
  })

  it('ignores focus that is not inside a row', async () => {
    const w = mountList()
    await nextTick()
    const outside = document.createElement('button')
    w.element.append(outside)
    outside.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
    expect(stopOf(w)).toBe('c')
  })
})
