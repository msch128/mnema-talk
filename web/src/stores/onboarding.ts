import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'
import { retryWithBackoff } from '../lib/retry'
import { decodeOnboardingState, type OnboardingState, type OnboardingTipsInput } from '../types/rest'
import { migrateLegacyHint, withFinished } from '../lib/onboardingTips'
import { useAuthStore } from './auth'

const RETRY_LOAD_MS = 1500
// Waits before the retries of storing "welcome done"; the list length bounds them.
const SAVE_BACKOFF_MS = [1000, 3000, 9000]

// The member's first-run record: the community's welcome text and rules, the
// rules they accepted, the tips they closed and whether the welcome flow is done.
export const useOnboardingStore = defineStore('onboarding', () => {
  const auth = useAuthStore()
  const state = ref<OnboardingState | null>(null)
  // True while the welcome flow is on screen, so tips wait for it.
  const welcomeOpen = ref(false)
  let loadedFor = ''
  // The account whose welcome flow this session finished; a lagging server answer must not undo it.
  let finishedFor = ''
  const pending = new Map<string, Promise<boolean>>()

  const adopt = (userId: string, fresh: OnboardingState) => withFinished(fresh, finishedFor === userId)
  async function read(userId: string): Promise<boolean> {
    try {
      const fresh = await api('/api/onboarding', { decode: decodeOnboardingState })
      if (auth.user?.id !== userId) return false
      state.value = adopt(userId, fresh)
      loadedFor = userId
      await migrateLegacyHint(state.value.dismissed_tips, save)
      return true
    } catch {
      return false // offline or rate limited: stay quiet; ensureLoaded asks once more
    }
  }

  const stillSignedIn = (userId: string) => () => auth.user?.id === userId

  /** Loads the record once per signed-in account; a failed load is tried once more. */
  async function ensureLoaded() {
    const userId = auth.user?.id
    if (!userId || loadedFor === userId) return
    let load = pending.get(userId)
    if (!load) {
      load = retryWithBackoff(() => read(userId), [RETRY_LOAD_MS], stillSignedIn(userId)).finally(() => pending.delete(userId))
      pending.set(userId, load)
    }
    await load
  }

  /** Re-reads the record (the rules may have changed meanwhile). */
  async function reload() {
    const userId = auth.user?.id
    if (userId) await read(userId)
  }
  async function save(input: OnboardingTipsInput) {
    const userId = auth.user?.id
    const next = await api('/api/onboarding/tips', { method: 'PUT', json: input, decode: decodeOnboardingState })
    if (userId && auth.user?.id === userId) state.value = adopt(userId, next)
  }

  /** The welcome flow is done: kept locally at once (a tab switch must not reopen it), then stored with a few retries. */
  async function markWelcomeDone() {
    const userId = auth.user?.id
    if (!userId) return
    finishedFor = userId
    if (state.value && !state.value.welcome_done) state.value = { ...state.value, welcome_done: true }
    const store = () => save({ welcome_done: true }).then(() => true, () => false)
    await retryWithBackoff(store, SAVE_BACKOFF_MS, stillSignedIn(userId)) // if it keeps failing, the next session asks again
  }

  /** Closes a tip at once and stores it; a failed save only means it may show again. */
  async function dismiss(id: string) {
    const current = state.value
    if (!current || current.dismissed_tips.includes(id)) return
    state.value = { ...current, dismissed_tips: [...current.dismissed_tips, id] }
    try { await save({ dismissed_tips: [id] }) } catch { /* shows again next start */ }
  }

  /** Accepts the rules version the member read. False when they changed meanwhile (the new ones are loaded). */
  async function acceptRules(version: number): Promise<boolean> {
    const accepted = await api('/api/onboarding/accept', { method: 'POST', json: { version } }).then(() => true, () => false)
    if (!accepted) {
      await reload()
      return false
    }
    if (state.value) state.value = { ...state.value, rules_accepted_version: version, rules_pending: false }
    return true
  }

  return { state, welcomeOpen, ensureLoaded, reload, save, markWelcomeDone, dismiss, acceptRules }
})
