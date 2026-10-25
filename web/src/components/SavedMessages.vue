<script setup lang="ts">
import type { ChatSavedMessage } from '../types/rest'
import { computed, nextTick, ref, watch } from 'vue'
import { Bookmark, BookmarkX, Check, Hash, Loader2, Pencil, Volume2 } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useSavedStore } from '../stores/saved'
import { useToastStore } from '../stores/toast'
import { useDismissable } from '../composables/useDismissable'
import { caughtErrorMessage } from '../lib/api'
import { clockTimeOptions } from '../lib/time'
import { previewText } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import { locale, t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

// The member's saved messages in the channel header: a button with a popover
// listing them, newest first. Access is rechecked by the server on every
// read, so a message the member can no longer see shows as unavailable (only
// their own note is left) and can be removed. A click jumps to the message.
const NOTE_MAX = 200
const chatStore = useChatStore()
const saved = useSavedStore()
const toasts = useToastStore()
// Open from the header's More menu when the header is narrow: no button of its own then.
defineProps<{ bare?: boolean }>()
const open = defineModel<boolean>('open', { default: false })
const root = ref<HTMLElement | null>(null)
const editing = ref<string | null>(null)
const draft = ref('')
const noteInput = ref<HTMLInputElement[] | HTMLInputElement | null>(null)
const channelNames = computed(() => new Map(chatStore.allChannels.map(c => [c.id, c])))

useDismissable(root, () => { open.value = false }, { active: open })

watch(open, isOpen => {
  if (isOpen) void saved.load()
  else editing.value = null
})

function pick(entry: ChatSavedMessage) {
  if (!entry.message) return
  open.value = false
  chatStore.goToMessage(entry.message)
}

async function remove(entry: ChatSavedMessage) {
  try {
    await saved.remove(entry.message_id)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('saved.removeFailed')))
  }
}

async function startNote(entry: ChatSavedMessage) {
  editing.value = entry.message_id
  draft.value = entry.note
  await nextTick()
  const input = Array.isArray(noteInput.value) ? noteInput.value[0] : noteInput.value
  input?.focus()
}

async function saveNote(entry: ChatSavedMessage) {
  const note = draft.value.trim()
  editing.value = null
  if (note === entry.note) return
  try {
    await saved.save(entry.message_id, note)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('saved.saveFailed')))
  }
}

function when(iso: string) {
  return new Date(iso).toLocaleDateString(locale.value, { month: 'short', day: 'numeric', ...clockTimeOptions(locale.value) })
}
</script>

