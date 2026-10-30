import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { useComposerLimits } from './useComposerLimits'
import { ApiError } from '../lib/api'
import { MAX_MESSAGE_LENGTH } from '../lib/composerLimits'
import { setLocale } from '../i18n'

function setup(state: { slow: number; exempt: boolean; text: string }) {
  let limits!: ReturnType<typeof useComposerLimits>
  mount(defineComponent({
    setup() {
      limits = useComposerLimits({ channelId: () => 'c1', slowModeSeconds: () => state.slow, exempt: () => state.exempt, text: () => state.text })
      return () => null
    }
  }))
  return limits
}

beforeEach(() => {
  setLocale('en')
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('limits of the composer', () => {
  it('says nothing while nothing limits the member', () => {
    expect(setup({ slow: 0, exempt: false, text: 'hi' }).hint.value).toBeNull()
  })

  it('counts the slow mode wait down after a message and clears it', async () => {
    const limits = setup({ slow: 10, exempt: false, text: 'hi' })
    limits.sent('c1')
    expect(limits.hint.value).toMatchObject({ blocks: true, text: 'Slow mode: you can write again in 0:10' })
    await vi.advanceTimersByTimeAsync(4000)
    expect(limits.hint.value?.text).toBe('Slow mode: you can write again in 0:06')
    await vi.advanceTimersByTimeAsync(7000)
    expect(limits.hint.value).toBeNull()
  })

  it('uses the rule of the channel the message was sent in, even after another one is opened', () => {
    const state = { slow: 10, exempt: false, text: 'hi' }
    const limits = setup(state)
    const rule = limits.rule()
    state.slow = 0
    limits.sent('c1', rule)
    state.slow = 10
    expect(limits.hint.value?.text).toBe('Slow mode: you can write again in 0:10')
  })

  it('does not hold back members who manage messages, or channels without slow mode', () => {
    const exempt = setup({ slow: 10, exempt: true, text: 'hi' })
    exempt.sent('c1')
    expect(exempt.hint.value).toBeNull()
    const none = setup({ slow: 0, exempt: false, text: 'hi' })
    none.sent('c1')
    expect(none.hint.value).toBeNull()
  })

  it('takes the wait from a refusal of the server', () => {
    const limits = setup({ slow: 30, exempt: false, text: 'hi' })
    limits.refused('c1', new ApiError(429, 'RATE_LIMITED', 'x', 12, true))
    expect(limits.hint.value?.text).toBe('Slow mode: you can write again in 0:12')
    const generic = setup({ slow: 30, exempt: false, text: 'hi' })
    generic.refused('c1', new ApiError(429, 'RATE_LIMITED', 'x', 12))
    expect(generic.hint.value).toBeNull()
    const other = setup({ slow: 30, exempt: false, text: 'hi' })
    other.refused('c1', new ApiError(500, 'INTERNAL_ERROR', 'x'))
    other.refused('c1', new Error('boom'))
    expect(other.hint.value).toBeNull()
  })

  it('shows the characters left near the limit and blocks a message that is too long', () => {
    const state = { slow: 0, exempt: false, text: 'a'.repeat(3900) }
    expect(setup(state).hint.value).toEqual({ text: '100 characters left', blocks: false, tone: 'calm' })
    state.text = 'a'.repeat(MAX_MESSAGE_LENGTH + 5)
    expect(setup(state).hint.value).toEqual({ text: '5 characters too many (at most 4000)', blocks: true, tone: 'warn' })
  })
})
