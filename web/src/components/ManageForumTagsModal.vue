<script setup lang="ts">
import { ref, computed } from 'vue'
import { AlertCircle, Loader2, Plus, Trash2, Edit2, Check, X } from '@lucide/vue'
import type { ForumTag } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { createForumTag, updateForumTag, deleteForumTag } from '../lib/forumApi'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps<{
  channelId: string
  tags: ForumTag[]
}>()

const emit = defineEmits<{
  close: []
  updated: []
}>()

const newName = ref('')
const newEmoji = ref('')
const isAdding = ref(false)
const error = ref('')

const editingId = ref<string | null>(null)
const editName = ref('')
const editEmoji = ref('')
const isSaving = ref(false)
const deletingId = ref<string | null>(null)

const canAdd = computed(() => {
  return props.tags.length < 20 &&
    newName.value.trim().length > 0 &&
    newName.value.trim().length <= 20
})

function startEdit(tag: ForumTag) {
  editingId.value = tag.id
  editName.value = tag.name
  editEmoji.value = tag.emoji || ''
  error.value = ''
}

function cancelEdit() {
  editingId.value = null
  editName.value = ''
  editEmoji.value = ''
}

async function handleAddTag() {
  if (!canAdd.value || isAdding.value) return
  error.value = ''
  isAdding.value = true

  try {
    await createForumTag(props.channelId, newName.value.trim(), newEmoji.value.trim() || undefined)
    newName.value = ''
    newEmoji.value = ''
    emit('updated')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('forum.addTagFailed'))
  } finally {
    isAdding.value = false
  }
}

async function handleSaveEdit(tagId: string) {
  if (editName.value.trim().length === 0 || editName.value.trim().length > 20 || isSaving.value) return
  error.value = ''
  isSaving.value = true

  try {
    await updateForumTag(props.channelId, tagId, editName.value.trim(), editEmoji.value.trim())
    cancelEdit()
    emit('updated')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('forum.updateTagFailed'))
  } finally {
    isSaving.value = false
  }
}

async function handleDeleteTag(tagId: string) {
  if (deletingId.value) return
  error.value = ''
  deletingId.value = tagId

  try {
    await deleteForumTag(props.channelId, tagId)
    emit('updated')
  } catch (err) {
    error.value = caughtErrorMessage(err, t('forum.deleteTagFailed'))
  } finally {
    deletingId.value = null
  }
}
</script>

