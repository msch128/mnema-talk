import type { OnboardingState, OnboardingTipsInput } from '../types/rest'

// Tip ids stored in the member's onboarding record (PUT /api/onboarding/tips).

/** The desktop-notification hint above the message list. */
export const NOTIFICATION_HINT_TIP = 'notif-hint'

/** The hint's dismissal used to live in this localStorage key. */
const LEGACY_HINT_KEY = 'mnema_notif_hint_dismissed'

/** Shown one at a time, in this order. Texts: i18n `tips.<id>`. */
export const TIPS = ['search', 'mention', 'talk'] as const
export type TipId = typeof TIPS[number]

export function legacyHintDismissed(): boolean {
  try { return localStorage.getItem(LEGACY_HINT_KEY) === '1' } catch { return false }
}

export function clearLegacyHint(): void {
  try { localStorage.removeItem(LEGACY_HINT_KEY) } catch { /* storage blocked: nothing to clear */ }
}

/** The record as the server sent it, plus a welcome flow this session already finished. */
export function withFinished(record: OnboardingState, finished: boolean): OnboardingState {
  return finished && !record.welcome_done ? { ...record, welcome_done: true } : record
}

/** The browser-only dismissal of the notification hint moves to the server, once. */
export async function migrateLegacyHint(dismissed: readonly string[] | undefined, save: (input: OnboardingTipsInput) => Promise<void>) {
  if (!legacyHintDismissed()) return
  if (dismissed?.includes(NOTIFICATION_HINT_TIP)) {
    clearLegacyHint()
    return
  }
  try {
    await save({ dismissed_tips: [NOTIFICATION_HINT_TIP] })
    clearLegacyHint()
  } catch { /* keep the flag and try again next start */ }
}
