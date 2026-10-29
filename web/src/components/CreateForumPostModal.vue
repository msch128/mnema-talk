<script setup lang="ts">
import { ref, computed } from 'vue'
import { AlertCircle, Loader2 } from '@lucide/vue'
import type { ForumTag, Message } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { createForumPost } from '../lib/forumApi'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps<{
  channelId: string
  availableTags: ForumTag[]
}>()

const emit = defineEmits<{
  close: []
  created: [post: Message]
}>()

const title = ref('')
const content = ref('')
const selectedTagIds = ref<string[]>([])
const error = ref('')
const isSubmitting = ref(false)

const titleTrimmed = computed(() => title.value.trim())
const contentTrimmed = computed(() => content.value.trim())
const isValid = computed(() => {
  return titleTrimmed.value.length > 0 &&
    titleTrimmed.value.length <= 100 &&
    contentTrimmed.value.length > 0 &&
    contentTrimmed.value.length <= 4000 &&
    selectedTagIds.value.length <= 5
})

function toggleTag(tagId: string) {
  const index = selectedTagIds.value.indexOf(tagId)
  if (index >= 0) {
    selectedTagIds.value.splice(index, 1)
  } else if (selectedTagIds.value.length < 5) {
    selectedTagIds.value.push(tagId)
  }
}

async function handleSubmit() {
  if (!isValid.value || isSubmitting.value) return
  error.value = ''
  isSubmitting.value = true

  try {
    const post = await createForumPost(
      props.channelId,
      titleTrimmed.value,
      contentTrimmed.value,
      selectedTagIds.value
    )
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
  <BaseDialog
    :title="$t('forum.newPostTitle')"
    panel-class="max-w-lg"
    @close="emit('close')"
  >
    <form @submit.prevent="handleSubmit" class="p-6 space-y-4">
      <div v-if="error" role="alert" class="p-3 bg-red-500/10 border border-red-500/20 rounded-lg flex items-center gap-2 text-sm text-red-400">
        <AlertCircle class="w-4 h-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <!-- Title Input -->
      <div>
        <div class="flex items-center justify-between mb-1.5">
          <label for="post-title" class="text-xs font-semibold text-mnema-secondary uppercase tracking-wider">
            {{ $t('forum.titleLabel') }}
          </label>
          <span class="text-xs text-mnema-tertiary">
            {{ title.length }}/100
          </span>
        </div>
        <input
          id="post-title"
          v-model="title"
          type="text"
          maxlength="100"
          :placeholder="$t('forum.titlePlaceholder')"
          class="w-full bg-mnema-surface border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden"
          autofocus
        />
      </div>

      <!-- Tags Selector (if available) -->
      <div v-if="availableTags.length > 0">
        <div class="flex items-center justify-between mb-1.5">
          <span id="post-tags-label" class="text-xs font-semibold text-mnema-secondary uppercase tracking-wider">
            {{ $t('forum.tagsLabel') }}
          </span>
          <span class="text-xs text-mnema-tertiary">
            {{ selectedTagIds.length }}/5
          </span>
        </div>
        <div role="group" aria-labelledby="post-tags-label" class="flex flex-wrap gap-1.5 max-h-28 overflow-y-auto p-1 bg-mnema-surface/50 border border-mnema-hairline rounded-lg">
          <button
            v-for="tag in availableTags"
            :key="tag.id"
            type="button"
            data-testid="tag-toggle-btn"
            :aria-pressed="selectedTagIds.includes(tag.id) ? 'true' : 'false'"
            :disabled="!selectedTagIds.includes(tag.id) && selectedTagIds.length >= 5"
            :class="[
              'inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-medium transition cursor-pointer',
              selectedTagIds.includes(tag.id)
                ? 'bg-mnema-accent text-white shadow-xs'
                : 'bg-mnema-surface border border-mnema-hairline text-mnema-secondary hover:text-mnema-text hover:border-mnema-border disabled:opacity-40 disabled:cursor-not-allowed'
            ]"
            @click="toggleTag(tag.id)"
          >
            <span v-if="tag.emoji">{{ tag.emoji }}</span>
            <span>{{ tag.name }}</span>
          </button>
        </div>
      </div>

      <!-- Message Content Textarea -->
      <div>
        <div class="flex items-center justify-between mb-1.5">
          <label for="post-content" class="text-xs font-semibold text-mnema-secondary uppercase tracking-wider">
            {{ $t('forum.contentLabel') }}
          </label>
          <span class="text-xs text-mnema-tertiary">
            {{ content.length }}/4000
          </span>
        </div>
        <textarea
          id="post-content"
          v-model="content"
          maxlength="4000"
          rows="5"
          :placeholder="$t('forum.contentPlaceholder')"
          class="w-full bg-mnema-surface border border-mnema-border rounded-lg p-3 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden resize-y min-h-[100px]"
        ></textarea>
      </div>

      <!-- Action Buttons -->
      <div class="flex justify-end gap-3 pt-2">
        <button
          type="button"
          class="btn btn-secondary px-4 py-2 text-sm"
          @click="emit('close')"
        >
          {{ $t('common.cancel') }}
        </button>
        <button
          type="submit"
          :disabled="!isValid || isSubmitting"
          class="btn btn-primary px-4 py-2 text-sm flex items-center gap-2"
        >
          <Loader2 v-if="isSubmitting" class="w-4 h-4 animate-spin" />
          <span>{{ $t('forum.submitPost') }}</span>
        </button>
      </div>
    </form>
  </BaseDialog>
</template>
