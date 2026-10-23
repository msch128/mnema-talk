import { describe, it, expect, beforeEach } from 'vitest'
import { setLocale } from '../i18n'
import { moderate, pendingModeration, customSeconds, durationLabel, formatRemaining, noticeText, kickedText } from './moderation'

beforeEach(() => {
  setLocale('en')
  pendingModeration.value = null
})

describe('moderation helpers', () => {
  it('turns a custom amount and unit into seconds, refusing anything else', () => {
    expect(customSeconds(5, 'seconds')).toBe(5)
    expect(customSeconds('3', 'minutes')).toBe(180)
    expect(customSeconds('2', 'hours')).toBe(7200)
    expect(customSeconds(1, 'days')).toBe(86400)
    for (const bad of [0, -1, 1.5, '', 'x', NaN]) expect(customSeconds(bad, 'minutes')).toBeNull()
  })

  it('names durations in the largest exact unit', () => {
    expect(durationLabel(7 * 86400)).toBe('1 w')
    expect(durationLabel(86400)).toBe('1 d')
    expect(durationLabel(3600)).toBe('1 h')
    expect(durationLabel(300)).toBe('5 min')
    expect(durationLabel(45)).toBe('45 s')
    expect(durationLabel(90)).toBe('90 s')
  })

  it('formats a countdown', () => {
    expect(formatRemaining(-5)).toBe('0:00')
    expect(formatRemaining(42_000)).toBe('0:42')
    expect(formatRemaining(41_001)).toBe('0:42')
    expect(formatRemaining(12 * 60_000 + 5_000)).toBe('12:05')
    expect(formatRemaining((3600 + 2 * 60 + 9) * 1000)).toBe('1:02:09')
    expect(formatRemaining((3 * 86400 + 4 * 3600 + 5) * 1000)).toBe('3 d 4 h')
  })

  it('words the notice a member reads', () => {
    expect(noticeText({ kind: 'timeout', duration_seconds: 3600, reason: 'spam' })).toBe('You were timed out for 1 h: spam')
    expect(noticeText({ kind: 'timeout', reason: '' })).toBe('You were timed out for ')
    expect(noticeText({ kind: 'timeout_removed', reason: '' })).toBe('Your timeout was lifted.')
    expect(noticeText({ kind: 'ban', reason: 'rude' })).toBe('You were banned: rude')
    expect(noticeText({ kind: 'ban', reason: '' })).toBe('You were banned')
  })

  it('words the note after a kick', () => {
    expect(kickedText('')).toBe('You were signed out by a moderator. You can sign in again right away.')
    expect(kickedText('take a break')).toBe('You were signed out by a moderator: take a break. You can sign in again right away.')
  })

  it('opens one dialog at a time; a new request cancels the old one', async () => {
    const first = moderate('timeout', { id: 'a', name: 'A' })
    const second = moderate('ban', { id: 'b', name: 'B' })
    expect(await first).toBe(false)
    expect(pendingModeration.value?.target.id).toBe('b')
    pendingModeration.value?.resolve(true)
    expect(await second).toBe(true)
    expect(pendingModeration.value).toBeNull()
    // Resolving a request that was replaced leaves the newer one alone.
    const old = moderate('timeout', { id: 'c', name: 'C' })
    const stale = pendingModeration.value
    void moderate('timeout', { id: 'd', name: 'D' })
    stale?.resolve(false)
    await old
    expect(pendingModeration.value?.target.id).toBe('d')
  })
})

describe('notices about messages', () => {
  it('words the removal of one or several messages', () => {
    expect(noticeText({ kind: 'message_deleted', reason: 'rude', channel_name: 'general' })).toBe('Your message in general was removed by someone with moderator rights: rude')
    expect(noticeText({ kind: 'message_deleted', reason: '' })).toBe('Your message in  was removed by someone with moderator rights')
    expect(noticeText({ kind: 'messages_deleted', reason: '', channel_name: 'general', count: 3 })).toBe('3 of your messages in general were removed by someone with moderator rights')
    expect(noticeText({ kind: 'messages_deleted', reason: '' })).toBe('0 of your messages in  were removed by someone with moderator rights')
  })
})
