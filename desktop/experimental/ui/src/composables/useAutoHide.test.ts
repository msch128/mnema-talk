import { required } from '../store-test-support.fixture'
import type { EffectScope } from 'vue'
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref, nextTick, effectScope } from 'vue'
import { useAutoHide, AUTO_HIDE_DELAY } from './useAutoHide'

let scope: EffectScope | undefined
function setup(opts: Parameters<typeof useAutoHide>[0] = {}) {
  scope = effectScope()
  return required(scope.run(() => useAutoHide(opts)))
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  scope?.stop()
  vi.useRealTimers()
})

describe('useAutoHide', () => {
  it('observes a disabled getter at timeout before its watcher has run', () => {
    let enabled = true
    const { visible, show } = setup({ enabled: () => enabled })
    enabled = false
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(visible.value).toBe(true)
    show()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('works without an effect scope', () => {
    const { visible } = useAutoHide({ delay: 1 })
    vi.advanceTimersByTime(1)
    expect(visible.value).toBe(false)
  })
  it('hides after 3 s without activity', () => {
    const { visible } = setup()
    expect(AUTO_HIDE_DELAY).toBe(3000)
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(2999)
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(visible.value).toBe(false)
  })

  it('shows again on activity and restarts the countdown', () => {
    const { visible, show } = setup()
    vi.advanceTimersByTime(3000)
    expect(visible.value).toBe(false)
    show()
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(2000)
    show() // a move before the end starts over
    vi.advanceTimersByTime(2000)
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(1000)
    expect(visible.value).toBe(false)
  })

  it('never hides while disabled (preview) and starts counting once enabled', async () => {
    const enabled = ref(false)
    const { visible } = setup({ enabled })
    vi.advanceTimersByTime(10_000)
    expect(visible.value).toBe(true)
    enabled.value = true
    await nextTick()
    vi.advanceTimersByTime(3000)
    expect(visible.value).toBe(false)
    // Disabled again: back and staying.
    enabled.value = false
    await nextTick()
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(10_000)
    expect(visible.value).toBe(true)
  })

  it('stays while pinned (menu open, focus inside) and hides 3 s after unpinning', async () => {
    const pinned = ref(false)
    const { visible } = setup({ pinned })
    pinned.value = true
    await nextTick()
    vi.advanceTimersByTime(20_000)
    expect(visible.value).toBe(true)
    pinned.value = false
    await nextTick()
    vi.advanceTimersByTime(2999)
    expect(visible.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(visible.value).toBe(false)
  })

  it('comes back when pinned while hidden (keyboard focus moves into it)', async () => {
    const pinned = ref(false)
    const { visible } = setup({ pinned })
    vi.advanceTimersByTime(3000)
    expect(visible.value).toBe(false)
    pinned.value = true
    await nextTick()
    expect(visible.value).toBe(true)
  })

  it('also respects a non-reactive pinned getter when the time is up', () => {
    let menuOpen = true
    const { visible } = setup({ pinned: () => menuOpen })
    vi.advanceTimersByTime(9000)
    expect(visible.value).toBe(true)
    menuOpen = false
    vi.advanceTimersByTime(3000)
    expect(visible.value).toBe(false)
  })

  it('stops its timer with the scope', () => {
    const { visible } = setup()
    required(scope).stop()
    vi.advanceTimersByTime(5000)
    expect(visible.value).toBe(true)
  })
})
