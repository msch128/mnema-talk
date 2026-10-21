import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { decodeOnboardingState, type OnboardingState, type OnboardingTipsInput } from '../types/rest'
import { NOTIFICATION_HINT_TIP, clearLegacyHint, legacyHintDismissed } from '../lib/onboardingTips'
import { useAuthStore } from './auth'

// The member's first-run record: the community's welcome text and rules, the
// rules they accepted, the tips they closed and whether the welcome flow is done.
export const useOnboardingStore = defineStore('onboarding', () => {
  const auth = useAuthStore()
  const state = ref<OnboardingState | null>(null)
  // True while the welcome flow is on screen, so tips wait for it.
  const welcomeOpen = ref(false)
  let loadedFor = ''
  let inflight: { userId: string; promise: Promise<void> } | null = null

  async function read(userId: string) {
    try {
      const fresh = await api('/api/onboarding', { decode: decodeOnboardingState })
      if (auth.user?.id !== userId) return
      state.value = fresh
      loadedFor = userId
      await migrateLegacyHint()
    } catch {
      // Offline or rate limited: stay quiet; the next start asks again.
    }
  }

  /** Loads the record once per signed-in account. */
  async function ensureLoaded() {
    const userId = auth.user?.id
    if (!userId || loadedFor === userId) return
    if (inflight?.userId !== userId) {
      const promise = read(userId).finally(() => { if (inflight?.promise === promise) inflight = null })
      inflight = { userId, promise }
    }
    await inflight.promise
  }

  /** Re-reads the record (the rules may have changed meanwhile). */
  async function reload() {
    const userId = auth.user?.id
    if (userId) await read(userId)
  }

  async function save(input: OnboardingTipsInput) {
    const userId = auth.user?.id
    const next = await api('/api/onboarding/tips', { method: 'PUT', json: input, decode: decodeOnboardingState })
    if (auth.user?.id === userId) state.value = next
  }

  /** Closes a tip at once and stores it; a failed save only means it may show again. */
  async function dismiss(id: string) {
    const current = state.value
    if (!current || current.dismissed_tips.includes(id)) return
    state.value = { ...current, dismissed_tips: [...current.dismissed_tips, id] }
    try { await save({ dismissed_tips: [id] }) } catch { /* shows again next start */ }
  }

  /** The browser-only dismissal of the notification hint moves to the server, once. */
  async function migrateLegacyHint() {
    if (!legacyHintDismissed()) return
    if (state.value?.dismissed_tips.includes(NOTIFICATION_HINT_TIP)) {
      clearLegacyHint()
      return
    }
    try {
      await save({ dismissed_tips: [NOTIFICATION_HINT_TIP] })
      clearLegacyHint()
    } catch { /* keep the flag and try again next start */ }
  }

  /** Accepts the rules version the member read. False when they changed meanwhile (the new ones are loaded). */
  async function acceptRules(version: number): Promise<boolean> {
    try {
      await api('/api/onboarding/accept', { method: 'POST', json: { version } })
    } catch {
      await reload()
      return false
    }
    if (state.value) state.value = { ...state.value, rules_accepted_version: version, rules_pending: false }
    return true
  }

  return { state, welcomeOpen, ensureLoaded, reload, save, dismiss, acceptRules }
})