<template>
  <div ref="root" class="relative">
    <button
      v-if="!bare"
      type="button"
      data-testid="saved-button"
      class="btn btn-icon max-md:h-11 max-md:w-11"
      :class="open ? 'bg-mnema-surface text-mnema-accent' : ''"
      :aria-label="$t('saved.title')"
      :aria-expanded="open ? 'true' : 'false'"
      aria-haspopup="dialog"
      v-tooltip="$t('saved.title')"
      @click="open = !open"
    >
      <Bookmark class="h-5 w-5" />
    </button>

    <div
      v-if="open"
      data-testid="saved-panel"
      role="dialog"
      :aria-label="$t('saved.title')"
      class="absolute right-0 top-full z-30 mt-2 flex max-h-[70vh] w-[min(24rem,calc(100vw-1.5rem))] flex-col max-md:fixed max-md:inset-x-3 max-md:top-[calc(3rem+env(safe-area-inset-top))] max-md:mt-0 max-md:max-h-[calc(100dvh-4.5rem-env(safe-area-inset-top))] max-md:w-auto overflow-hidden rounded-xl bg-mnema-elevated shadow-popover pop-in"
    >
      <div class="border-b border-mnema-hairline px-4 py-3 font-mono text-xs font-semibold uppercase tracking-widest text-mnema-tertiary">
        {{ $t('saved.title') }}
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
        <div v-if="saved.loading" class="flex justify-center py-8 text-mnema-accent">
          <Loader2 class="h-4 w-4 animate-spin" />
        </div>
        <p v-else-if="saved.failed" role="alert" class="py-8 text-center text-sm text-mnema-tertiary">{{ $t('saved.failed') }}</p>
        <p v-else-if="!saved.items.length" data-testid="saved-empty" class="px-4 py-8 text-center text-sm text-mnema-tertiary">{{ $t('saved.empty') }}</p>
        <div
          v-for="entry in saved.items"
          v-else
          :key="entry.message_id"
          data-testid="saved-item"
          class="group rounded-lg p-1 transition hover:bg-mnema-hover"
        >
          <div class="flex items-start gap-1">
            <button
              v-if="entry.available && entry.message"
              type="button"
              data-testid="saved-open"
              class="flex min-w-0 flex-1 items-start gap-3 rounded-lg p-1.5 text-left"
              @click="pick(entry)"
            >
              <UserAvatar :user="{ username: entry.message.username, display_name: entry.message.display_name, avatar_url: entry.message.avatar_url }" size="sm" class="mt-0.5" />
              <span class="min-w-0 flex-1">
                <span class="mb-0.5 flex items-center gap-2">
                  <span class="truncate text-sm font-medium text-mnema-text">{{ shownName(entry.message) }}</span>
                  <span v-if="channelNames.get(entry.message.channel_id)" class="flex items-center gap-1 rounded-sm border border-mnema-border bg-mnema-surface px-1.5 py-0.5 text-xs text-mnema-muted">
                    <Volume2 v-if="channelNames.get(entry.message.channel_id)?.type === 'voice'" class="h-2.5 w-2.5 text-mnema-tertiary" />
                    <Hash v-else class="h-2.5 w-2.5 text-mnema-tertiary" />
                    {{ channelNames.get(entry.message.channel_id)?.name }}
                  </span>
                  <span class="ml-auto shrink-0 text-xs text-mnema-tertiary">{{ when(entry.message.created_at) }}</span>
                </span>
                <span class="line-clamp-3 wrap-break-word text-sm text-mnema-muted">{{ previewText(entry.message.content) || $t('chat.attachment') }}</span>
              </span>
            </button>
            <p v-else data-testid="saved-unavailable" class="min-w-0 flex-1 p-2 text-sm italic text-mnema-tertiary">{{ $t('saved.unavailable') }}</p>
            <button
              v-if="entry.available"
              type="button"
              data-testid="saved-note-edit"
              class="btn btn-icon mt-1.5 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
              :aria-label="$t('saved.editNote')"
              v-tooltip="$t('saved.editNote')"
              @click="startNote(entry)"
            >
              <Pencil class="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              data-testid="saved-remove"
              class="btn btn-icon mr-0.5 mt-1.5 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
              :aria-label="$t('saved.remove')"
              v-tooltip="$t('saved.remove')"
              @click="remove(entry)"
            >
              <BookmarkX class="h-4 w-4" />
            </button>
          </div>
          <form v-if="editing === entry.message_id" class="flex items-center gap-1 px-2 pb-1.5" @submit.prevent="saveNote(entry)">
            <input
              ref="noteInput"
              v-model="draft"
              data-testid="saved-note-input"
              type="text"
              :maxlength="NOTE_MAX"
              :aria-label="$t('saved.notePlaceholder')"
              :placeholder="$t('saved.notePlaceholder')"
              class="min-w-0 flex-1 rounded-md border border-mnema-border bg-mnema-surface px-2 py-1 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
              @keydown.esc.stop="editing = null"
            >
            <button type="submit" data-testid="saved-note-save" class="btn btn-icon h-7 w-7 text-mnema-accent" :aria-label="$t('saved.saveNote')">
              <Check class="h-4 w-4" />
            </button>
          </form>
          <p v-else-if="entry.note" data-testid="saved-note" class="wrap-break-word px-2 pb-1.5 text-xs text-mnema-accent">{{ entry.note }}</p>
        </div>
        <button
          v-if="saved.hasMore && !saved.loading"
          type="button"
          data-testid="saved-more"
          :disabled="saved.loadingMore"
          class="btn btn-ghost flex w-full items-center justify-center gap-2"
          @click="saved.loadMore()"
        >
          <Loader2 v-if="saved.loadingMore" class="h-3.5 w-3.5 animate-spin" />
          {{ $t('chat.searchMore') }}
        </button>
      </div>
    </div>
  </div>
</template>
