<script setup lang="ts">
import { ref, computed } from 'vue'
import { charCount } from '../lib/textLength'
import { AlertCircle, Loader2, Paperclip, X, Info } from '@lucide/vue'
import type { ForumTag, Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { createForumPost, createForumPostWithFile, uploadForumPostFile } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { MAX_POST_TAGS } from '../lib/forumTags'
import { encodeShortcodes } from '../lib/customEmoji'
import { isMedia } from '../lib/composerFiles'
import { useEmojiStore } from '../stores/emoji'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { t } from '../i18n'
import { useComposerAssist } from '../composables/useComposerAssist'
import BaseDialog from './BaseDialog.vue'
import ForumTagPicker from './ForumTagPicker.vue'
import EmojiButton from './EmojiButton.vue'
import MarkdownHelp from './MarkdownHelp.vue'
import MentionSuggestions from './MentionSuggestions.vue'

const props = defineProps<{
  channelId: string
  availableTags: ForumTag[]
  settings: ForumSettings
  canModerate: boolean
  canAttach: boolean
}>()

const emit = defineEmits<{
  close: []
  created: [post: Message]
}>()

const MAX_FILES = 4
const TITLE_MAX = 100
const CONTENT_MAX = 4000

const toasts = useToastStore()
const permissions = usePermissionsStore()
const emojis = useEmojiStore()
const title = ref('')
const content = ref('')
const selectedTagIds = ref<string[]>([])
const files = ref<File[]>([])
const error = ref('')
const isSubmitting = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)
const textArea = ref<HTMLTextAreaElement | null>(null)
const assist = useComposerAssist(textArea, content)

const titleTrimmed = computed(() => title.value.trim())
const contentTrimmed = computed(() => content.value.trim())

// A forum that requires a tag is no obstacle for a member who may apply none.
const usableTags = computed(() => props.availableTags.filter(tag => props.canModerate || !tag.moderated))
const needsTag = computed(() => props.settings.require_tag && usableTags.value.length > 0)

const isValid = computed(() =>
  titleTrimmed.value.length > 0 && charCount(titleTrimmed.value) <= TITLE_MAX &&
  contentTrimmed.value.length > 0 && charCount(contentTrimmed.value) <= CONTENT_MAX &&
  selectedTagIds.value.length <= MAX_POST_TAGS &&
  (!needsTag.value || selectedTagIds.value.length > 0)
)

function addFiles(list: FileList | null) {
  const picked = Array.from(list ?? [])
  if (fileInput.value) fileInput.value.value = ''
  const media = picked.filter(isMedia)
  if (media.length < picked.length) toasts.info(t('chat.attachMediaOnly'))
  const max = permissions.maxUploadMb
  const fitting = media.filter(file => max === null || file.size <= max * 1024 * 1024)
  if (fitting.length < media.length) toasts.error(t('chat.uploadTooLarge', { max: max ?? 0 }))
  files.value = [...files.value, ...fitting].slice(0, MAX_FILES)
}

function removeFile(index: number) {
  files.value = files.value.filter((_, i) => i !== index)
}

