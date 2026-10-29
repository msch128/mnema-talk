import { describe, expect, it } from 'vitest'
import { botInitial, curlExample, endOfDayIso, hasIntent, isPowerfulRole, relativeTime, toggleIntent } from './bots'
import { PERMISSION_BITS } from './permissions'

describe('bots helpers', () => {
  it('toggles intents', () => {
    expect(hasIntent(513, 512)).toBe(true)
    expect(hasIntent(1, 2)).toBe(false)
    expect(toggleIntent(1, 512, true)).toBe(513)
    expect(toggleIntent(513, 512, false)).toBe(1)
  })
  it('flags administrator and admin-area roles', () => {
    expect(isPowerfulRole(PERMISSION_BITS.administrator)).toBe(true)
    expect(isPowerfulRole(PERMISSION_BITS.manage_bots)).toBe(true)
    expect(isPowerfulRole(PERMISSION_BITS.send_messages | PERMISSION_BITS.view_channel)).toBe(false)
  })
  it('formats relative time', () => {
    const now = Date.parse('2026-01-02T12:00:00Z')
    expect(relativeTime(undefined, 'en', now)).toBe('')
    expect(relativeTime('nope', 'en', now)).toBe('')
    expect(relativeTime('2026-01-02T11:59:30Z', 'en', now)).toContain('30 seconds ago')
    expect(relativeTime('2026-01-02T11:55:00Z', 'en', now)).toContain('5 minutes ago')
    expect(relativeTime('2026-01-02T09:00:00Z', 'en', now)).toContain('3 hours ago')
    expect(relativeTime('2025-12-30T12:00:00Z', 'en', now)).toContain('3 days ago')
  })
  it('builds the curl example and initials', () => {
    expect(curlExample('https://x.test')).toBe('curl -H "Authorization: Bot <token>" https://x.test/api/bot/v1/users/@me')
    expect(botInitial(' ab')).toBe('A')
    expect(botInitial('  ')).toBe('?')
    expect(botInitial('🤖 Helper')).toBe('🤖')
  })
  it('turns a date into an RFC3339 end of day', () => {
    expect(endOfDayIso('')).toBeUndefined()
    expect(endOfDayIso('2026-03-04')).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/)
  })
})
