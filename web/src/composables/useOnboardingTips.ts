// Dismissible tips, stored on the server with the member's onboarding record so
// they follow the account across devices. The notification hint is one of them.
import { computed, onMounted } from 'vue'
import { TIPS, type TipId } from '../lib/onboardingTips'
import { useOnboardingStore } from '../stores/onboarding'

export function useOnboardingTips() {
  const onboarding = useOnboardingStore()
  onMounted(() => { void onboarding.ensureLoaded() })

  /** The next tip to show: none before the record is loaded, while the welcome flow is open or on a first run. */
  const activeTip = computed<TipId | null>(() => {
    const state = onboarding.state
    if (!state || !state.welcome_done || onboarding.welcomeOpen) return null
    return TIPS.find(id => !state.dismissed_tips.includes(id)) ?? null
  })

  return { activeTip, dismissTip: (id: TipId) => onboarding.dismiss(id) }
}
