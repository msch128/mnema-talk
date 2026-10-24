<script setup lang="ts">
import { dateTime } from '../lib/time'
import type { Message } from '../types/domain'
import { computed, ref, watch } from 'vue'
import { ArrowUpRight, Inbox, Hash, Volume2, CornerDownRight, Loader2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useMentionsInbox } from '../composables/useMentionsInbox'
import { useDismissable } from '../composables/useDismissable'
import { locale } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import Badge from './Badge.vue'
import HeaderPopover from './HeaderPopover.vue'
import StatePanel from './StatePanel.vue'
import { shownName } from '../lib/deletedUser'
import { previewText } from '../lib/replies'

// The mentions inbox in the channel header: a button with the number of
// unread mentions and a popover listing them; a click jumps to the message.
const chatStore = useChatStore()
const inbox = useMentionsInbox()
// Open from the header's More menu when the header is narrow: no button of its own then.
defineProps<{ bare?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const root = ref<HTMLElement | null>(null)

// Unread mentions over all channels, from the read state (the server counts
// only channels the reader may view).
const unread = computed(() => Object.values(chatStore.readStates).reduce((sum, s) => sum + (s.mention_count || 0), 0))
const badge = computed(() => (unread.value > 99 ? '99+' : String(unread.value)))
const channelNames = computed(() => new Map(chatStore.allChannels.map(c => [c.id, c])))

useDismissable(root, () => { open.value = false }, { active: open })

watch(open, isOpen => {
  if (isOpen) inbox.load()
})

function pick(msg: Message) {
  open.value = false
  chatStore.goToMessage(msg)
}

const when = (iso: string) => dateTime(iso, locale.value)
</script>

<template>
  <div ref="root" class="relative">
    <button
      v-if="!bare"
      type="button"
      data-testid="mentions-inbox-button"
      class="btn btn-icon max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('mentions.inbox')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('mentions.inbox')"
      @click="open = !open"
    >
      <Inbox class="h-5 w-5" />
      <Badge v-if="unread > 0" variant="info" data-testid="mentions-inbox-badge" class="absolute -right-1 -top-1 min-w-4 px-1 py-0 text-[10px] leading-4">{{ badge }}</Badge>
    </button>

    <HeaderPopover v-if="open" data-testid="mentions-inbox" :title="$t('mentions.inbox')">
      <StatePanel v-if="inbox.loading.value" state="loading" />
      <StatePanel v-else-if="inbox.failed.value" state="error" retryable @retry="inbox.load()">{{ $t('mentions.failed') }}</StatePanel>
      <StatePanel v-else-if="!inbox.items.value.length" state="empty" :icon="Inbox" data-testid="mentions-inbox-empty">{{ $t('mentions.empty') }}</StatePanel>
      <button
        v-for="msg in inbox.items.value"
        v-else
        :key="msg.id"
        type="button"
        data-testid="mentions-inbox-item"
        class="focus-ring group/row flex w-full items-start gap-3 rounded-lg p-2.5 text-left transition hover:bg-mnema-hover"
        @click="pick(msg)"
      >
        <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />
        <span class="min-w-0 flex-1">
          <span class="mb-0.5 flex items-center gap-2">
            <span class="min-w-8 truncate text-sm font-medium text-mnema-text">{{ shownName(msg) }}</span>
            <span v-if="channelNames.get(msg.channel_id)" class="flex min-w-0 max-w-[45%] items-center gap-1 rounded-sm border border-mnema-border bg-mnema-surface px-1.5 py-0.5 text-xs text-mnema-muted">
              <Volume2 v-if="channelNames.get(msg.channel_id)?.type === 'voice'" class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" />
              <Hash v-else class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" />
              <span class="truncate">{{ channelNames.get(msg.channel_id)?.name }}</span>
            </span>
            <CornerDownRight v-if="msg.parent_id" class="h-3 w-3 text-mnema-tertiary" :aria-label="$t('chat.inThread')" />
            <span class="ml-auto shrink-0 text-xs text-mnema-tertiary">{{ when(msg.created_at) }}</span>
            <ArrowUpRight class="-ml-1 h-3.5 w-3.5 shrink-0 text-mnema-tertiary opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-visible/row:opacity-100" aria-hidden="true" />
          </span>
          <span class="line-clamp-2 wrap-break-word text-sm text-mnema-muted">{{ previewText(msg.content) }}</span>
        </span>
      </button>
      <button
        v-if="inbox.hasMore.value && !inbox.loading.value"
        type="button"
        data-testid="mentions-inbox-more"
        :disabled="inbox.loadingMore.value"
        class="btn btn-ghost flex w-full items-center justify-center gap-2"
        @click="inbox.loadMore()"
      >
        <Loader2 v-if="inbox.loadingMore.value" class="h-3.5 w-3.5 animate-spin" />
        {{ $t('chat.searchMore') }}
      </button>
    </HeaderPopover>
  </div>
</template>
