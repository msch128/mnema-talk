<script setup lang="ts">
import type { Message } from '../types/domain'
import { computed, ref, watch } from 'vue'
import { Pin, PinOff, Loader2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useDismissable } from '../composables/useDismissable'
import { caughtErrorMessage } from '../lib/api'
import { replySnippet, stripMarkdown } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import { locale, t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

// The channel's pinned messages: a header button with a popover listing them,
// newest pin first. A click jumps to the message; moderators can unpin here.
// The list refetches while it is open when the server reports a change.
const chatStore = useChatStore()
const permissions = usePermissionsStore()
const toasts = useToastStore()
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const items = ref<Message[]>([])
const loading = ref(false)
const failed = ref(false)
let seq = 0

const channelId = computed(() => chatStore.activeChannel?.id ?? '')
const canUnpin = computed(() => permissions.canIn(channelId.value, 'manage_messages'))

useDismissable(root, () => { open.value = false }, { active: open })

async function load() {
  const id = channelId.value
  if (!id) return
  const mine = ++seq
  loading.value = items.value.length === 0
  failed.value = false
  try {
    const list = await chatStore.fetchPins(id)
    if (mine === seq) items.value = list
  } catch (error) {
    if (mine !== seq) return
    console.warn('Failed to load pinned messages:', error)
    items.value = []
    failed.value = true
  } finally {
    if (mine === seq) loading.value = false
  }
}

watch(open, isOpen => {
  if (isOpen) void load()
})
// Another channel: close, forget the list.
watch(channelId, () => {
  open.value = false
  items.value = []
  seq++
})
watch(() => chatStore.pinsSignal, signal => {
  if (open.value && signal?.channelId === channelId.value) void load()
})

function pick(msg: Message) {
  open.value = false
  chatStore.goToMessage(msg)
}

async function unpin(msg: Message) {
  try {
    await chatStore.setPinned(msg.channel_id, msg.id, false)
    items.value = items.value.filter(m => m.id !== msg.id)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('pins.unpinFailed')))
  }
}

/** What the text alone does not show: a poll, a file, a reply inside a thread. */
function markers(msg: Message): string[] {
  const marks: string[] = []
  if (msg.poll) marks.push(t('pins.poll'))
  if (msg.attachments?.length && stripMarkdown(msg.content).trim()) marks.push(t('chat.attachment'))
  if (msg.parent_id) marks.push(t('chat.inThread'))
  return marks
}

function excerpt(msg: Message) {
  const text = replySnippet(stripMarkdown(msg.content), 160)
  return text || (msg.attachments?.length ? t('chat.attachment') : '')
}

/** "Pinned by Name · 12 Oct": who pinned it and when, as far as the server says (older servers say nothing). */
function pinnedLine(msg: Message): string {
  const name = msg.pinned_by ? msg.pinned_by.display_name || msg.pinned_by.username : ''
  const day = msg.pinned_at ? when(msg.pinned_at) : ''
  if (name && day) return t('pins.pinnedByOn', { name, date: day })
  return name ? t('pins.pinnedBy', { name }) : ''
}

function when(iso: string) {
  return new Date(iso).toLocaleDateString(locale.value, { month: 'short', day: 'numeric', year: 'numeric' })
}
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      data-testid="pinned-button"
      class="flex h-8 w-8 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('pins.title')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('pins.title')"
      @click="open = !open"
    >
      <Pin class="h-5 w-5" />
    </button>

    <div
      v-if="open"
      data-testid="pinned-panel"
      role="dialog"
      :aria-label="$t('pins.title')"
      class="absolute right-0 top-full z-30 mt-2 flex max-h-[70vh] w-[min(24rem,calc(100vw-1.5rem))] flex-col max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:max-h-[calc(100dvh-4.5rem-env(safe-area-inset-top))] max-md:w-auto overflow-hidden rounded-[10px] bg-mnema-elevated shadow-[inset_0_0_0_1px_#2B2F2D,0_12px_32px_rgba(0,0,0,0.5)]"
    >
      <div class="border-b border-mnema-hairline px-3 py-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">
        {{ $t('pins.title') }}
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
        <div v-if="loading" class="flex justify-center py-8 text-mnema-accent">
          <Loader2 class="h-4 w-4 animate-spin" />
        </div>
        <p v-else-if="failed" role="alert" class="py-8 text-center text-sm text-mnema-tertiary">{{ $t('pins.failed') }}</p>
        <p v-else-if="!items.length" data-testid="pinned-empty" class="px-4 py-8 text-center text-sm text-mnema-tertiary">
          {{ $t(canUnpin ? 'pins.emptyModerator' : 'pins.empty') }}
        </p>
        <div v-for="msg in items" v-else :key="msg.id" class="group flex items-start gap-1 rounded-lg transition hover:bg-mnema-hover">
          <button
            type="button"
            data-testid="pinned-item"
            class="flex min-w-0 flex-1 items-start gap-3 rounded-lg p-2.5 text-left"
            @click="pick(msg)"
          >
            <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />
            <span class="min-w-0 flex-1">
              <span class="mb-0.5 flex items-center gap-2">
                <span class="truncate text-sm font-medium text-mnema-text">{{ shownName(msg) }}</span>
                <span class="ml-auto shrink-0 text-xs text-mnema-tertiary">{{ when(msg.created_at) }}</span>
              </span>
              <span class="line-clamp-3 wrap-break-word text-sm text-mnema-muted">{{ excerpt(msg) }}</span>
              <span v-if="pinnedLine(msg)" data-testid="pinned-by" class="mt-1 block truncate text-xs text-mnema-tertiary">{{ pinnedLine(msg) }}</span>
              <span v-if="markers(msg).length" data-testid="pinned-markers" class="mt-1 block truncate text-xs text-mnema-tertiary">{{ markers(msg).join(' · ') }}</span>
            </span>
          </button>
          <button
            v-if="canUnpin"
            type="button"
            data-testid="pinned-unpin"
            class="mr-1.5 mt-2 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text max-md:h-11 max-md:w-11"
            :aria-label="$t('moderation.unpinMessage')"
            v-tooltip="$t('moderation.unpinMessage')"
            @click="unpin(msg)"
          >
            <PinOff class="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
