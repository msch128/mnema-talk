import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, KeepAlive, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useReloadOnActivate } from './useReloadOnActivate'

describe('useReloadOnActivate', () => {
  it('reloads when a kept-alive component returns, not on its first activation', async () => {
    const reload = vi.fn()
    const Tab = defineComponent({ setup() { useReloadOnActivate(reload); return () => h('div') } })
    const shown = ref(true)
    const w = mount(defineComponent({ setup: () => () => h(KeepAlive, null, shown.value ? [h(Tab)] : []) }))
    await nextTick()
    expect(reload).not.toHaveBeenCalled()
    shown.value = false
    await w.vm.$forceUpdate(); await nextTick()
    shown.value = true
    await w.vm.$forceUpdate(); await nextTick()
    expect(reload).toHaveBeenCalledTimes(1)
  })
})
