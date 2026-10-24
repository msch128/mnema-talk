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
    class="absolute bottom-full left-0 right-0 z-30 mb-1.5 max-h-72 overflow-y-auto rounded-xl bg-mnema-elevated p-2 shadow-popover pop-in"
  >
    <div class="px-3 pb-2 pt-1 font-mono text-xs font-semibold uppercase tracking-widest text-mnema-tertiary">{{ $t('slash.title') }}</div>
    <div
      v-for="(item, i) in items"
      :id="`${id}-${i}`"
      :key="item.id"
      role="option"
      :aria-selected="i === active ? 'true' : 'false'"
      :data-slash-option="item.name"
      :class="[
        'flex h-11 cursor-pointer items-center gap-3 rounded-lg px-3 text-sm transition-colors',
        i === active ? 'bg-mnema-hover text-mnema-text ring-1 ring-inset ring-mnema-accent/25' : 'text-mnema-body-ink'
      ]"
      @mousedown.prevent="emit('pick', item)"
      @mousemove="emit('hover', i)"
    >
      <span v-if="item.builtin" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-mnema-accent/15 text-mnema-accent" aria-hidden="true"><Slash class="h-3 w-3" /></span>
      <UserAvatar v-else :user="{ display_name: item.bot_name, username: item.bot_name, avatar_url: item.bot_avatar_url }" size="xs" ring-class="bg-mnema-elevated" />
      <span class="shrink-0 font-mono font-semibold text-mnema-text">/{{ item.name }}</span>
      <span class="min-w-0 flex-1 truncate text-sm text-mnema-tertiary">{{ commandDescription(item) }}</span>
      <span class="min-w-0 max-w-[35%] truncate text-xs text-mnema-muted">{{ item.builtin ? $t('slash.builtinLabel') : item.bot_name }}</span>
    </div>
  </div>
</template>
