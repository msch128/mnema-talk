<script setup lang="ts">
// The microphone / headphone marks of a muted or deafened Talk member.
import { MicOff, HeadphoneOff } from '@lucide/vue'

defineProps({
  muted: { type: Boolean, default: false },
  deafened: { type: Boolean, default: false },
  // px size of each icon
  size: { type: Number, default: 14 }
})
</script>

<template>
  <span v-if="muted || deafened" class="inline-flex flex-shrink-0 items-center gap-0.5 text-mnema-danger" data-mute-marks>
    <MicOff :style="{ width: `${size}px`, height: `${size}px` }" role="img" :aria-label="$t('voice.memberMuted')" />
    <HeadphoneOff v-if="deafened" :style="{ width: `${size}px`, height: `${size}px` }" role="img" :aria-label="$t('voice.memberDeafened')" />
  </span>
</template>
