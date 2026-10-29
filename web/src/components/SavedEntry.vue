<script setup lang="ts">
import type { PropType } from 'vue'
import type { ChatSavedMessage } from '../types/rest'
import type { SavedContext } from '../lib/savedGroups'
import { nextTick, ref } from 'vue'
import { ArrowUpRight, BookmarkX, Check, EyeOff, Hash, Lock, MessageCircle, Pencil, Volume2 } from '@lucide/vue'
import { useSavedStore } from '../stores/saved'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { dateTime } from '../lib/time'
import { previewText } from '../lib/replies'
import { shownName } from '../lib/deletedUser'
import { locale, t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

// One saved message: who wrote it, where, when and what it says, a private
// note of the member's own, and the ways to open or remove it. A message the
// member can no longer see is shown calmly as unavailable (the server rechecks
// access on every read); its note stays.
const NOTE_MAX = 200
const props = defineProps({
  entry: { type: Object as PropType<ChatSavedMessage>, required: true },
  // Where it was written; omitted under a heading that says so already.
  context: { type: Object as PropType<SavedContext | null>, default: null },
  flash: { type: Boolean, default: false }
})
const emit = defineEmits<{ open: [] }>()

const saved = useSavedStore()
const toasts = useToastStore()
const editing = ref(false)
const draft = ref('')
const noteInput = ref<HTMLInputElement | null>(null)

async function remove() {
  try {
    await saved.remove(props.entry.message_id)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('saved.removeFailed')))
  }
}

async function startNote() {
  editing.value = true
  draft.value = props.entry.note
  await nextTick()
  noteInput.value?.focus()
}

async function saveNote() {
  const note = draft.value.trim()
  editing.value = false
  if (note === props.entry.note) return
  try {
    await saved.save(props.entry.message_id, note)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('saved.saveFailed')))
  }
}

const when = (iso: string) => dateTime(iso, locale.value)
</script>

<template>
  <div
    data-testid="saved-item"
    :data-message-id="entry.message_id"
    :class="['group/row rounded-lg p-1 transition hover:bg-mnema-hover', flash ? 'msg-flash' : '']"
  >
    <div class="flex items-start gap-1">
      <button
        v-if="entry.available && entry.message"
        type="button"
        data-testid="saved-open"
        class="focus-ring flex min-w-0 flex-1 items-start gap-3 rounded-lg p-1.5 text-left"
        :aria-label="$t('saved.openMessage')"
        @click="emit('open')"
      >
        <UserAvatar :user="{ username: entry.message.username, display_name: entry.message.display_name, avatar_url: entry.message.avatar_url }" size="sm" class="mt-0.5" />
        <span class="min-w-0 flex-1">
          <span class="mb-0.5 flex items-center gap-2">
            <span class="min-w-8 truncate text-sm font-medium text-mnema-text">{{ shownName(entry.message) }}</span>
            <span v-if="context" data-testid="saved-context" class="flex min-w-0 max-w-[45%] items-center gap-1 rounded-sm border border-mnema-border bg-mnema-surface px-1.5 py-0.5 text-xs text-mnema-muted">
              <MessageCircle v-if="context.dm" class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" aria-hidden="true" />
              <Volume2 v-else-if="context.voice" class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" aria-hidden="true" />
              <Hash v-else class="h-2.5 w-2.5 shrink-0 text-mnema-tertiary" aria-hidden="true" />
              <span class="truncate">{{ context.name }}</span>
            </span>
            <span class="ml-auto shrink-0 text-xs text-mnema-tertiary" :title="$t('saved.savedAt', { time: when(entry.saved_at) })">{{ when(entry.message.created_at) }}</span>
            <ArrowUpRight class="-ml-1 h-3.5 w-3.5 shrink-0 text-mnema-tertiary opacity-0 transition-opacity group-hover/row:opacity-100 group-focus-within/row:opacity-100" aria-hidden="true" />
          </span>
          <span class="line-clamp-3 wrap-break-word text-sm text-mnema-muted">{{ previewText(entry.message.content) || $t('chat.attachment') }}</span>
        </span>
      </button>
      <div v-else data-testid="saved-unavailable" class="flex min-w-0 flex-1 items-start gap-2 p-2 text-sm text-mnema-tertiary">
        <EyeOff class="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
        <p class="min-w-0">
          <span class="block">{{ $t('saved.unavailable') }}</span>
          <span class="block text-xs">{{ $t('saved.unavailableHint') }}</span>
        </p>
      </div>
      <button
        v-if="entry.available"
        type="button"
        data-testid="saved-note-edit"
        class="btn btn-icon mt-1.5 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
        :aria-label="entry.note ? $t('saved.editNote') : $t('saved.addNote')"
        v-tooltip="entry.note ? $t('saved.editNote') : $t('saved.addNote')"
        @click="startNote"
      >
        <Pencil class="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        data-testid="saved-remove"
        class="btn btn-icon mr-0.5 mt-1.5 h-7 w-7 shrink-0 max-md:h-11 max-md:w-11"
        :aria-label="$t('saved.remove')"
        v-tooltip="$t('saved.remove')"
        @click="remove"
      >
        <BookmarkX class="h-4 w-4" />
      </button>
    </div>
    <form v-if="editing" class="px-2 pb-1.5" @submit.prevent="saveNote">
      <div class="flex items-center gap-1">
        <input
          ref="noteInput"
          v-model="draft"
          data-testid="saved-note-input"
          type="text"
          :maxlength="NOTE_MAX"
          :aria-label="$t('saved.notePlaceholder')"
          :placeholder="$t('saved.notePlaceholder')"
          class="min-w-0 flex-1 rounded-md border border-mnema-border bg-mnema-surface px-2 py-1 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
          @keydown.esc.stop="editing = false"
        >
        <button type="submit" data-testid="saved-note-save" class="btn btn-icon h-7 w-7 text-mnema-accent" :aria-label="$t('saved.saveNote')">
          <Check class="h-4 w-4" />
        </button>
      </div>
      <p class="mt-1 text-xs text-mnema-tertiary">{{ $t('saved.noteHint') }}</p>
    </form>
    <p v-else-if="entry.note" data-testid="saved-note" class="flex items-start gap-1.5 wrap-break-word px-2 pb-1.5 text-xs text-mnema-accent" :title="$t('saved.noteHint')">
      <Lock class="mt-0.5 h-3 w-3 shrink-0" aria-hidden="true" />
      <span class="min-w-0"><span class="text-mnema-tertiary">{{ $t('saved.noteLabel') }}</span> · {{ entry.note }}</span>
    </p>
  </div>
</template>
