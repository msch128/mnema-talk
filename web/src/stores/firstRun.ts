import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeOnboardingFirstRun, type OnboardingFirstRun, type OnboardingFirstRunInput } from '../types/rest'
import { useAuthStore } from './auth'

/** The steps only opening a page of the console completes. */
export type VisitedStep = 'channels' | 'status'

// The Owner's "Get started" checklist (GET and PUT /api/admin/first-run). The
// server ticks the steps from real state; the store only reads it again when
// something that may have changed it happened.
export const useFirstRunStore = defineStore('firstRun', () => {
  const auth = useAuthStore()
  const state = ref<OnboardingFirstRun | null>(null)
  let loadedFor = ''

  const owner = computed(() => auth.isAdmin)
  /** The card is on screen: the Owner has not closed the checklist. */
  const visible = computed(() => owner.value && !!state.value && !state.value.hidden)
  const done = computed(() => state.value?.steps.filter(s => s.done).length ?? 0)

  async function put(input: OnboardingFirstRunInput) {
    const userId = auth.user?.id
    const next = await api('/api/admin/first-run', { method: 'PUT', json: input, decode: decodeOnboardingFirstRun })
    if (userId && auth.user?.id === userId) state.value = next
  }

  /** Reads the checklist (Owner only); a failed read leaves the card out rather than showing a wrong one. */
  async function reload() {
    const userId = auth.user?.id
    if (!userId || !owner.value) return
    try {
      const next = await api('/api/admin/first-run', { decode: decodeOnboardingFirstRun })
      if (auth.user?.id === userId) {
        state.value = next
        loadedFor = userId
      }
    } catch { /* offline or rate limited: no card until the next start */ }
  }

  async function ensureLoaded() {
    if (auth.user?.id && loadedFor !== auth.user.id) await reload()
  }

  /** Closes (true) or reopens (false) the checklist, at once on screen, then stored. */
  async function setHidden(hidden: boolean) {
    const before = state.value
    if (before) state.value = { ...before, hidden }
    try {
      await put({ hidden })
    } catch {
      state.value = before
    }
  }

  /** The Owner opened a console page that completes a step. */
  async function visit(step: VisitedStep) {
    if (!owner.value) return
    await ensureLoaded()
    const current = state.value
    if (!current || current.steps.find(s => s.id === step)?.done) return
    try { await put({ visited: [step] }) } catch { /* it ticks the next time they open the page */ }
  }

  function reset() {
    state.value = null
    loadedFor = ''
  }

  return { state, visible, done, ensureLoaded, reload, setHidden, visit, reset }
})
