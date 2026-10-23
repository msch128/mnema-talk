<script setup lang="ts">
import type { PropType } from 'vue'
import type { BotChannelCommand } from '../types/rest'
// The slash command list above a composer. Mouse picks; the keyboard is
// handled by the composer (useSlashCommands) so focus stays in the textarea.
import { Slash } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { commandDescription } from '../lib/slashCommands'

defineProps({
  items: { type: Array as PropType<BotChannelCommand[]>, required: true },
  active: { type: Number, default: 0 },
  id: { type: String, default: 'slash-suggestions' }
})
const emit = defineEmits<{ pick: [item: BotChannelCommand]; hover: [index: number] }>()
</script>

<template>
  <div
    :id="id"
    role="listbox"
    data-testid="slash-suggestions"
    :aria-label="$t('slash.listLabel')"
    class="absolute bottom-full left-0 right-0 z-30 mb-1.5 max-h-72 overflow-y-auto rounded-[10px] bg-mnema-elevated p-1.5 shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
  >
    <div class="px-2 pb-1 pt-0.5 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('slash.title') }}</div>
    <div
      v-for="(item, i) in items"
      :id="`${id}-${i}`"
      :key="item.id"
      role="option"
      :aria-selected="i === active ? 'true' : 'false'"
      :data-slash-option="item.name"
      :class="[
        'flex h-10 cursor-pointer items-center gap-2.5 rounded-md px-2 text-sm',
        i === active ? 'bg-mnema-hover text-mnema-text' : 'text-mnema-body-ink'
      ]"
      @mousedown.prevent="emit('pick', item)"
      @mousemove="emit('hover', i)"
    >
      <span v-if="item.builtin" class="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full bg-mnema-accent/20 text-mnema-accent" aria-hidden="true"><Slash class="h-3 w-3" /></span>
      <UserAvatar v-else :user="{ display_name: item.bot_name, username: item.bot_name, avatar_url: item.bot_avatar_url }" size="xs" ring-class="bg-mnema-elevated" />
      <span class="flex-shrink-0 font-mono font-semibold text-mnema-text">/{{ item.name }}</span>
      <span class="min-w-0 flex-1 truncate text-xs text-mnema-tertiary">{{ commandDescription(item) }}</span>
      <span class="flex-shrink-0 truncate text-xs text-mnema-muted">{{ item.builtin ? $t('slash.builtinLabel') : item.bot_name }}</span>
    </div>
  </div>
</template>
