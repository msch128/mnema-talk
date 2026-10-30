import { computed } from 'vue'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { communityIconUrl, type CommunityIconSize } from '../lib/communityIcon'

/** The URL of the community icon the Owner set, or '' while there is none (the Mnema mark shows then). */
export function useCommunityIcon(size: CommunityIconSize = 128) {
  const features = useServerFeaturesStore()
  return computed(() => communityIconUrl(features.communityIcon, size))
}
