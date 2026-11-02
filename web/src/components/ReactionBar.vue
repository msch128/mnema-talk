<script setup lang="ts">
import type { PropType } from 'vue'
import type { ReactionSummary } from '../types/domain'
// Reaction badges under a message plus the dashed "add reaction" button
// with its palette.
import { computed } from 'vue'
import { SmilePlus } from '@lucide/vue'
import { parseEmojiToken, plainEmojiText } from '../lib/customEmoji'
import { useEmojiStore } from '../stores/emoji'
import { useAuthStore } from '../stores/auth'
import EmojiGlyph from './EmojiGlyph.vue'
import ReactionPalette from './ReactionPalette.vue'
import { PICKER_ANCHOR } from '../composables/useMessageActions'
import { usePermissionsStore } from '../stores/permissions'
import { waitSentence } from '../lib/newMember'

defineProps({
  reactions: { type: Array as PropType<ReactionSummary[]>, default: () => [] },
  // May the member add reactions (they can always take back their own).
  canAdd: { type: Boolean, default: true },
  // Whether this bar's palette is open.
  pickerOpen: { type: Boolean, default: false },
  // Smaller badges for thread replies.
  compact: { type: Boolean, default: false },
  // Smaller add-reaction icon (thread panel).
  smallIcon: { type: Boolean, default: false }
})
const emit = defineEmits<{ toggle: [emoji: string]; 'toggle-picker': []; 'close-picker': [] }>()

const authStore = useAuthStore()
const emojiStore = useEmojiStore()
const permissions = usePermissionsStore()
// A new member who may not react yet: the sentence that says until when (empty: nothing holds them back).
const reactWait = computed(() => {
  const ms = permissions.newMemberWait('react')
  return ms > 0 ? waitSentence('react', ms) : ''
})

function reacted(reaction: ReactionSummary) {
  const me = authStore.user?.id
  return !!me && !!reaction?.users?.includes(me)
}

// A deleted custom emoji stays on old messages as :name: text; it can be taken
// back but not added anew.
function gone(reaction: ReactionSummary) {
  const token = parseEmojiToken(reaction.emoji)
  return !!token && emojiStore.loaded && !emojiStore.byId.has(token.id)
}
</script>

<template>
  <div v-if="reactions?.length" :class="['flex flex-wrap gap-1 items-center', compact ? 'mt-1.5' : 'mt-2']">
    <button
      v-for="r in reactions"
      :key="r.emoji"
      type="button"
      :disabled="(!canAdd || gone(r)) && !reacted(r)"
      :aria-pressed="reacted(r) ? 'true' : 'false'"
      @click.stop="emit('toggle', r.emoji)"
      :class="[
        'focus-ring inline-flex items-center gap-1 py-0.5 rounded-full border transition cursor-pointer active:scale-95',
        compact ? 'px-1.5 text-xs' : 'px-2 text-sm',
        reacted(r)
          ? 'bg-mnema-accent/20 border-mnema-accent/40 text-mnema-accent font-semibold'
          : 'bg-mnema-surface hover:bg-mnema-band border-mnema-border text-mnema-muted'
      ]"
      v-tooltip="$t('chat.reaction', { emoji: plainEmojiText(r.emoji) })"
    >
      <span><EmojiGlyph :value="r.emoji" /></span>
      <span class="text-xs font-mono">{{ r.count }}</span>
    </button>

    <!-- Add-reaction button inline with reactions -->
    <div v-if="canAdd" :class="['relative inline-block', PICKER_ANCHOR]">
      <button
        type="button"
        data-testid="reaction-add"
        :aria-expanded="pickerOpen ? 'true' : 'false'"
        @click.stop="emit('toggle-picker')"
        class="btn btn-secondary inline-flex items-center justify-center w-7 h-7 rounded-full border-dashed cursor-pointer p-0"
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
    <!-- A new member who has to wait: the button is there, held back, and says until when -->
    <span v-else-if="reactWait" class="relative inline-block" data-testid="reaction-wait" v-tooltip="reactWait">
      <span class="btn btn-secondary inline-flex items-center justify-center w-7 h-7 rounded-full border-dashed p-0 opacity-50" aria-disabled="true" role="img" :aria-label="reactWait">
        <SmilePlus :class="smallIcon ? 'w-3.5 h-3.5' : 'w-4 h-4'" />
      </span>
    </span>
  </div>
</template>
