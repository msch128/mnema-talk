// Which steps the welcome flow shows for a member. Pure: the flow freezes the
// answer when it opens so a step does not vanish under the member's hands.
import type { OnboardingState } from '../types/rest'

export type WelcomeStep = 'welcome' | 'rules' | 'profile' | 'notifications'

// Accounts older than this when the flow first runs were here before the
// welcome flow existed (an upgrade) or have long found their way around:
// they are marked done instead of being walked through it.
export const FIRST_RUN_WINDOW_MS = 24 * 60 * 60 * 1000

/** Whether this is a new member who has not seen the flow yet. */
export function isFirstRun(state: OnboardingState, createdAt: string, now = Date.now()): boolean {
  if (state.welcome_done) return false
  const created = Date.parse(createdAt)
  return Number.isNaN(created) || now - created < FIRST_RUN_WINDOW_MS
}

/**
 * The steps to show: everything on a first run, only the rules when they are
 * pending later. A welcome page without a welcome text would be empty, so it is skipped.
 */
export function welcomeSteps(state: OnboardingState, opts: { firstRun: boolean; canAskNotifications: boolean }): WelcomeStep[] {
  const steps: WelcomeStep[] = []
  if (opts.firstRun && state.welcome_text.trim() !== '') steps.push('welcome')
  if (state.rules_pending) steps.push('rules')
  if (opts.firstRun) steps.push('profile')
  if (opts.firstRun && opts.canAskNotifications) steps.push('notifications')
  return steps
}
