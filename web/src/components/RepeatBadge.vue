<script setup lang="ts">
import { computed } from 'vue'
import { Repeat } from '@lucide/vue'
import type { CommunityEvent } from '../types/domain'
import { repeatKey } from '../lib/eventRecurrence'

// A calm line saying that an event is one of a series, and whether the series is over.
const props = defineProps<{ event: CommunityEvent }>()
const key = computed(() => repeatKey(props.event))
</script>

<template>
  <span v-if="key && event.series" data-testid="repeat-badge" class="inline-flex items-center gap-1 text-xs text-mnema-tertiary">
    <Repeat class="h-3.5 w-3.5" aria-hidden="true" />
    <span>{{ $t(key) }}</span>
    <span v-if="event.series.ended">· {{ $t('events.repeat.ended') }}</span>
  </span>
</template>
