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
