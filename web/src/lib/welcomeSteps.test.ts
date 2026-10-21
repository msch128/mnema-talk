import type { OnboardingState } from '../types/rest'
import { describe, expect, it } from 'vitest'
import { FIRST_RUN_WINDOW_MS, isFirstRun, welcomeSteps } from './welcomeSteps'

const base: OnboardingState = {
  community_name: '', welcome_text: '', rules_text: '', rules_version: 0, rules_accepted_version: 0,
  rules_pending: false, start_channel_id: null, dismissed_tips: [], welcome_done: false
}
const now = Date.parse('2030-01-10T12:00:00Z')

describe('isFirstRun', () => {
  it('is true for a new account that has not finished the flow', () => {
    expect(isFirstRun(base, '2030-01-10T11:00:00Z', now)).toBe(true)
  })
  it('is false for an account older than the window (an upgrade)', () => {
    expect(isFirstRun(base, new Date(now - FIRST_RUN_WINDOW_MS - 1).toISOString(), now)).toBe(false)
  })
  it('is false once the flow is done', () => {
    expect(isFirstRun({ ...base, welcome_done: true }, '2030-01-10T11:00:00Z', now)).toBe(false)
  })
  it('treats an unreadable creation date as new', () => {
    expect(isFirstRun(base, 'nonsense', now)).toBe(true)
  })
})

describe('welcomeSteps', () => {
  it('runs everything on a first run, rules only when pending', () => {
    expect(welcomeSteps(base, { firstRun: true, canAskNotifications: true })).toEqual(['welcome', 'profile', 'notifications'])
    expect(welcomeSteps({ ...base, rules_pending: true }, { firstRun: true, canAskNotifications: false })).toEqual(['welcome', 'rules', 'profile'])
  })
  it('asks only for the rules later', () => {
    expect(welcomeSteps({ ...base, welcome_done: true, rules_pending: true }, { firstRun: false, canAskNotifications: true })).toEqual(['rules'])
    expect(welcomeSteps({ ...base, welcome_done: true }, { firstRun: false, canAskNotifications: true })).toEqual([])
  })
})