<template>
  <BaseDialog
    :title="$t('forum.manageTagsTitle')"
    panel-class="max-w-md"
    @close="emit('close')"
  >
    <div class="p-6 space-y-4">
      <div v-if="error" class="p-3 bg-red-500/10 border border-red-500/20 rounded-lg flex items-center gap-2 text-sm text-red-400">
        <AlertCircle class="w-4 h-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <!-- Add New Tag Form (if under 20 limit) -->
      <div v-if="tags.length < 20" class="p-3 bg-mnema-surface/60 border border-mnema-hairline rounded-lg space-y-3">
        <div class="flex items-center justify-between">
          <span class="text-xs font-semibold text-mnema-secondary uppercase tracking-wider">
            {{ $t('forum.addTag') }}
          </span>
          <span class="text-xs text-mnema-tertiary">
            {{ tags.length }}/20
          </span>
        </div>
        <div class="flex items-center gap-2">
          <input
            v-model="newEmoji"
            type="text"
            maxlength="32"
            :placeholder="$t('forum.emojiPlaceholder')"
            class="w-16 bg-mnema-surface border border-mnema-border rounded-lg px-2 py-1.5 text-sm text-center text-mnema-text focus:outline-hidden focus:border-mnema-accent"
          />
          <input
            v-model="newName"
            type="text"
            maxlength="20"
            :placeholder="$t('forum.tagNamePlaceholder')"
            class="flex-1 min-w-0 bg-mnema-surface border border-mnema-border rounded-lg px-3 py-1.5 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent"
            @keydown.enter.prevent="handleAddTag"
          />
          <button
            type="button"
            :disabled="!canAdd || isAdding"
            class="btn btn-primary px-3 py-1.5 text-xs flex items-center gap-1 shrink-0"
            @click="handleAddTag"
          >
            <Loader2 v-if="isAdding" class="w-3.5 h-3.5 animate-spin" />
            <Plus v-else class="w-3.5 h-3.5" />
            <span>{{ $t('forum.add') }}</span>
          </button>
        </div>
      </div>
      <div v-else class="text-xs text-mnema-tertiary text-center">
        {{ $t('forum.maxTagsReached') }} (20/20)
      </div>

      <!-- Existing Tags List -->
      <div class="space-y-1.5 max-h-64 overflow-y-auto pr-1">
        <div
          v-if="tags.length === 0"
          class="text-xs text-mnema-tertiary text-center py-6 italic"
        >
          {{ $t('forum.noTags') }}
        </div>

        <div
          v-for="tag in tags"
          :key="tag.id"
          class="flex items-center justify-between gap-2 p-2 bg-mnema-surface/40 hover:bg-mnema-surface rounded-lg border border-mnema-hairline/80 transition"
        >
          <!-- Editing Mode -->
          <template v-if="editingId === tag.id">
            <input
              v-model="editEmoji"
              type="text"
              maxlength="32"
              class="w-12 bg-mnema-surface border border-mnema-border rounded-md px-1.5 py-1 text-xs text-center text-mnema-text focus:outline-hidden focus:border-mnema-accent"
            />
            <input
              v-model="editName"
              type="text"
              maxlength="20"
              class="flex-1 min-w-0 bg-mnema-surface border border-mnema-border rounded-md px-2 py-1 text-xs text-mnema-text focus:outline-hidden focus:border-mnema-accent"
              @keydown.enter.prevent="handleSaveEdit(tag.id)"
              @keydown.esc.prevent="cancelEdit"
            />
            <div class="flex items-center gap-1 shrink-0">
              <button
                type="button"
                :disabled="isSaving"
                class="p-1 hover:bg-mnema-hover rounded-md text-emerald-400"
                @click="handleSaveEdit(tag.id)"
              >
                <Check class="w-4 h-4" />
              </button>
              <button
                type="button"
                class="p-1 hover:bg-mnema-hover rounded-md text-mnema-tertiary hover:text-mnema-text"
                @click="cancelEdit"
              >
                <X class="w-4 h-4" />
              </button>
            </div>
          </template>

          <!-- Normal Display Mode -->
          <template v-else>
            <div class="flex items-center gap-2 min-w-0">
              <span v-if="tag.emoji" class="text-sm shrink-0">{{ tag.emoji }}</span>
              <span class="text-sm font-medium text-mnema-text truncate">{{ tag.name }}</span>
            </div>
            <div class="flex items-center gap-1 shrink-0">
              <button
                type="button"
                class="p-1.5 hover:bg-mnema-hover rounded-md text-mnema-tertiary hover:text-mnema-text transition"
                :title="$t('forum.edit')"
                @click="startEdit(tag)"
              >
                <Edit2 class="w-3.5 h-3.5" />
              </button>
              <button
                type="button"
                :disabled="deletingId === tag.id"
                class="p-1.5 hover:bg-mnema-hover rounded-md text-mnema-tertiary hover:text-red-400 transition"
                :title="$t('common.delete')"
                @click="handleDeleteTag(tag.id)"
              >
                <Loader2 v-if="deletingId === tag.id" class="w-3.5 h-3.5 animate-spin" />
                <Trash2 v-else class="w-3.5 h-3.5" />
              </button>
            </div>
          </template>
        </div>
      </div>

      <!-- Close Button -->
      <div class="flex justify-end pt-2">
        <button
          type="button"
          class="btn btn-secondary px-4 py-2 text-sm"
          @click="emit('close')"
        >
          {{ $t('common.close') }}
        </button>
      </div>
    </div>
  </BaseDialog>
</template>
