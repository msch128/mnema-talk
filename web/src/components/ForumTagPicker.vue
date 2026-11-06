<script setup lang="ts">
import { computed } from 'vue'
import type { ForumTag } from '../types/domain'
import { MAX_POST_TAGS } from '../lib/forumTags'
import ForumTagChip from './ForumTagChip.vue'

// Chooses the tags of a post out of the forum's tags, at most MAX_POST_TAGS.
// A moderated tag is applied and removed only by someone who may moderate:
// for everyone else it stays as it is and shows a lock.
const props = defineProps<{
  tags: ForumTag[]
  canModerate: boolean
  required?: boolean
  labelId: string
}>()

const selected = defineModel<string[]>({ required: true })

const full = computed(() => selected.value.length >= MAX_POST_TAGS)

function frozen(tag: ForumTag): boolean {
  return tag.moderated && !props.canModerate
}

function toggle(tag: ForumTag) {
  if (frozen(tag)) return
  const at = selected.value.indexOf(tag.id)
  if (at >= 0) selected.value = selected.value.filter(id => id !== tag.id)
  else if (!full.value) selected.value = [...selected.value, tag.id]
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-1.5">
      <span :id="labelId" class="text-xs font-semibold text-mnema-secondary uppercase tracking-wider">
        {{ required ? $t('forum.tagsLabelRequired') : $t('forum.tagsLabel') }}
      </span>
      <span class="text-xs text-mnema-tertiary tabular-nums">{{ selected.length }}/{{ MAX_POST_TAGS }}</span>
    </div>
    <div
      role="group"
      :aria-labelledby="labelId"
      class="flex flex-wrap gap-1.5 max-h-32 overflow-y-auto p-1.5 bg-mnema-surface/50 border border-mnema-hairline rounded-lg"
    >
      <ForumTagChip
        v-for="tag in tags"
        :key="tag.id"
        data-testid="tag-toggle-btn"
        :tag="tag"
        selectable
        :selected="selected.includes(tag.id)"
        :disabled="frozen(tag) || (!selected.includes(tag.id) && full)"
        :locked="tag.moderated"
        @toggle="toggle"
      />
    </div>
  </div>
</template>
