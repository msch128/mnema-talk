<script setup lang="ts">
import type { PropType } from 'vue'
import type { BotChannelCommand } from '../types/rest'
// The slash command list above a composer. Mouse picks; the keyboard is
// handled by the composer (useSlashCommands) so focus stays in the textarea.
// Commands are grouped by where they come from: the group is named once, not
// on every row. A fade at the bottom says there is more to scroll to.
import { nextTick, onMounted, ref, watch } from 'vue'
import { Slash } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { commandDescription } from '../lib/slashCommands'

const props = defineProps({
  items: { type: Array as PropType<BotChannelCommand[]>, required: true },
  active: { type: Number, default: 0 },
  id: { type: String, default: 'slash-suggestions' }
})
const emit = defineEmits<{ pick: [item: BotChannelCommand]; hover: [index: number] }>()

const list = ref<HTMLElement | null>(null)
const more = ref(false)

/** The group a command belongs to: the built-ins, or the bot that offers it. */
function groupOf(item: BotChannelCommand): string {
  return item.builtin ? '' : item.bot_name
}

function startsGroup(i: number): boolean {
  const item = props.items[i]
  return !!item && (i === 0 || groupOf(props.items[i - 1] as BotChannelCommand) !== groupOf(item))
}

function measure() {
  const el = list.value
  more.value = !!el && el.scrollHeight - el.scrollTop - el.clientHeight > 4
}

onMounted(measure)
watch(() => props.items, () => nextTick(measure))
// The keyboard moves the active row; keep it in view.
watch(() => props.active, index => {
  nextTick(() => list.value?.querySelector(`[id="${props.id}-${index}"]`)?.scrollIntoView?.({ block: 'nearest' }))
})
</script>

<template>
  <div
    :id="id"
    ref="list"
    role="listbox"
    data-testid="slash-suggestions"
    :aria-label="$t('slash.listLabel')"
    :data-more="more ? '' : undefined"
    class="fade-bottom absolute bottom-full left-0 right-0 z-30 mb-1.5 max-h-[min(26rem,55vh)] overflow-y-auto rounded-xl bg-mnema-elevated p-2 shadow-popover pop-in"
    @scroll.passive="measure"
  >
    <template v-for="(item, i) in items" :key="item.id">
      <div
        v-if="startsGroup(i)"
        role="presentation"
        data-testid="slash-group"
        class="truncate px-3 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary first:pt-1"
      >{{ item.builtin ? $t('slash.builtinLabel') : item.bot_name }}</div>
      <div
        :id="`${id}-${i}`"
        role="option"
        :aria-selected="i === active ? 'true' : 'false'"
        :data-slash-option="item.name"
        :class="[
          'flex h-10 cursor-pointer items-center gap-3 rounded-lg px-3 text-sm transition-colors',
          i === active ? 'bg-mnema-hover text-mnema-text ring-1 ring-inset ring-mnema-accent/25' : 'text-mnema-body-ink'
        ]"
        @mousedown.prevent="emit('pick', item)"
        @mousemove="emit('hover', i)"
      >
        <span v-if="item.builtin" class="flex h-6 w-6 shrink-0 items-center justify-center rounded-lg bg-mnema-accent/15 text-mnema-accent" aria-hidden="true"><Slash class="h-3 w-3" /></span>
        <UserAvatar v-else :user="{ display_name: item.bot_name, username: item.bot_name, avatar_url: item.bot_avatar_url }" size="xs" ring-class="bg-mnema-elevated" />
        <span class="shrink-0 font-mono font-semibold text-mnema-text">/{{ item.name }}</span>
        <span class="min-w-0 flex-1 truncate text-sm text-mnema-tertiary">{{ commandDescription(item) }}</span>
      </div>
    </template>
  </div>
</template>
