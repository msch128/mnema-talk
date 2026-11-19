import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref, withDirectives } from 'vue'
import { mount } from '@vue/test-utils'
import { entering } from './entering'

const key = ref<string>('')
const List = defineComponent({
  setup: () => () => withDirectives(h('ul', [h('li', 'a'), h('li', 'b')]), [[entering, key.value, '', { end: true }]])
})

afterEach(() => { vi.useRealTimers() })

describe('v-entering', () => {
  it('marks the list while new content arrives, then lets it be', async () => {
    vi.useFakeTimers()
    key.value = ''
    const w = mount(List)
    expect(w.attributes('data-entering')).toBeUndefined()

    key.value = 'general'
    await nextTick()
    expect(w.attributes('data-entering')).toBe('end')

    vi.advanceTimersByTime(600)
    expect(w.attributes('data-entering')).toBeUndefined()
  })

  it('does not mark rows that arrive while the key stays the same', async () => {
    vi.useFakeTimers()
    key.value = 'general'
    const w = mount(List)
    vi.advanceTimersByTime(600)
    key.value = 'general'
    await w.vm.$forceUpdate()
    await nextTick()
    expect(w.attributes('data-entering')).toBeUndefined()
  })
})
