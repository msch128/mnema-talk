<script setup lang="ts">
import type { PropType } from 'vue'
// Live reactions over a participant's tile: each emoji rises and fades for a
// moment. With reduced motion it stays where it appears as a still badge until
// the store drops it (no float). Purely decorative: the sound of a Talk is the
// Talk, so nothing here is announced.
import EmojiGlyph from './EmojiGlyph.vue'

defineProps({
  reactions: { type: Array as PropType<{ id: number; emoji: string }[]>, default: () => [] },
  // A list row instead of a tile: smaller, floating above the row.
  compact: { type: Boolean, default: false }
})
</script>

<template>
  <div
    v-if="reactions.length"
    data-testid="talk-reactions"
    aria-hidden="true"
    :class="['absolute inset-0 z-40 rounded-[inherit] pointer-events-none', compact ? 'reaction-compact' : 'overflow-hidden']"
  >
    <span
      v-for="reaction in reactions"
      :key="reaction.id"
      data-testid="talk-reaction"
      :class="['reaction-float absolute leading-none drop-shadow-sm', compact ? 'bottom-full text-2xl' : 'bottom-12 text-4xl', `reaction-slot-${reaction.id % 4}`]"
    ><EmojiGlyph :value="reaction.emoji" /></span>
  </div>
</template>

<style scoped>
.reaction-float {
  animation: reaction-rise 2.8s ease-out forwards;
}
.reaction-slot-0 { left: 18%; }
.reaction-slot-1 { left: 36%; }
.reaction-slot-2 { left: 54%; }
.reaction-slot-3 { left: 72%; }
.reaction-compact .reaction-float {
  animation-name: reaction-rise-small;
}
.reaction-compact .reaction-slot-0 { left: 1rem; }
.reaction-compact .reaction-slot-1 { left: 2.5rem; }
.reaction-compact .reaction-slot-2 { left: 4rem; }
.reaction-compact .reaction-slot-3 { left: 5.5rem; }
@keyframes reaction-rise-small {
  0% { opacity: 0; transform: translateY(0) scale(0.8); }
  12% { opacity: 1; transform: translateY(-0.25rem) scale(1); }
  100% { opacity: 0; transform: translateY(-2.5rem) scale(1.1); }
}
@keyframes reaction-rise {
  0% { opacity: 0; transform: translateY(0) scale(0.8); }
  12% { opacity: 1; transform: translateY(-0.5rem) scale(1); }
  100% { opacity: 0; transform: translateY(-5.5rem) scale(1.15); }
}
/* Reduced motion: a still badge in the corner for as long as the reaction lasts. */
@media (prefers-reduced-motion: reduce) {
  .reaction-float,
  .reaction-compact .reaction-float {
    animation: none;
    left: auto;
    right: 0.5rem;
    bottom: 3rem;
  }
  .reaction-compact .reaction-float {
    bottom: 100%;
  }
}
</style>
