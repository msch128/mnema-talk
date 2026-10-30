<script setup lang="ts">
import { Search, X, ArrowUpDown } from '@lucide/vue'
import type { ForumTag } from '../types/domain'
import type { ForumSort } from '../composables/useForumPosts'
import ForumTagChip from './ForumTagChip.vue'

// Search, sort and the tag filter of a forum list.
defineProps<{ tags: ForumTag[] }>()

const search = defineModel<string>('search', { required: true })
const sort = defineModel<ForumSort>('sort', { required: true })
const tagId = defineModel<string | null>('tagId', { required: true })

function pick(id: string) {
  tagId.value = tagId.value === id ? null : id
}
</script>

<template>
  <section class="shrink-0 space-y-2 border-b border-mnema-hairline/80 bg-mnema-canvas px-4 py-2.5 max-sm:px-3" data-testid="forum-toolbar">
    <div class="flex items-center gap-2">
      <div class="relative min-w-0 flex-1">
        <Search class="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-mnema-tertiary" />
        <input
          v-model="search"
          type="search"
          data-testid="forum-search"
          :aria-label="$t('forum.searchLabel')"
          :placeholder="$t('forum.searchPlaceholder')"
          v-max-chars="100"
          class="h-9 w-full rounded-lg border border-mnema-border bg-mnema-surface pl-8 pr-8 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden focus:border-mnema-accent max-md:h-11 [&::-webkit-search-cancel-button]:hidden"
          @keydown.esc.stop="search = ''"
        />
        <button
          v-if="search"
          type="button"
          class="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-mnema-tertiary hover:text-mnema-text max-md:h-9 max-md:w-9"
          :aria-label="$t('forum.clearSearch')"
          @click="search = ''"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
      <label class="flex shrink-0 items-center gap-1.5 text-xs text-mnema-tertiary">
        <ArrowUpDown class="h-3.5 w-3.5" />
        <select
          v-model="sort"
          data-testid="forum-sort-select"
          :aria-label="$t('forum.sortLabel')"
          class="h-9 cursor-pointer rounded-lg border border-mnema-border bg-mnema-surface px-2 text-xs text-mnema-text focus:outline-hidden max-md:h-11"
        >
          <option value="activity">{{ $t('forum.sortActivity') }}</option>
          <option value="newest">{{ $t('forum.sortNewest') }}</option>
        </select>
      </label>
    </div>

    <div v-if="tags.length" class="-mx-3 flex items-center gap-1.5 overflow-x-auto px-3 pb-0.5 sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0" role="group" :aria-label="$t('forum.filterByTag')">
      <button
        type="button"
        data-testid="filter-all-tags"
        :aria-pressed="tagId === null ? 'true' : 'false'"
        :class="[
          'shrink-0 cursor-pointer rounded-md border px-2.5 py-0.5 text-xs font-medium transition',
          tagId === null
            ? 'border-mnema-accent bg-mnema-accent/15 text-mnema-text'
            : 'border-mnema-hairline bg-mnema-surface text-mnema-secondary hover:border-mnema-border hover:text-mnema-text'
        ]"
        @click="tagId = null"
      >
        {{ $t('forum.allTags') }}
      </button>
      <ForumTagChip
        v-for="tag in tags"
        :key="tag.id"
        class="shrink-0"
        data-testid="filter-tag-pill"
        :tag="tag"
        selectable
        :selected="tagId === tag.id"
        @toggle="pick(tag.id)"
      />
    </div>
  </section>
</template>
