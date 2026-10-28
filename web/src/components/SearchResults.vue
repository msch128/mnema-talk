<script setup lang="ts">
import type { Message } from '../types/domain'
import type { ChatSearchRoot } from '../types/rest'
import type { SearchProblem } from '../lib/searchProblems'
import { ref, watch, nextTick } from 'vue'
import { CircleAlert, Loader2, Search } from '@lucide/vue'
import SearchResultItem from './SearchResultItem.vue'

// The result list with its header (count, sort) and its empty, loading, error
// and "problem with a filter" states. The area keeps its size in every state,
// so the dialog never changes height.
type Sort = 'newest' | 'oldest'
const SORTS: readonly Sort[] = ['newest', 'oldest']

const props = defineProps<{
  results: Message[]
  roots: Record<string, ChatSearchRoot>
  hasMore: boolean
  loadingMore: boolean
  searching: boolean
  failed: boolean
  filterUnsupported?: boolean
  hasCriteria: boolean
  selectedIndex: number
  terms: string[]
  channelNames: Map<string, string>
  voiceChannels: Set<string>
  // The results are direct messages, not channel messages.
  conversations?: boolean
  sort?: Sort
  showV3?: boolean
  /** Filters that cannot be applied; nothing is searched while there are any. */
  problems?: SearchProblem[]
  /** The text part of the query and the number of filters, for the empty state. */
  queryText?: string
  filterCount?: number
  activeResultId?: string | undefined
}>()
const emit = defineEmits<{
  select: [msg: Message]
  more: []
  retry: []
  'clear-filters': []
  'update:sort': [sort: Sort]
}>()

const scrollEl = ref<HTMLElement | null>(null)
const current = (): Sort => props.sort ?? 'newest'
const hasProblems = () => (props.problems?.length ?? 0) > 0

// Left and Right move the sort; Up and Down stay with the results.
function onSortKeydown(e: KeyboardEvent) {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
  e.preventDefault()
  const step = e.key === 'ArrowRight' ? 1 : SORTS.length - 1
  const next = SORTS[(SORTS.indexOf(current()) + step) % SORTS.length]
  if (!next) return
  emit('update:sort', next)
  void nextTick(() => scrollEl.value?.parentElement?.querySelector<HTMLElement>(`[data-sort="${next}"]`)?.focus())
}

