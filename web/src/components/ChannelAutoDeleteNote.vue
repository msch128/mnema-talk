<script setup lang="ts">
import { computed } from 'vue'
import { Timer } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useDmChat } from '../composables/useDmChat'

// One calm line under the channel header while auto-delete is on, so the rule
// is stated in the channel and not only in its settings.
const chat = useChatStore()
const features = useServerFeaturesStore()
const { isDm } = useDmChat()
const days = computed(() => {
  const d = chat.activeChannel?.auto_delete_days ?? 0
  return features.channelAutoDelete && !isDm.value && d > 0 ? d : 0
})
</script>

<template>
  <p v-if="days" class="flex shrink-0 items-center gap-1.5 border-b border-mnema-hairline bg-mnema-canvas px-4 py-1 text-xs text-mnema-tertiary max-md:px-3" data-testid="auto-delete-note">
    <Timer class="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
    <span>{{ $t('autoDelete.line', { period: $t(`autoDelete.periods.${days}`) }) }}</span>
  </p>
</template>
