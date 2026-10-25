<script setup lang="ts">
import { clockTimeOptions } from '../lib/time'
import type { Message } from '../types/domain'
import type { ChatSearchRoot } from '../types/rest'
import { computed } from 'vue'
import { Hash, Volume2, Paperclip, ArrowRight, CornerDownRight, Pin, ChartNoAxesColumn, MessageCircle } from '@lucide/vue'
import { locale } from '../i18n'
import { splitHighlight } from '../lib/searchHighlight'
import { previewText } from '../lib/replies'
import UserAvatar from './UserAvatar.vue'
import { shownName } from '../lib/deletedUser'

// One hit. The text is shown as plain text (no Markdown markers, spoilers hidden, emoji as :name:);
// matches are highlighted by splitting it into nodes, never as HTML.
const props = defineProps<{
  msg: Message
  index: number
  selected: boolean
  channelName?: string | undefined
  voice: boolean
  conversation?: boolean | undefined
  terms: string[]
  root?: ChatSearchRoot | undefined
}>()
const emit = defineEmits<{ select: [msg: Message] }>()

const parts = computed(() => splitHighlight(previewText(props.msg.content), props.terms))
const rootParts = computed(() => splitHighlight(previewText(props.root?.snippet), props.terms))

function formatDate(iso: string) {
  if (!iso) return ''
  const at = new Date(iso)
  // Older hits carry their year, so "24 Oct" is never ambiguous.
  const year = at.getFullYear() === new Date().getFullYear() ? {} : { year: 'numeric' as const }
  return at.toLocaleDateString(locale.value, { month: 'short', day: 'numeric', ...year, ...clockTimeOptions(locale.value) })
}
</script>

<template>
  <div
    :id="`search-result-${index}`"
    role="option"
    :aria-selected="selected ? 'true' : 'false'"
    :class="[
      'flex w-full cursor-pointer items-start gap-3 rounded-lg px-3 py-2 text-left transition',
      selected ? 'bg-mnema-hover' : 'hover:bg-mnema-hover/60'
    ]"
    @mousedown.prevent
    @click="emit('select', msg)"
  >
    <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />

    <div class="flex-1 min-w-0">
      <div class="mb-0.5 flex items-center gap-2">
        <span class="min-w-0 truncate text-sm font-medium text-mnema-text">{{ shownName(msg) }}</span>
        <span v-if="channelName" class="flex min-w-0 max-w-[45%] items-center gap-1 rounded-sm border border-mnema-border bg-mnema-surface px-1.5 py-0.5 text-xs text-mnema-muted">
          <MessageCircle v-if="conversation" class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" />
          <Volume2 v-else-if="voice" class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" />
          <Hash v-else class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" />
          <span class="truncate">{{ channelName }}</span>
        </span>
        <span v-if="msg.parent_id" class="text-xs text-mnema-tertiary flex items-center gap-1">
          <CornerDownRight class="w-3 h-3" />
          {{ $t('chat.inThread') }}
        </span>
        <span v-if="msg.is_pinned" data-search-marker="pinned" class="flex items-center gap-1 text-xs text-mnema-accent">
          <Pin class="w-3 h-3" aria-hidden="true" />{{ $t('chat.searchPinned') }}
        </span>
        <span v-if="msg.poll" data-search-marker="poll" class="flex items-center gap-1 text-xs text-mnema-accent">
          <ChartNoAxesColumn class="w-3 h-3" aria-hidden="true" />{{ $t('poll.title') }}
        </span>
        <span class="ml-auto shrink-0 whitespace-nowrap text-xs text-mnema-tertiary">{{ formatDate(msg.created_at) }}</span>
      </div>

      <p v-if="root" data-search-root class="mb-1 flex min-w-0 items-baseline gap-1 border-l-2 border-mnema-border pl-2 text-xs text-mnema-tertiary">
        <span class="shrink-0 font-medium">{{ shownName(root) }}:</span>
        <span class="truncate">
          <template v-for="(part, i) in rootParts" :key="i"><mark v-if="part.match" class="bg-transparent font-semibold text-mnema-accent">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template>
        </span>
      </p>

      <p class="text-sm text-mnema-muted line-clamp-2 wrap-break-word">
        <template v-for="(part, i) in parts" :key="i"><mark v-if="part.match" class="bg-transparent font-semibold text-mnema-accent">{{ part.text }}</mark><template v-else>{{ part.text }}</template></template>
      </p>

      <div v-if="msg.attachments?.length" class="mt-1.5 flex items-center gap-1 text-xs text-mnema-accent">
        <Paperclip class="w-3 h-3" />
        <span>{{ msg.attachments.length }} {{ $t('chat.attachment') }}</span>
      </div>
    </div>

    <ArrowRight v-if="selected" class="mt-1 h-4 w-4 shrink-0 text-mnema-tertiary max-md:hidden" aria-hidden="true" />
  </div>
</template>
