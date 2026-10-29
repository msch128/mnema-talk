import { describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { useAutoGrow } from './useAutoGrow'

function setup(heightFor: (text: string) => number) {
  const text = ref('')
  let api!: ReturnType<typeof useAutoGrow>
  const w = mount(defineComponent({
    setup() {
      const el = ref<HTMLTextAreaElement | null>(null)
      api = useAutoGrow(el, text)
      return () => h('textarea', { ref: el })
    }
  }), { attachTo: document.body })
  const area = w.element as HTMLTextAreaElement
  Object.defineProperty(area, 'scrollHeight', { get: () => heightFor(text.value) })
  return { text, area, api, w }
}

describe('useAutoGrow', () => {
  it('grows with the text and shrinks back when it is cleared', async () => {
    Object.defineProperty(window, 'innerHeight', { value: 1000, configurable: true })
    const { text, area, api } = setup(t => 24 * Math.max(1, t.split('\n').length))
    api.fit()
    expect(area.style.height).toBe('24px')
    text.value = 'a\nb\nc'
    await nextTick()
    expect(area.style.height).toBe('72px')
    expect(api.multiline.value).toBe(true)
    text.value = ''
    await nextTick()
    expect(area.style.height).toBe('24px')
    expect(api.multiline.value).toBe(false)
  })

  it('stops at 45 percent of the window and scrolls inside', async () => {
    Object.defineProperty(window, 'innerHeight', { value: 1000, configurable: true })
    const { text, area, api } = setup(t => 24 * Math.max(1, t.split('\n').length))
    api.fit()
    text.value = Array(100).fill('x').join('\n')
    await nextTick()
    expect(area.style.height).toBe('450px')
    expect(area.style.overflowY).toBe('auto')
  })

  it('measures the single line from the styles, not from the first content it saw', async () => {
    Object.defineProperty(window, 'innerHeight', { value: 1000, configurable: true })
    // A restored three-line draft is there on mount.
    const { text, area, api } = setup(t => 24 * Math.max(1, t.split('\n').length))
    area.style.lineHeight = '24px'
    text.value = 'a\nb\nc'
    await nextTick()
    expect(api.multiline.value).toBe(true)
    text.value = ''
    await nextTick()
    expect(api.multiline.value).toBe(false)
  })

  it('refits when the width changes, not when only the height does, and disconnects', async () => {
    Object.defineProperty(window, 'innerHeight', { value: 1000, configurable: true })
    let notify!: (width: number) => void
    const disconnect = vi.fn()
    vi.stubGlobal('ResizeObserver', class {
      constructor(cb: ResizeObserverCallback) { notify = width => cb([{ contentRect: { width } } as ResizeObserverEntry], this as unknown as ResizeObserver) }
      observe = vi.fn()
      disconnect = disconnect
    })
    let lines = 1
    const { area, w, api } = setup(() => 24 * lines)
    api.fit()
    expect(area.style.height).toBe('24px')
    lines = 3
    notify(400)
    expect(area.style.height).toBe('72px')
    lines = 5
    notify(400)
    expect(area.style.height).toBe('72px')
    notify(300)
    expect(area.style.height).toBe('120px')
    w.unmount()
    expect(disconnect).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
