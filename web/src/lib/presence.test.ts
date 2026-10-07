import type { Presence } from '../types/domain'
import { describe, it, expect } from 'vitest'
import { isQuiet, snapshotToMap, applyPresenceUpdate, normalizeStatus, CHOOSABLE } from './presence'

describe('presence', () => {
  it('silences notifications only for dnd and focus', () => {
    expect(isQuiet('dnd')).toBe(true)
    expect(isQuiet('focus')).toBe(true)
    expect(isQuiet('away')).toBe(false)
    expect(isQuiet('online')).toBe(false)
  })

  it('never offers offline as a choice', () => {
    expect(CHOOSABLE).not.toContain('offline')
    expect(CHOOSABLE).toContain('focus')
  })

  it('reads both snapshot shapes', () => {
    expect(snapshotToMap(['a', 'b'])).toEqual({ a: 'online', b: 'online' })
    expect(snapshotToMap({ a: 'dnd', b: 'offline', c: 'weird' })).toEqual({ a: 'dnd' })
  })

  it('applies updates immutably', () => {
    const start: Record<string, Presence> = { a: 'online' }
    const next = applyPresenceUpdate(start, { user_id: 'a', status: 'away' })
    expect(next).toEqual({ a: 'away' })
    expect(start).toEqual({ a: 'online' })
    expect(applyPresenceUpdate(next, { user_id: 'a', status: 'offline' })).toEqual({})
    expect(normalizeStatus('nonsense')).toBe('offline')
  })
})
