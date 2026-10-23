<script setup lang="ts">
import { clockTimeOptions } from '../lib/time'
import type { Message } from '../types/domain'
import { computed, ref, watch } from 'vue'
import { Inbox, Hash, Volume2, CornerDownRight, Loader2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useMentionsInbox } from '../composables/useMentionsInbox'
import { useDismissable } from '../composables/useDismissable'
import { locale } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import { shownName } from '../lib/deletedUser'
import { previewText } from '../lib/replies'

// The mentions inbox in the channel header: a button with the number of
// unread mentions and a popover listing them; a click jumps to the message.
const chatStore = useChatStore()
const inbox = useMentionsInbox()
const open = ref(false)
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

function when(iso: string) {
  const d = new Date(iso)
  return d.toLocaleDateString(locale.value, { month: 'short', day: 'numeric', ...clockTimeOptions(locale.value) })
}
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      data-testid="mentions-inbox-button"
      class="relative flex h-8 w-8 max-md:h-11 max-md:w-11 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('mentions.inbox')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('mentions.inbox')"
      @click="open = !open"
    >
      <Inbox class="h-5 w-5" />
      <span
        v-if="unread > 0"
        data-testid="mentions-inbox-badge"
        class="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-mnema-accent px-1 text-center font-mono text-[10px] font-semibold leading-4 text-mnema-accent-ink"
      >{{ badge }}</span>
    </button>

    <div
      v-if="open"
      data-testid="mentions-inbox"
      role="dialog"
      :aria-label="$t('mentions.inbox')"
      class="absolute right-0 top-full z-30 mt-2 flex max-h-[70vh] w-[min(24rem,calc(100vw-1.5rem))] flex-col max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:max-h-[calc(100dvh-4.5rem-env(safe-area-inset-top))] max-md:w-auto overflow-hidden rounded-[10px] bg-mnema-elevated shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
    >
      <div class="border-b border-mnema-hairline px-3 py-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">
        {{ $t('mentions.inbox') }}
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
        <div v-if="inbox.loading.value" class="flex justify-center py-8 text-mnema-accent">
          <Loader2 class="h-4 w-4 animate-spin" />
        </div>
        <p v-else-if="inbox.failed.value" role="alert" class="py-8 text-center text-sm text-mnema-tertiary">
          {{ $t('mentions.failed') }}
        </p>
        <p v-else-if="!inbox.items.value.length" data-testid="mentions-inbox-empty" class="py-8 text-center text-sm text-mnema-tertiary">
          {{ $t('mentions.empty') }}
        </p>
        <button
          v-for="msg in inbox.items.value"
          v-else
          :key="msg.id"
          type="button"
          data-testid="mentions-inbox-item"
          class="flex w-full items-start gap-3 rounded-lg p-2.5 text-left transition hover:bg-mnema-hover"
          @click="pick(msg)"
        >
          <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />
          <span class="min-w-0 flex-1">
            <span class="mb-0.5 flex items-center gap-2">
              <span class="truncate text-sm font-medium text-mnema-text">{{ shownName(msg) }}</span>
              <span v-if="channelNames.get(msg.channel_id)" class="flex items-center gap-1 rounded-sm border border-mnema-border bg-mnema-surface px-1.5 py-0.5 text-xs text-mnema-muted">
                <Volume2 v-if="channelNames.get(msg.channel_id)?.type === 'voice'" class="h-2.5 w-2.5 text-mnema-tertiary" />
                <Hash v-else class="h-2.5 w-2.5 text-mnema-tertiary" />
                {{ channelNames.get(msg.channel_id)?.name }}
              </span>
              <CornerDownRight v-if="msg.parent_id" class="h-3 w-3 text-mnema-tertiary" :aria-label="$t('chat.inThread')" />
              <span class="ml-auto shrink-0 text-xs text-mnema-tertiary">{{ when(msg.created_at) }}</span>
            </span>
            <span class="line-clamp-2 wrap-break-word text-sm text-mnema-muted">{{ previewText(msg.content) }}</span>
          </span>
        </button>
        <button
          v-if="inbox.hasMore.value && !inbox.loading.value"
          type="button"
          data-testid="mentions-inbox-more"
          :disabled="inbox.loadingMore.value"
          class="flex w-full items-center justify-center gap-2 rounded-lg py-2 text-sm text-mnema-muted transition hover:bg-mnema-hover hover:text-mnema-text disabled:opacity-50"
          @click="inbox.loadMore()"
        >
          <Loader2 v-if="inbox.loadingMore.value" class="h-3.5 w-3.5 animate-spin" />
          {{ $t('chat.searchMore') }}
        </button>
      </div>
    </div>
  </div>
</template>