async function handleSubmit() {
  if (!isValid.value || isSubmitting.value) return
  error.value = ''
  isSubmitting.value = true
  const body = encodeShortcodes(contentTrimmed.value, emojis.byName)
  const draft = { title: titleTrimmed.value, content: body, tagIds: selectedTagIds.value }
  try {
    const [first, ...rest] = files.value
    const post = first
      ? await createForumPostWithFile(props.channelId, draft, first)
      : await createForumPost(props.channelId, draft.title, draft.content, draft.tagIds)
    // A message carries one file: the others follow as the author's first replies.
    for (const file of rest) {
      try {
        await uploadForumPostFile(props.channelId, post.id, file)
      } catch (err) {
        toasts.error(caughtErrorMessage(err, t('chat.uploadFailed')))
      }
    }
    emit('created', post)
    emit('close')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('forum.createFailed'))
  } finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <BaseDialog :title="$t('forum.newPostTitle')" panel-class="max-w-xl" sheet @close="emit('close')">
    <form class="space-y-4 overflow-y-auto p-6 max-md:p-4" @submit.prevent="handleSubmit">
      <div v-if="error" role="alert" class="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-400">
        <AlertCircle class="h-4 w-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <div v-if="settings.guidelines" data-testid="forum-guidelines" class="flex gap-2.5 rounded-lg border border-mnema-hairline bg-mnema-surface/60 p-3">
        <Info class="mt-0.5 h-4 w-4 shrink-0 text-mnema-accent" />
        <div class="min-w-0">
          <p class="mb-0.5 text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.guidelinesTitle') }}</p>
          <p class="whitespace-pre-line break-words text-sm text-mnema-secondary">{{ settings.guidelines }}</p>
        </div>
      </div>

      <div>
        <div class="mb-1.5 flex items-center justify-between">
          <label for="post-title" class="text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.titleLabel') }}</label>
          <span class="text-xs tabular-nums text-mnema-tertiary">{{ charCount(title) }}/{{ TITLE_MAX }}</span>
        </div>
        <input
          id="post-title"
          v-model="title"
          type="text"
          v-max-chars="TITLE_MAX"
          :placeholder="$t('forum.titlePlaceholder')"
          class="w-full rounded-lg border border-mnema-border bg-mnema-surface px-3 py-2 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden"
          autofocus
        />
      </div>

      <ForumTagPicker
        v-if="availableTags.length > 0"
        v-model="selectedTagIds"
        label-id="post-tags-label"
        :tags="availableTags"
        :can-moderate="canModerate"
        :required="needsTag"
      />

      <div>
        <div class="mb-1.5 flex items-center justify-between">
          <label for="post-content" class="text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.contentLabel') }}</label>
          <span class="text-xs tabular-nums text-mnema-tertiary">{{ charCount(content) }}/{{ CONTENT_MAX }}</span>
        </div>
        <div class="relative rounded-lg border border-mnema-border bg-mnema-surface focus-within:border-mnema-accent">
          <MentionSuggestions
            v-if="assist.open.value"
            id="post-mentions"
            :items="assist.suggestions.value"
            :active="assist.active.value"
            @pick="assist.pick"
            @hover="assist.active.value = $event"
          />
          <textarea
            id="post-content"
            ref="textArea"
            v-model="content"
            data-bare
            v-max-chars="CONTENT_MAX"
            rows="6"
            :placeholder="$t('forum.contentPlaceholder')"
            class="block min-h-[120px] w-full resize-y bg-transparent p-3 text-sm text-mnema-text placeholder:text-mnema-tertiary outline-hidden"
            @keydown="assist.onKeydown"
            @input="assist.onInput"
            @click="assist.onInput"
            @blur="assist.close"
          ></textarea>
          <div class="flex items-center gap-1 border-t border-mnema-hairline px-2 py-1">
            <button
              v-if="canAttach"
              type="button"
              data-testid="post-attach"
              :disabled="files.length >= MAX_FILES || isSubmitting"
              class="flex h-8 w-8 items-center justify-center rounded-full text-mnema-tertiary transition hover:bg-mnema-hover hover:text-mnema-text disabled:opacity-50"
              :aria-label="$t('thread.upload')"
              v-tooltip="$t('thread.upload')"
              @click="fileInput?.click()"
            >
              <Paperclip class="h-4 w-4" />
            </button>
            <EmojiButton align="left" :disabled="isSubmitting" @pick="assist.insertText" />
            <MarkdownHelp :disabled="isSubmitting" />
          </div>
        </div>
        <input ref="fileInput" type="file" class="hidden" multiple accept="image/*,video/*" @change="addFiles(($event.target as HTMLInputElement).files)" />
        <ul v-if="files.length" class="mt-2 flex flex-wrap gap-1.5" data-testid="post-files">
          <li v-for="(file, i) in files" :key="`${file.name}-${i}`" class="flex max-w-full items-center gap-1 rounded-md border border-mnema-hairline bg-mnema-raised py-0.5 pl-2 pr-0.5 text-xs text-mnema-secondary">
            <span class="truncate">{{ file.name }}</span>
            <button type="button" class="flex h-6 w-6 items-center justify-center rounded-sm hover:text-mnema-text" :aria-label="$t('forum.removeFile')" @click="removeFile(i)">
              <X class="h-3.5 w-3.5" />
            </button>
          </li>
        </ul>
      </div>

      <div class="flex justify-end gap-3 pt-2">
        <button type="button" class="btn btn-secondary px-4 py-2 text-sm" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="!isValid || isSubmitting" data-testid="submit-post" class="btn btn-primary flex items-center gap-2 px-4 py-2 text-sm">
          <Loader2 v-if="isSubmitting" class="h-4 w-4 animate-spin" />
          <span>{{ $t('forum.submitPost') }}</span>
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
