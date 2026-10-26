<script setup lang="ts">
// The quick-switcher part of Ctrl/Cmd+K: while a name is typed, the
// conversations with that name are one click away above the message results.
import { computed } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { dmTitle } from '../lib/dm'
import { matchDms } from '../lib/dmList'
import DmAvatar from './DmAvatar.vue'

const MAX = 4

const props = defineProps({
  // What was typed, without filters.
  text: { type: String, default: '' }
})
const emit = defineEmits<{ jumped: [] }>()

const authStore = useAuthStore()
const chatStore = useChatStore()
const features = useServerFeaturesStore()

const matches = computed(() => (features.dmV2 ? matchDms(chatStore.dms, authStore.user?.id, props.text).slice(0, MAX) : []))

async function jump(id: string) {
  await chatStore.openDm(id)
  emit('jumped')
}
</script>

<template>
  <div v-if="matches.length" class="border-b border-mnema-hairline px-3 py-2" data-testid="search-jump">
    <p class="px-1 pb-1 text-xs font-medium text-mnema-tertiary">{{ $t('dm.jumpTo') }}</p>
    <button
      v-for="dm in matches"
      :key="dm.id"
      type="button"
      :data-jump="dm.id"
      class="flex min-h-9 w-full items-center gap-2.5 rounded-md px-2 py-1 text-left text-sm text-mnema-muted transition-colors hover:bg-mnema-hover hover:text-mnema-text focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent max-md:min-h-11"
      @click="jump(dm.id)"
    >
      <DmAvatar :dm="dm" ring-class="bg-mnema-canvas" />
      <span class="truncate">{{ dmTitle(dm, authStore.user?.id) }}</span>
    </button>
  </div>
</template>
