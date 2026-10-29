import { computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { useFirstRunStore } from '../stores/firstRun'
import { useOnboardingStore } from '../stores/onboarding'

/**
 * Whether the Owner's "Get started" list is on screen: not before the welcome
 * flow is done, and only where the Owner is reading a text channel. The tips
 * wait for it, so two bars never stack under the header.
 */
export function useFirstRunShown() {
  const chat = useChatStore()
  const firstRun = useFirstRunStore()
  const onboarding = useOnboardingStore()
  return computed(() => firstRun.visible && onboarding.state?.welcome_done === true && !onboarding.welcomeOpen && chat.activeChannel?.type === 'text')
}
