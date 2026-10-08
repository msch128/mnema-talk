<script setup lang="ts">
import type { PropType } from 'vue'
import type { LivePresence } from './presentationTypes'
// The live-status mark on avatars and in menus. Each status has its own
// shape as well as its own colour, so it reads without colour vision too.
import { computed } from 'vue'
import { normalizeStatus } from '../lib/presence'

const props = defineProps({
  status: { type: String as PropType<LivePresence>, default: 'offline' },
  // Diameter in px.
  size: { type: Number, default: 10 },
  // Background colour of the 2px ring that cuts the dot out of the avatar.
  ringClass: { type: String, default: 'bg-mnema-canvas' },
  ring: { type: Boolean, default: true }
})

const s = computed<LivePresence>(() => normalizeStatus(props.status))
const colour = computed(() => ({
  online: 'text-mnema-accent',
  away: 'text-mnema-away',
  dnd: 'text-mnema-danger',
  focus: 'text-mnema-focus',
  offline: 'text-mnema-tertiary'
}[s.value]))
</script>

<template>
  <span
    :class="['inline-flex flex-shrink-0 items-center justify-center rounded-full', colour, ring ? ringClass : '']"
    :style="{ width: `${size + (ring ? 4 : 0)}px`, height: `${size + (ring ? 4 : 0)}px` }"
    :data-status="s"
    aria-hidden="true"
  >
    <svg :width="size" :height="size" viewBox="0 0 10 10" class="block">
      <!-- online: full disc -->
      <circle v-if="s === 'online'" cx="5" cy="5" r="5" fill="currentColor" />
      <!-- away: crescent -->
      <template v-else-if="s === 'away'">
        <mask :id="`moon-${size}`">
          <rect width="10" height="10" fill="white" />
          <circle cx="2.6" cy="2.6" r="3.4" fill="black" />
        </mask>
        <circle cx="5" cy="5" r="5" fill="currentColor" :mask="`url(#moon-${size})`" />
      </template>
      <!-- do not disturb: disc with a bar -->
      <template v-else-if="s === 'dnd'">
        <circle cx="5" cy="5" r="5" fill="currentColor" />
        <rect x="2" y="4" width="6" height="2" rx="1" fill="#0F1110" />
      </template>
      <!-- focus: disc with a centre point -->
      <template v-else-if="s === 'focus'">
        <circle cx="5" cy="5" r="5" fill="currentColor" />
        <circle cx="5" cy="5" r="2.2" fill="#0F1110" />
        <circle cx="5" cy="5" r="1" fill="currentColor" />
      </template>
      <!-- offline: hollow ring -->
      <circle v-else cx="5" cy="5" r="3.6" fill="none" stroke="currentColor" stroke-width="2.4" />
    </svg>
  </span>
</template>
