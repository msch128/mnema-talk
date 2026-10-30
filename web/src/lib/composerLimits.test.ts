import { describe, expect, it } from 'vitest'
import { COUNTER_FROM, MAX_MESSAGE_LENGTH, lengthState, slowModeRemaining } from './composerLimits'
import { ApiError, waitSeconds } from './api'

describe('the message length counter', () => {
  it('stays away from short messages', () => {
    expect(lengthState('hello')).toBeNull()
    expect(lengthState('a'.repeat(COUNTER_FROM - 1))).toBeNull()
  })

  it('counts down near the limit', () => {
    expect(lengthState('a'.repeat(COUNTER_FROM))).toEqual({ left: MAX_MESSAGE_LENGTH - COUNTER_FROM, over: false })
    expect(lengthState('a'.repeat(MAX_MESSAGE_LENGTH))).toEqual({ left: 0, over: false })
  })

  it('says by how much a message is too long', () => {
    expect(lengthState('a'.repeat(MAX_MESSAGE_LENGTH + 3))).toEqual({ left: -3, over: true })
  })

  it('counts characters as the server does, not UTF-16 units, and ignores outer spaces', () => {
    expect(lengthState('😀'.repeat(MAX_MESSAGE_LENGTH))).toEqual({ left: 0, over: false })
    expect(lengthState(`  ${'a'.repeat(MAX_MESSAGE_LENGTH)}  `)).toEqual({ left: 0, over: false })
  })
})

describe('the wait of slow mode', () => {
  it('is what is left until the member may write again', () => {
    expect(slowModeRemaining(undefined, 1000)).toBe(0)
    expect(slowModeRemaining(5000, 1000)).toBe(4000)
    expect(slowModeRemaining(5000, 9000)).toBe(0)
  })

  it('is read from the sentence of a refusal', () => {
    expect(waitSeconds('slow mode: wait 7 s before posting again')).toBe(7)
    expect(waitSeconds('too many requests: retry in 42 s')).toBe(42)
    expect(waitSeconds('too many requests')).toBeNull()
    expect(waitSeconds(undefined)).toBeNull()
    expect(new ApiError(429, 'RATE_LIMITED', 'x', 7).retryAfter).toBe(7)
  })
})
