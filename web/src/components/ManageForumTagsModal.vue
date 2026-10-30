<script setup lang="ts">
import { ref } from 'vue'
import { AlertCircle, Loader2, Plus, Trash2, Pencil, ArrowUp, ArrowDown, Lock } from '@lucide/vue'
import type { ForumTag } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { createForumTag, updateForumTag, deleteForumTag, reorderForumTags } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { MAX_FORUM_TAGS } from '../lib/forumTags'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import ForumTagChip from './ForumTagChip.vue'
import ForumTagEditor from './ForumTagEditor.vue'
import ForumSettingsForm from './ForumSettingsForm.vue'

// Where moderators define the tags of a forum (name, emoji, colour, moderated,
// order) and its rules (a tag is required, the guidelines).
const props = defineProps<{
  channelId: string
  tags: ForumTag[]
  settings: ForumSettings
}>()

const emit = defineEmits<{
  close: []
  updated: []
  'settings-saved': [settings: ForumSettings]
}>()

type Fields = { name: string; emoji: string; color: string; moderated: boolean }

const error = ref('')
const adding = ref(false)
const editingId = ref<string | null>(null)
const busyId = ref<string | null>(null)
const isSaving = ref(false)

// Escape while a tag is being edited leaves edit mode; the dialog stays open.
function onDialogClose() {
  if (adding.value || editingId.value) {
    adding.value = false
    editingId.value = null
  } else emit('close')
}

async function run(failure: string, work: () => Promise<void>) {
  error.value = ''
  try {
    await work()
    emit('updated')
  } catch (err) {
    error.value = caughtErrorMessage(err, failure)
  }
}

async function addTag(fields: Fields) {
  isSaving.value = true
  await run(t('forum.addTagFailed'), async () => {
    await createForumTag(props.channelId, fields.name, fields.emoji || undefined, { color: fields.color, moderated: fields.moderated })
    adding.value = false
  })
  isSaving.value = false
}

async function saveTag(tag: ForumTag, fields: Fields) {
  isSaving.value = true
  await run(t('forum.updateTagFailed'), async () => {
    await updateForumTag(props.channelId, tag.id, fields.name, fields.emoji, { color: fields.color, moderated: fields.moderated })
    editingId.value = null
  })
  isSaving.value = false
}

async function removeTag(tag: ForumTag) {
  const ok = await confirm({
    title: t('forum.deleteTagTitle'),
    body: t('forum.deleteTagBody'),
    excerpt: tag.name,
    confirmLabel: t('common.delete'),
    danger: true
  })
  if (!ok) return
  busyId.value = tag.id
  await run(t('forum.deleteTagFailed'), async () => { await deleteForumTag(props.channelId, tag.id) })
  busyId.value = null
}

async function move(index: number, by: -1 | 1) {
  const ids = props.tags.map(tag => tag.id)
  const other = index + by
  if (other < 0 || other >= ids.length) return
  ;[ids[index], ids[other]] = [ids[other]!, ids[index]!]
  await run(t('forum.reorderFailed'), async () => { await reorderForumTags(props.channelId, ids) })
}
</script>

<template>
  <BaseDialog :title="$t('forum.manageTagsTitle')" panel-class="max-w-lg" sheet @close="onDialogClose">
    <div class="space-y-6 overflow-y-auto p-6">
      <div v-if="error" role="alert" class="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-400">
        <AlertCircle class="h-4 w-4 shrink-0" />
        <span>{{ error }}</span>
      </div>

      <section class="space-y-3">
        <div class="flex items-center justify-between">
          <h3 class="text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.tagsTitle') }}</h3>
          <span class="text-xs tabular-nums text-mnema-tertiary">{{ tags.length }}/{{ MAX_FORUM_TAGS }}</span>
        </div>
        <p class="text-xs leading-relaxed text-mnema-tertiary">{{ $t('forum.tagsIntro') }}</p>

        <p v-if="tags.length === 0 && !adding" class="py-4 text-center text-xs italic text-mnema-tertiary">{{ $t('forum.noTags') }}</p>

        <ul class="space-y-1.5" data-testid="forum-tag-list">
          <li v-for="(tag, index) in tags" :key="tag.id">
            <ForumTagEditor v-if="editingId === tag.id" :tag="tag" :saving="isSaving" @save="saveTag(tag, $event)" @cancel="editingId = null" />
            <div v-else class="flex items-center justify-between gap-2 rounded-lg border border-mnema-hairline/80 bg-mnema-surface/40 p-2 transition hover:bg-mnema-surface" data-testid="forum-tag-row">
              <div class="flex min-w-0 items-center gap-2">
                <ForumTagChip :tag="tag" />
                <Lock v-if="tag.moderated" class="h-3.5 w-3.5 shrink-0 text-mnema-tertiary" :aria-label="$t('forum.moderatedTag')" v-tooltip="$t('forum.moderatedTag')" />
              </div>
              <div class="flex shrink-0 items-center">
                <button type="button" class="btn btn-icon h-8 w-8 max-md:h-10 max-md:w-10" :disabled="index === 0" :aria-label="$t('forum.moveUp')" v-tooltip="$t('forum.moveUp')" @click="move(index, -1)">
                  <ArrowUp class="h-3.5 w-3.5" />
                </button>
                <button type="button" class="btn btn-icon h-8 w-8 max-md:h-10 max-md:w-10" :disabled="index === tags.length - 1" :aria-label="$t('forum.moveDown')" v-tooltip="$t('forum.moveDown')" @click="move(index, 1)">
                  <ArrowDown class="h-3.5 w-3.5" />
                </button>
                <button type="button" class="btn btn-icon h-8 w-8 max-md:h-10 max-md:w-10" :aria-label="$t('forum.edit')" v-tooltip="$t('forum.edit')" @click="editingId = tag.id; adding = false">
                  <Pencil class="h-3.5 w-3.5" />
                </button>
                <button type="button" class="btn btn-icon h-8 w-8 hover:text-red-400 max-md:h-10 max-md:w-10" :disabled="busyId === tag.id" :aria-label="$t('common.delete')" v-tooltip="$t('common.delete')" @click="removeTag(tag)">
                  <Loader2 v-if="busyId === tag.id" class="h-3.5 w-3.5 animate-spin" />
                  <Trash2 v-else class="h-3.5 w-3.5" />
                </button>
              </div>
            </div>
          </li>
        </ul>

        <ForumTagEditor v-if="adding" :saving="isSaving" @save="addTag" @cancel="adding = false" />
        <button
          v-else-if="tags.length < MAX_FORUM_TAGS"
          type="button"
          data-testid="forum-add-tag"
          class="btn btn-secondary flex items-center gap-1.5 px-3 py-1.5 text-xs"
          @click="adding = true; editingId = null"
        >
          <Plus class="h-3.5 w-3.5" />
          <span>{{ $t('forum.addTag') }}</span>
        </button>
        <p v-else class="text-center text-xs text-mnema-tertiary">{{ $t('forum.maxTagsReached') }}</p>
      </section>

      <ForumSettingsForm :channel-id="channelId" :settings="settings" :no-tags="tags.length === 0" @saved="emit('settings-saved', $event)" />

      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary px-4 py-2 text-sm" @click="emit('close')">{{ $t('common.close') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
