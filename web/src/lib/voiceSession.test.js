import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { save, recent, track, forget, untrack, STORAGE_KEY, RESUME_WINDOW_MS } from './voiceSession'

beforeEach(() => localStorage.clear())
afterEach(() => {
  untrack()
  vi.useRealTimers()
})

describe('voiceSession', () => {
  it('resumes within the window', () => {
    save('ch-1', 1_000)
    expect(recent(1_000 + RESUME_WINDOW_MS - 1)).toBe('ch-1')
  })

  it('does not resume after the window and cleans up', () => {
    save('ch-1', 1_000)
    expect(recent(1_000 + RESUME_WINDOW_MS + 1)).toBeNull()
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull()
  })

  it('ignores garbage and future timestamps', () => {
    localStorage.setItem(STORAGE_KEY, '{not json')
    expect(recent()).toBeNull()
    save('ch-1', Date.now() + 60_000)
    expect(recent()).toBeNull()
  })

  it('an explicit leave forgets the session', () => {
    track('ch-2')
    expect(recent()).toBe('ch-2')
    forget()
    expect(recent()).toBeNull()
  })

  it('the heartbeat keeps the session fresh while in a call', () => {
    vi.useFakeTimers()
    vi.setSystemTime(0)
    track('ch-3')
    vi.advanceTimersByTime(60_000) // a long call
    expect(recent(Date.now())).toBe('ch-3')
  })

  it('a reload stamps the moment the page went away', () => {
    vi.useFakeTimers()
    vi.setSystemTime(0)
    track('ch-4')
    vi.setSystemTime(4_000)
    window.dispatchEvent(new Event('pagehide'))
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY)).ts).toBe(4_000)
  })
})
