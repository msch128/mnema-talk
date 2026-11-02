<script setup lang="ts">
import { computed, onMounted, ref, toRef } from 'vue'
import { Bell, BellOff, Pencil, SendHorizontal, Trash2 } from '@lucide/vue'
import type { EventComment } from '../lib/eventCommentsApi'
import { useEventComments } from '../composables/useEventComments'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { confirm } from '../lib/confirm'
import { charCount } from '../lib/textLength'
import { eventAvatar } from '../lib/eventAvatar'
import { relative } from '../lib/time'
import { locale, t } from '../i18n'
import MarkdownContent from './MarkdownContent.vue'
import UserAvatar from './UserAvatar.vue'

// The discussion under an event: comments oldest first (older pages on
// request), a composer, edit and delete where the member may, and a bell that
// switches the notices of this event's comments off.
const props = defineProps<{ eventId: string }>()

const MAX = 2000
const auth = useAuthStore()
const permissions = usePermissionsStore()
const list = useEventComments(toRef(props, 'eventId'))
const draft = ref('')
const editingId = ref<string | null>(null)
const editText = ref('')

onMounted(() => { void list.load() })

const canSend = computed(() => draft.value.trim().length > 0 && charCount(draft.value) <= MAX && !list.busy.value)
const isModerator = computed(() => permissions.can('manage_events') || permissions.can('manage_messages'))
const isOwn = (c: EventComment) => !!auth.user && c.user.id === auth.user.id
const canDelete = (c: EventComment) => isOwn(c) || isModerator.value
const name = (c: EventComment) => c.user.display_name || c.user.username

async function send() {
  if (!canSend.value) return
  if (await list.send(draft.value)) draft.value = ''
}

function onComposerKey(e: KeyboardEvent) {
  if (e.key !== 'Enter' || e.shiftKey || e.isComposing) return
  e.preventDefault()
  void send()
}

function startEdit(c: EventComment) {
  editingId.value = c.id
  editText.value = c.content
}

async function saveEdit(c: EventComment) {
  if (!editText.value.trim() || charCount(editText.value) > MAX) return
  if (await list.edit(c.id, editText.value)) editingId.value = null
}

