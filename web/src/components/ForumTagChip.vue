<script setup lang="ts">
import { Lock } from '@lucide/vue'
import type { ForumTag } from '../types/domain'
import { tagChipClass } from '../lib/forumTags'

// A forum tag: its emoji, its name, its colour. With `selectable` it is a
// toggle button (the filter row, the tag picker); otherwise plain text.
defineProps<{
  tag: ForumTag
  selectable?: boolean
  selected?: boolean
  disabled?: boolean
  /** Shown as a small lock on a moderated tag that this member cannot change. */
  locked?: boolean
}>()

const emit = defineEmits<{ toggle: [tag: ForumTag] }>()
</script>

<template>
  <component
    :is="selectable ? 'button' : 'span'"
    :type="selectable ? 'button' : undefined"
    :aria-pressed="selectable ? (selected ? 'true' : 'false') : undefined"
    :disabled="selectable ? disabled : undefined"
    :class="[
      'inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-medium border transition',
      tagChipClass(tag.color),
      selectable ? 'cursor-pointer max-md:py-1.5 hover:brightness-125 disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:brightness-100' : '',
      selected ? 'ring-1 ring-mnema-accent border-mnema-accent' : ''
    ]"
    @click="selectable && emit('toggle', tag)"
  >
    <span v-if="tag.emoji">{{ tag.emoji }}</span>
    <span class="truncate max-w-40">{{ tag.name }}</span>
    <Lock v-if="locked" class="w-3 h-3 opacity-70" :aria-label="$t('forum.moderatedTag')" />
  </component>
</template>
