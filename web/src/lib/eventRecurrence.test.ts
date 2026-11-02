import { afterEach, describe, expect, it, vi } from 'vitest'
import { browserZone, repeatKey, untilIso } from './eventRecurrence'
import { eventWithRoles } from '../eventRoles.fixture'

afterEach(() => vi.restoreAllMocks())

describe('event recurrence helpers', () => {
  it('names the zone of the browser, and nothing when it cannot', () => {
    expect(browserZone()).toBeTruthy()
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => { throw new Error('no Intl') })
    expect(browserZone()).toBeUndefined()
  })

  it('turns the chosen last day into the end of that day', () => {
    const iso = untilIso('2026-12-24')
    expect(iso).not.toBeNull()
    const d = new Date(iso!)
    expect([d.getFullYear(), d.getMonth(), d.getDate(), d.getHours(), d.getMinutes()]).toEqual([2026, 11, 24, 23, 59])
    expect(untilIso('')).toBeNull()
    expect(untilIso('not a date')).toBeNull()
  })

  it('says how often an occurrence repeats', () => {
    expect(repeatKey(eventWithRoles())).toBeNull()
    const series = { id: 's', frequency: 'biweekly' as const, timezone: 'UTC', index: 1, ended: false }
    expect(repeatKey(eventWithRoles({ series }))).toBe('events.repeat.biweekly')
  })
})