watch(() => props.selectedIndex, async () => {
  await nextTick()
  scrollEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
})
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div
      v-if="showV3 && !hasProblems() && (results.length > 0 || hasCriteria)"
      class="flex shrink-0 items-center justify-between gap-2 border-b border-mnema-hairline px-4 py-2 text-xs text-mnema-muted"
      data-testid="search-results-header"
    >
      <span class="font-medium text-mnema-tertiary" aria-live="polite">
        <template v-if="results.length > 0">{{ $t(hasMore ? 'chat.resultsCountMore' : 'chat.resultsCount', { count: results.length }) }}</template>
        <template v-else-if="searching">{{ $t('chat.searchSearching') }}</template>
        <template v-else>{{ $t('chat.searchResults') }}</template>
      </span>
      <div class="flex items-center gap-1.5" role="radiogroup" :aria-label="$t('chat.sortLabel')" @keydown="onSortKeydown">
        <span class="text-mnema-tertiary max-md:hidden">{{ $t('chat.sortBy') }}:</span>
        <div class="flex rounded-md border border-mnema-border bg-mnema-surface p-0.5">
          <button
            v-for="s in SORTS"
            :key="s"
            type="button"
            role="radio"
            :aria-checked="current() === s ? 'true' : 'false'"
            :tabindex="current() === s ? 0 : -1"
            :data-sort="s"
            :class="[
              'rounded-sm px-2 py-0.5 text-xs font-medium transition',
              current() === s ? 'bg-mnema-accent/20 text-mnema-accent' : 'text-mnema-muted hover:text-mnema-text'
            ]"
            @mousedown.prevent
            @click="emit('update:sort', s)"
          >
            {{ $t(`chat.sort${s.charAt(0).toUpperCase() + s.slice(1)}`) }}
          </button>
        </div>
      </div>
    </div>

    <div ref="scrollEl" class="min-h-0 flex-1 overflow-y-auto p-2">
      <ul v-if="hasProblems()" class="space-y-2 px-3 py-6" role="status" data-testid="search-problems">
        <li v-for="(p, i) in problems" :key="i" class="flex items-start gap-2 text-sm text-mnema-muted" data-search-problem>
          <CircleAlert class="mt-0.5 h-4 w-4 shrink-0 text-mnema-danger" aria-hidden="true" />
          <span v-if="p.kind === 'member' || p.kind === 'channel'">{{ $t(p.kind === 'member' ? 'chat.searchProblemMember' : 'chat.searchProblemChannel', { value: p.value, filter: p.filter }) }}</span>
          <span v-else-if="p.kind === 'date'">{{ $t('chat.searchProblemDate', { value: p.value, filter: p.filter }) }}</span>
          <span v-else-if="p.kind === 'option'">{{ $t('chat.searchProblemOption', { value: p.value, filter: p.filter, options: p.options }) }}</span>
          <span v-else>{{ $t('chat.searchProblemEmpty', { filter: p.filter }) }}</span>
        </li>
      </ul>
      <div v-else-if="!hasCriteria" class="flex flex-col items-center gap-2 px-6 py-16 text-center" data-testid="search-empty">
        <Search class="h-6 w-6 text-mnema-tertiary" aria-hidden="true" />
        <p class="text-sm text-mnema-muted">{{ $t('chat.searchHint') }}</p>
        <p class="text-xs text-mnema-tertiary">{{ $t('chat.searchSyntaxTip') }}</p>
      </div>
      <div v-else-if="failed && filterUnsupported" class="py-12 text-center text-sm text-mnema-tertiary" role="alert" data-testid="search-filter-unsupported">{{ $t('chat.searchFilterUnsupported') }}</div>
      <div v-else-if="failed" class="flex flex-col items-center gap-3 px-6 py-14 text-center" role="alert" data-testid="search-failed">
        <CircleAlert class="h-6 w-6 text-mnema-danger" aria-hidden="true" />
        <p class="text-sm text-mnema-muted">{{ $t('chat.searchFailed') }}</p>
        <button type="button" class="btn btn-secondary btn-sm" data-search-retry @mousedown.prevent @click="emit('retry')">{{ $t('chat.searchRetry') }}</button>
      </div>
      <div v-else-if="!results.length && searching" class="space-y-3 px-2 py-3" aria-hidden="true" data-testid="search-skeleton">
        <div v-for="n in 6" :key="n" class="flex animate-pulse items-start gap-3 px-1">
          <div class="h-8 w-8 shrink-0 rounded-full bg-mnema-hover"></div>
          <div class="flex-1 space-y-2 pt-1">
            <div class="h-3 w-1/3 rounded-sm bg-mnema-hover"></div>
            <div class="h-3 w-5/6 rounded-sm bg-mnema-hover"></div>
          </div>
        </div>
      </div>
      <div v-else-if="!results.length" class="flex flex-col items-center gap-2 px-6 py-14 text-center" data-testid="search-no-results">
        <p class="text-sm text-mnema-muted">{{ $t('chat.noResults') }}</p>
        <p class="text-xs text-mnema-tertiary">{{ queryText ? $t('chat.noResultsFor', { query: queryText }) : $t('chat.noResultsFiltered') }}</p>
        <button v-if="(filterCount ?? 0) > 0" type="button" class="btn btn-secondary btn-sm mt-1" data-search-clear-filters @mousedown.prevent @click="emit('clear-filters')">{{ $t('chat.searchClearFilters') }}</button>
      </div>

      <div
        id="search-results"
        role="listbox"
        :aria-label="$t('chat.searchResults')"
        :aria-busy="searching ? 'true' : 'false'"
        :aria-activedescendant="activeResultId"
        :tabindex="results.length ? 0 : -1"
        :class="['space-y-0.5 rounded-lg transition-opacity focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-mnema-accent/50', searching && results.length ? 'opacity-60' : '']"
      >
        <SearchResultItem
          v-for="(msg, idx) in results"
          :key="msg.id"
          :msg="msg"
          :index="idx"
          :selected="selectedIndex === idx"
          :channel-name="channelNames.get(msg.channel_id)"
          :voice="voiceChannels.has(msg.channel_id)"
          :conversation="conversations === true"
          :terms="terms"
          :root="msg.parent_id ? roots[msg.parent_id] : undefined"
          @select="emit('select', $event)"
        />
      </div>

      <button
        v-if="hasMore"
        type="button"
        :disabled="loadingMore"
        class="btn btn-ghost mt-1 flex w-full items-center justify-center gap-2"
        @mousedown.prevent
        @click="emit('more')"
      >
        <Loader2 v-if="loadingMore" class="w-4 h-4 animate-spin" />
        {{ $t('chat.searchMore') }}
      </button>
    </div>
  </div>
</template>
