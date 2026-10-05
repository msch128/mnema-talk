<script setup>
// Reaction badges under a message plus the dashed "add reaction" button
// with its palette.
import { SmilePlus } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import ReactionPalette from './ReactionPalette.vue'
import { PICKER_ANCHOR } from '../composables/useMessageActions'

defineProps({
  reactions: { type: Array, default: () => [] },
  // Whether this bar's palette is open.
  pickerOpen: { type: Boolean, default: false },
  // Smaller badges for thread replies.
  compact: { type: Boolean, default: false },
  // Smaller add-reaction icon (thread panel).
  smallIcon: { type: Boolean, default: false }
})
const emit = defineEmits(['toggle', 'toggle-picker', 'close-picker'])

const authStore = useAuthStore()

function reacted(reaction) {
  const me = authStore.user?.id
  return !!me && !!reaction?.users?.includes(me)
}
</script>

<template>
  <div v-if="reactions?.length" :class="['flex flex-wrap gap-1 items-center', compact ? 'mt-1.5' : 'mt-2']">
    <button
      v-for="r in reactions"
      :key="r.emoji"
      type="button"
      :aria-pressed="reacted(r) ? 'true' : 'false'"
      @click.stop="emit('toggle', r.emoji)"
      :class="[
        'inline-flex items-center gap-1 py-0.5 rounded-full border transition cursor-pointer active:scale-95',
        compact ? 'px-1.5 text-xs' : 'px-2 text-sm',
        reacted(r)
          ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
          : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
      ]"
      v-tooltip="$t('chat.reaction', { emoji: r.emoji })"
    >
      <span>{{ r.emoji }}</span>
      <span class="text-xs font-mono">{{ r.count }}</span>
    </button>

    <!-- Add-reaction button inline with reactions -->
    <div :class="['relative inline-block', PICKER_ANCHOR]">
      <button
        type="button"
        data-testid="reaction-add"
        :aria-expanded="pickerOpen ? 'true' : 'false'"
        @click.stop="emit('toggle-picker')"
        class="inline-flex items-center justify-center w-7 h-7 rounded-full border border-dashed border-mnema-border hover:border-mnema-accent text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition cursor-pointer text-sm"
        v-tooltip="$t('chat.addReaction')"
      >
        <SmilePlus :class="smallIcon ? 'w-3.5 h-3.5' : 'w-4 h-4'" />
      </button>

      <ReactionPalette
        v-if="pickerOpen"
        align="left"
        @pick="emit('toggle', $event)"
        @close="emit('close-picker')"
      />
    </div>
  </div>
</template>
