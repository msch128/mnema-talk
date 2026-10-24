<script setup lang="ts">
// The small green mark beside the leader of a temporary Talk, like the crown
// beside the Owner.
import { Flag } from '@lucide/vue'
</script>

<template>
  <span
    data-testid="talk-leader-mark"
    class="inline-flex shrink-0 text-mnema-accent"
    role="img"
    :aria-label="$t('talk.leader')"
    v-tooltip="$t('talk.leader')"
  >
    <Flag class="h-3.5 w-3.5" aria-hidden="true" />
  </span>
</template>
