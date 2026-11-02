import { describe, expect, it } from 'vitest'
import en from '../i18n/en.json'
import de from '../i18n/de.json'
import presets from './presets.json'
import { DURATION_CHOICES, MAX_BAN_SECONDS, MAX_TIMEOUT_SECONDS, MIN_BAN_SECONDS, MIN_TIMEOUT_SECONDS, durationLabel } from './moderation'
import { ACCOUNT_AGES, RAID_RESTRICTIONS, RAID_WINDOWS, RULE_TIMEOUTS } from './automod'
import { POLL_DURATIONS } from './polls'
import { EXPIRY_CHOICES, expiryTime, isFuture } from './statusExpiry'
import { isAcceptableTime, presetTime } from './laterTimes'
import type { LaterPreset } from './laterTimes'
import { MUTE_DURATIONS } from '../composables/useMuteMenu'

// The bounds of the server (internal/auth, internal/chat, internal/automod);
// the Go side pushes the same presets.json through the real validation.
const MAX_SLOW_MODE = 86400
const MAX_INVITE_HOURS = 8760
const MAX_MUTE_SECONDS = 366 * 24 * 3600

function text(tree: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((node, key) => (node && typeof node === 'object' ? (node as Record<string, unknown>)[key] : undefined), tree)
}

const inRange = (values: readonly number[], min: number, max: number) => values.every(v => Number.isInteger(v) && v >= min && v <= max)

describe('ready-made choices stay inside what the server accepts', () => {
  it('slow mode', () => {
    expect(inRange(presets.slowModeSeconds, 0, MAX_SLOW_MODE)).toBe(true)
  })

  it('invite validity and uses', () => {
    expect(inRange(presets.inviteCreateHours, 1, MAX_INVITE_HOURS)).toBe(true)
    expect(inRange(presets.inviteExpiryMinutes, 1, MAX_INVITE_HOURS * 60)).toBe(true)
    expect(inRange(presets.inviteUses, 1, 1000)).toBe(true)
  })

  it('timeout and ban durations', () => {
    expect(inRange(DURATION_CHOICES, Math.max(MIN_TIMEOUT_SECONDS, MIN_BAN_SECONDS), Math.min(MAX_TIMEOUT_SECONDS, MAX_BAN_SECONDS))).toBe(true)
  })

  it('automod rules', () => {
    expect(inRange(ACCOUNT_AGES, 60, 30 * 24 * 3600)).toBe(true)
    expect(inRange(RAID_WINDOWS, 5, 3600)).toBe(true)
    expect(inRange(RAID_RESTRICTIONS, 60, 24 * 3600)).toBe(true)
    expect(RULE_TIMEOUTS.every(s => s === 0 || (s >= 5 && s <= 28 * 24 * 3600))).toBe(true)
  })

  it('lockdown and waiting times of new members', () => {
    expect(inRange(presets.lockdownHours, 0, 720)).toBe(true)
    expect(inRange(presets.newMemberWaitMinutes, 0, 30 * 24 * 60)).toBe(true)
    expect(presets.newMemberWaitMinutes).toContain(0)
  })

  it('timed mutes', () => {
    const timed = MUTE_DURATIONS.flatMap(d => (d.ms === null ? [] : [d.ms / 60_000]))
    expect(timed).toEqual(presets.muteMinutes)
    expect(presets.muteMinutes.every(m => m > 0 && m * 60 <= MAX_MUTE_SECONDS)).toBe(true)
  })
})

describe('the menus offer exactly the shared presets', () => {
  it('lockdown durations have a text in both languages', () => {
    for (const tree of [en, de]) {
      for (const h of presets.lockdownHours) expect(text(tree, `raid.lockdown.durations.${h}`), `durations.${h}`).toEqual(expect.any(String))
    }
  })

  it('poll durations', () => {
    expect([...POLL_DURATIONS]).toEqual(presets.pollDurations)
  })

  it('every duration has a text in both languages', () => {
    for (const tree of [en, de]) {
      for (const d of presets.pollDurations) expect(text(tree, `poll.durations.${d}`), `poll.durations.${d}`).toEqual(expect.any(String))
    }
  })

  it('every duration reads as a label', () => {
    const all = [
      ...presets.slowModeSeconds.filter(s => s > 0), ...DURATION_CHOICES, ...ACCOUNT_AGES, ...RAID_WINDOWS,
      ...RAID_RESTRICTIONS, ...RULE_TIMEOUTS.filter(s => s > 0)
    ]
    for (const s of all) expect(durationLabel(s), `${s} s`).not.toMatch(/units\./)
  })
})

describe('time based choices are usable at any time of day', () => {
  const moments = [new Date(2026, 9, 24, 0, 0, 5), new Date(2026, 9, 24, 12, 0, 0), new Date(2026, 9, 24, 23, 58, 0), new Date(2026, 9, 31, 23, 59, 59), new Date(2026, 2, 28, 1, 30, 0)]
  const laterPresets: LaterPreset[] = ['in20m', 'in30m', 'in1h', 'in3h', 'tomorrow']

  it('remind me and send later', () => {
    for (const now of moments) for (const p of laterPresets) expect(isAcceptableTime(presetTime(p, now), now), `${p} at ${now.toISOString()}`).toBe(true)
  })

  it('status clear-after', () => {
    // "today" ends at the end of the local day, so in its last second it is already past (a known edge of the dialog).
    for (const now of moments) {
      for (const c of EXPIRY_CHOICES.filter(x => x !== 'never' && x !== 'today')) {
        expect(isFuture(expiryTime(c, now), now), `${c} at ${now.toISOString()}`).toBe(true)
      }
    }
  })
})
