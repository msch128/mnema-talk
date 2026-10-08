<script setup lang="ts">
import type { PropType } from 'vue'
// Counts up from a server timestamp, once a second.
import { computed } from 'vue'
import { now, secondsSince, formatClock } from '../lib/clock'

const props = defineProps({ since: { type: String as PropType<string | undefined>, default: '' } })
const text = computed(() => (props.since ? formatClock(secondsSince(props.since, now.value)) : ''))
</script>

<template>
  <span v-if="text" class="font-mono tabular-nums" data-voice-timer>{{ text }}</span>
</template>
