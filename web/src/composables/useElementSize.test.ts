import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'
import { required } from '../store-test-support.fixture'
import { useElementSize } from './useElementSize'

afterEach(() => vi.unstubAllGlobals())

describe('useElementSize', () => {
  it('measures the current element, ignores unchanged notifications and replaces observers', async () => {
    const callbacks: ResizeObserverCallback[] = []
    const observers: TestObserver[] = []
    class TestObserver implements ResizeObserver {
      observe = vi.fn()
      unobserve = vi.fn()
      disconnect = vi.fn()
      constructor(callback: ResizeObserverCallback) { callbacks.push(callback); observers.push(this) }
    }
    vi.stubGlobal('ResizeObserver', TestObserver)
    const el = document.createElement('div')
    let width = 120
    Object.defineProperties(el, { clientWidth: { get: () => width }, clientHeight: { value: 60 } })
    const target = ref<HTMLElement | null>(null)
    const scope = effectScope()
    const size = required(scope.run(() => useElementSize(target)))
    expect(size.value).toEqual({ width: 0, height: 0 })
    target.value = el
    await nextTick()
    expect(size.value).toEqual({ width: 120, height: 60 })
    const measured = size.value
    required(callbacks[0])([], required(observers[0]))
    expect(size.value).toBe(measured)
    width = 180
    required(callbacks[0])([], required(observers[0]))
    expect(size.value.width).toBe(180)
    target.value = document.createElement('div')
    await nextTick()
    expect(required(observers[0]).disconnect).toHaveBeenCalledOnce()
    expect(size.value).toEqual({ width: 0, height: 0 })
    target.value = null
    await nextTick()
    expect(required(observers[1]).disconnect).toHaveBeenCalledOnce()
    scope.stop()
  })

  it('measures without ResizeObserver and disconnects the current observer on disposal', () => {
    vi.stubGlobal('ResizeObserver', undefined)
    expect(useElementSize(document.createElement('div')).value).toEqual({ width: 0, height: 0 })
    const disconnect = vi.fn()
    class Observer implements ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect = disconnect
    }
    vi.stubGlobal('ResizeObserver', Observer)
    const scope = effectScope()
    scope.run(() => useElementSize(document.createElement('div')))
    scope.stop()
    expect(disconnect).toHaveBeenCalledOnce()
  })
})
