<script setup lang="ts">
import type { PropType } from 'vue'
import type { MentionSuggestion } from '../lib/mentionQuery'
// The @mention list above a composer. Mouse picks; the keyboard is handled
// by the composer (useComposerAssist) so focus stays in the textarea.
import { Users, Radio } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { useChatStore } from '../stores/chat'

defineProps({
  items: { type: Array as PropType<MentionSuggestion[]>, required: true },
  active: { type: Number, default: 0 },
  id: { type: String, default: 'mention-suggestions' }
})
const emit = defineEmits<{ pick: [item: MentionSuggestion]; hover: [index: number] }>()
const chatStore = useChatStore()
</script>

<template>
  <div
    :id="id"
    role="listbox"
    data-testid="mention-suggestions"
    :aria-label="$t('mention.listLabel')"
    class="absolute bottom-full left-0 right-0 z-30 mb-1.5 max-h-72 overflow-y-auto rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
  >
    <div class="px-2 pb-1 pt-0.5 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('mention.title') }}</div>
    <div
      v-for="(item, i) in items"
      :id="`${id}-${i}`"
      :key="item.username"
      role="option"
      :aria-selected="i === active ? 'true' : 'false'"
      :data-mention-option="item.username"
      :class="[
        'flex h-9 cursor-pointer items-center gap-2.5 rounded-md px-2 text-sm',
        i === active ? 'bg-mnema-hover text-mnema-text' : 'text-mnema-body-ink'
      ]"
      @mousedown.prevent="emit('pick', item)"
      @mousemove="emit('hover', i)"
    >
      <template v-if="item.group">
        <span class="flex h-6 w-6 items-center justify-center rounded-full bg-mnema-band text-mnema-mint">
          <Users v-if="item.username === 'all'" class="h-3.5 w-3.5" />
          <Radio v-else class="h-3.5 w-3.5" />
        </span>
        <span class="font-semibold text-mnema-text">@{{ item.username }}</span>
        <span class="truncate text-xs text-mnema-tertiary">{{ $t(`mention.${item.username}Hint`) }}</span>
      </template>
      <template v-else>
        <UserAvatar :user="item.user" size="xs" show-status :status="chatStore.presenceOf(item.user.id)" ring-class="bg-mnema-elevated" />
        <span class="truncate font-medium text-mnema-text">{{ item.display_name }}</span>
        <span class="truncate font-mono text-xs text-mnema-tertiary">@{{ item.username }}</span>
      </template>
    </div>
  </div>
</template>
