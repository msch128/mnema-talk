import { computed } from 'vue'
import { useOnboardingStore } from '../stores/onboarding'
import { useServerFeaturesStore } from '../stores/serverFeatures'

/** The product's name, shown only while the server has told nothing else. */
export const PRODUCT_NAME = 'Mnema Talk'

/**
 * What this server calls itself: the name the Owner set (live, the server
 * announces a rename), else the name from the public health answer (the host
 * while no name is set), else fallback (the product name).
 */
export function useCommunityName(fallback = PRODUCT_NAME) {
  const onboarding = useOnboardingStore()
  const features = useServerFeaturesStore()
  return computed(() => onboarding.state?.community_name.trim() || features.communityName || fallback)
}
