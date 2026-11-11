<script setup lang="ts">
import { MessagesSquare, SearchX } from '@lucide/vue'

// What the post list says when there is nothing to list: a new forum explains
// what a forum is and how to start; a filter or search that finds nothing says so.
defineProps<{
  /** A tag or a search is narrowing the list. */
  filtered: boolean
  canPost: boolean
  guidelines: string
}>()

const emit = defineEmits<{ create: []; reset: [] }>()
</script>

<template>
  <div v-if="filtered" data-testid="forum-empty-filtered" class="flex min-h-64 flex-col items-center justify-center gap-3 p-6 text-center">
    <div class="flex h-12 w-12 items-center justify-center rounded-full border border-mnema-hairline bg-mnema-raised text-mnema-tertiary">
      <SearchX class="h-6 w-6" />
    </div>
    <div class="space-y-1">
      <h2 class="text-sm font-semibold text-mnema-text">{{ $t('forum.noMatchTitle') }}</h2>
      <p class="max-w-sm text-xs text-mnema-tertiary">{{ $t('forum.emptyTagFilter') }}</p>
    </div>
    <button type="button" class="btn btn-secondary px-3.5 py-1.5 text-xs" @click="emit('reset')">{{ $t('forum.resetFilters') }}</button>
  </div>

  <div v-else data-testid="forum-empty" class="mx-auto flex min-h-64 max-w-md flex-col items-center justify-center gap-3 p-6 text-center">
    <div class="flex h-12 w-12 items-center justify-center rounded-full border border-mnema-hairline bg-mnema-raised text-mnema-accent">
      <MessagesSquare class="h-6 w-6" />
    </div>
    <div class="space-y-1.5">
      <h2 class="text-sm font-semibold text-mnema-text">{{ $t('forum.emptyTitle') }}</h2>
      <p class="text-sm text-mnema-secondary">{{ $t('forum.emptyExplain') }}</p>
      <p class="text-xs text-mnema-tertiary">{{ canPost ? $t('forum.emptyStart') : $t('forum.emptyReadOnly') }}</p>
    </div>
    <p v-if="guidelines" class="w-full whitespace-pre-line break-words rounded-lg border border-mnema-hairline bg-mnema-surface/60 p-3 text-left text-xs text-mnema-secondary">{{ guidelines }}</p>
    <button v-if="canPost" type="button" data-testid="forum-empty-new" class="btn btn-primary mt-1 px-3.5 py-1.5 text-xs" @click="emit('create')">{{ $t('forum.newPost') }}</button>
  </div>
</template>
