<script setup lang="ts">
import { onMounted } from 'vue'
import { Calendar } from '@lucide/vue'
import { useEventsStore } from '../stores/events'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { openEvents } from '../lib/eventsDialog'

const features = useServerFeaturesStore()
const eventsStore = useEventsStore()

onMounted(() => {
  if (features.events) {
    void eventsStore.load()
  }
})
</script>

<template>
  <div v-if="features.events" class="mb-1 shrink-0">
    <button
      type="button"
      class="group flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-sm font-medium text-mnema-secondary hover:bg-mnema-hover hover:text-mnema-text transition-colors cursor-pointer"
      :aria-label="$t('events.events')"
      @click="openEvents()"
    >
      <div class="flex items-center gap-2 min-w-0">
        <Calendar class="w-4 h-4 shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
        <span class="truncate">{{ $t('events.events') }}</span>
      </div>
      <span
        v-if="eventsStore.upcomingCount > 0"
        class="ml-2 inline-flex items-center justify-center rounded-full bg-mnema-accent/20 px-1.5 py-0.5 text-xs font-semibold text-mnema-accent"
      >
        {{ eventsStore.upcomingCount }}
      </span>
    </button>

  </div>
</template>
