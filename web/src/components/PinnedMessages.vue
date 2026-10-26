<script setup lang="ts">
import type { Message } from '../types/domain'
import { computed, ref, watch } from 'vue'
import { ArrowUpRight, Pin, PinOff } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useDismissable } from '../composables/useDismissable'
import { caughtErrorMessage } from '../lib/api'
import { replySnippet, stripMarkdown } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import { dateTime, dayLabel } from '../lib/time'
import { locale, t } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import HeaderPopover from './HeaderPopover.vue'
import StatePanel from './StatePanel.vue'

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
  const day = msg.pinned_at ? dayLabel(msg.pinned_at, locale.value) : ''
  if (name && day) return t('pins.pinnedByOn', { name, date: day })
  return name ? t('pins.pinnedBy', { name }) : ''
}

const when = (iso: string) => dateTime(iso, locale.value)
</script>

<template>
  <div ref="root" class="relative">
    <button
      type="button"
      data-testid="pinned-button"
      class="btn btn-icon max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('pins.title')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('pins.title')"
      @click="open = !open"
    >
      <Pin class="h-5 w-5" />
    </button>

    <HeaderPopover v-if="open" data-testid="pinned-panel" :title="$t('pins.title')">
      <StatePanel v-if="loading" state="loading" />
      <StatePanel v-else-if="failed" state="error" retryable @retry="load">{{ $t('pins.failed') }}</StatePanel>
      <StatePanel v-else-if="!items.length" state="empty" :icon="Pin" data-testid="pinned-empty">
        {{ $t(canUnpin ? 'pins.emptyModerator' : 'pins.empty') }}
      </StatePanel>
      <div v-for="msg in items" v-else :key="msg.id" class="group/row flex items-start gap-1 rounded-lg transition hover:bg-mnema-hover">
        <button
          type="button"
          data-testid="pinned-item"
          class="focus-ring flex min-w-0 flex-1 items-start gap-3 rounded-lg p-2.5 text-left"
          @click="pick(msg)"
        >
          <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />
          <span class="min-w-0 flex-1">
            <span class="mb-0.5 flex items-center gap-2">
              <span class="truncate text-sm font-medium text-mnema-text">{{ shownName(msg) }}</span>
              <span class="ml-auto shrink-0 text-xs text-mnema-tertiary">{{ when(msg.created_at) }}</span>
              <ArrowUpRight class="-ml-1 h-3.5 w-3.5 shrink-0 text-mnema-tertiary opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100" aria-hidden="true" />
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
          class="btn btn-icon mr-1.5 mt-2 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
          :aria-label="$t('moderation.unpinMessage')"
          v-tooltip="$t('moderation.unpinMessage')"
          @click="unpin(msg)"
        >
          <PinOff class="h-4 w-4" />
        </button>
      </div>
    </HeaderPopover>
  </div>
</template>
