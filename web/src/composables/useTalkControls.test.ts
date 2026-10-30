import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref, type EffectScope } from 'vue'
import { required } from '../store-test-support.fixture'
import { AUTO_HIDE_DELAY } from './useAutoHide'
import { useCompactLayout } from './useCompactLayout'
import { MOVE_HINT_SESSIONS, useTalkControls } from './useTalkControls'

let scope: EffectScope | undefined
function setup(initial: { connected?: boolean; others?: number } = {}) {
  const connected = ref(initial.connected ?? true)
  const others = ref(initial.others ?? 2)
  scope = effectScope()
  const controls = required(scope.run(() => useTalkControls({ connected, others, menuOpen: () => false })))
  return { controls, connected, others }
}

beforeEach(() => {
  localStorage.clear()
  vi.useFakeTimers()
  useCompactLayout().isCompact.value = false
})
afterEach(() => {
  scope?.stop()
  vi.useRealTimers()
})

describe('useTalkControls on a desktop', () => {
  it('keeps the bar while the member is alone in the Talk', () => {
    const { controls } = setup({ others: 1 })
    vi.advanceTimersByTime(AUTO_HIDE_DELAY * 3)
    expect(controls.controlsShown.value).toBe(true)
    expect(controls.showMoveHint.value).toBe(false)
  })

  it('hides the bar with others and says how to bring it back', () => {
    const { controls } = setup()
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(controls.controlsShown.value).toBe(false)
    expect(controls.showMoveHint.value).toBe(true)
    expect(controls.showTapHint.value).toBe(false)
    controls.onActivity()
    expect(controls.controlsShown.value).toBe(true)
    expect(controls.showMoveHint.value).toBe(false)
  })

  it('stops hinting once it was shown in a few sessions', () => {
    localStorage.setItem('mnema_talk_controls_hint', String(MOVE_HINT_SESSIONS))
    const { controls } = setup()
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(controls.controlsShown.value).toBe(false)
    expect(controls.showMoveHint.value).toBe(false)
  })

  it('counts a session once, however often the bar hides', () => {
    const { controls } = setup()
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(controls.showMoveHint.value).toBe(true)
    controls.onActivity()
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(controls.showMoveHint.value).toBe(true)
    expect(localStorage.getItem('mnema_talk_controls_hint')).toBe('1')
  })
})

describe('useTalkControls on a phone', () => {
  it('asks for a tap instead of a mouse', () => {
    useCompactLayout().isCompact.value = true
    const { controls } = setup()
    vi.advanceTimersByTime(AUTO_HIDE_DELAY)
    expect(controls.showTapHint.value).toBe(true)
    expect(controls.showMoveHint.value).toBe(false)
  })
})