async function removeComment(c: EventComment) {
  const ok = await confirm({
    title: t('events.commentDeleteTitle'),
    body: t('events.commentDeleteBody'),
    excerpt: c.content.slice(0, 160),
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (ok) await list.remove(c.id)
}
</script>

<template>
  <section class="mb-6" data-testid="event-comments" :aria-label="$t('events.comments')">
    <div class="mb-2 flex items-center justify-between gap-2">
      <h3 class="text-sm font-medium text-mnema-text">{{ $t('events.comments') }}</h3>
      <button
        type="button"
        data-testid="comments-mute"
        class="flex h-8 w-8 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text max-md:h-11 max-md:w-11"
        :aria-pressed="list.muted.value ? 'true' : 'false'"
        :aria-label="list.muted.value ? $t('events.commentsUnmute') : $t('events.commentsMute')"
        v-tooltip="list.muted.value ? $t('events.commentsUnmute') : $t('events.commentsMute')"
        :disabled="list.busy.value"
        @click="list.toggleMute"
      >
        <BellOff v-if="list.muted.value" class="h-4 w-4" />
        <Bell v-else class="h-4 w-4" />
      </button>
    </div>

    <p v-if="list.loading.value" class="py-3 text-center text-sm text-mnema-tertiary">{{ $t('common.loading') }}</p>
    <p v-else-if="list.failed.value" role="alert" class="text-sm text-red-400">{{ $t('events.commentsLoadFailed') }}</p>
    <template v-else>
      <button
        v-if="list.hasMore.value"
        type="button"
        data-testid="comments-older"
        class="btn btn-secondary mb-2 px-3 py-1.5 text-xs max-md:min-h-11"
        :disabled="list.loadingMore.value"
        @click="list.loadOlder"
      >
        {{ $t('events.commentsOlder') }}
      </button>
      <p v-if="list.comments.value.length === 0" class="mb-2 text-sm italic text-mnema-tertiary">{{ $t('events.noComments') }}</p>
      <ul v-else class="mb-3 flex max-h-72 flex-col divide-y divide-mnema-hairline overflow-y-auto rounded-lg border border-mnema-border bg-mnema-surface">
        <li v-for="c in list.comments.value" :key="c.id" data-testid="event-comment" class="group flex gap-2.5 p-3">
          <UserAvatar :user="eventAvatar(c.user)" size="sm" class="mt-0.5 shrink-0" />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-baseline gap-x-2">
              <span class="truncate text-sm font-semibold text-mnema-text">{{ name(c) }}</span>
              <span class="text-xs text-mnema-tertiary">{{ relative(c.created_at, locale) }}</span>
              <span v-if="c.edited_at" class="text-xs italic text-mnema-tertiary">{{ $t('chat.edited') }}</span>
              <span class="ml-auto flex shrink-0 items-center md:opacity-0 md:transition md:group-focus-within:opacity-100 md:group-hover:opacity-100">
                <button v-if="isOwn(c) && editingId !== c.id" type="button" data-testid="comment-edit" class="btn btn-icon h-7 w-7 max-md:h-11 max-md:w-11" :aria-label="$t('events.commentEdit')" @click="startEdit(c)">
                  <Pencil class="h-3.5 w-3.5" />
                </button>
                <button v-if="canDelete(c)" type="button" data-testid="comment-delete" class="btn btn-icon h-7 w-7 hover:text-red-400 max-md:h-11 max-md:w-11" :aria-label="$t('common.delete')" @click="removeComment(c)">
                  <Trash2 class="h-3.5 w-3.5" />
                </button>
              </span>
            </div>
            <form v-if="editingId === c.id" class="mt-1 flex flex-col gap-2" @submit.prevent="saveEdit(c)" @keydown.esc.stop="editingId = null">
              <textarea v-model="editText" v-max-chars="MAX" rows="2" data-testid="comment-edit-input" :aria-label="$t('events.commentEdit')" class="w-full resize-y rounded-lg border border-mnema-border bg-mnema-canvas p-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"></textarea>
              <div class="flex justify-end gap-2">
                <button type="button" class="btn btn-secondary px-3 py-1.5 text-xs max-md:min-h-11" @click="editingId = null">{{ $t('common.cancel') }}</button>
                <button type="submit" data-testid="comment-edit-save" class="btn btn-primary px-3 py-1.5 text-xs max-md:min-h-11" :disabled="list.busy.value || !editText.trim()">{{ $t('common.save') }}</button>
              </div>
            </form>
            <MarkdownContent v-else :content="c.content" class="text-sm text-mnema-text" />
          </div>
        </li>
      </ul>
    </template>

    <form class="flex items-end gap-2" @submit.prevent="send">
      <div class="min-w-0 flex-1">
        <textarea
          v-model="draft"
          v-max-chars="MAX"
          rows="2"
          data-testid="comment-input"
          :aria-label="$t('events.commentPlaceholder')"
          :placeholder="$t('events.commentPlaceholder')"
          class="w-full resize-y rounded-lg border border-mnema-border bg-mnema-surface p-2.5 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden focus:border-mnema-accent"
          @keydown="onComposerKey"
        ></textarea>
        <p v-if="draft.length > MAX * 0.8" class="mt-0.5 text-right text-xs tabular-nums text-mnema-tertiary">{{ charCount(draft) }}/{{ MAX }}</p>
      </div>
      <button
        type="submit"
        data-testid="comment-send"
        class="btn btn-primary flex h-10 w-10 shrink-0 items-center justify-center p-0 max-md:h-11 max-md:w-11"
        :disabled="!canSend"
        :aria-label="$t('chat.send')"
        v-tooltip="$t('chat.send')"
      >
        <SendHorizontal class="h-4 w-4" />
      </button>
    </form>
  </section>
</template>
