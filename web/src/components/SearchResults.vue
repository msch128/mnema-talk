<script setup lang="ts">
import type { Message } from '../types/domain'
import type { ChatSearchRoot } from '../types/rest'
import { ref, watch, nextTick } from 'vue'
import { Loader2 } from '@lucide/vue'
import SearchResultItem from './SearchResultItem.vue'

// The result list with its empty, error and "load more" states.
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
  sort?: 'newest' | 'oldest' | 'relevance'
  showV3?: boolean
}>()
const emit = defineEmits<{
  select: [msg: Message]
  more: []
  'update:sort': [sort: 'newest' | 'oldest' | 'relevance']
}>()

const listEl = ref<HTMLElement | null>(null)

function onSortKeydown(e: KeyboardEvent) {
  const options = ['newest', 'oldest', 'relevance'] as const
  const current = props.sort ?? 'newest'
  const idx = options.indexOf(current)
  if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
    e.preventDefault()
    const next = options[(idx + 1) % options.length]
    if (next) emit('update:sort', next)
  } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
    e.preventDefault()
    const prev = options[(idx - 1 + options.length) % options.length]
    if (prev) emit('update:sort', prev)
  }
}

watch(() => props.selectedIndex, async () => {
  await nextTick()
  listEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
})
</script>

<template>
  <div id="search-results" ref="listEl" role="listbox" class="flex-1 overflow-y-auto p-2 space-y-1">
    <div
      v-if="showV3 && (results.length > 0 || hasCriteria)"
      class="mb-2 flex items-center justify-between border-b border-mnema-hairline px-2 pb-2 text-xs text-mnema-muted"
      data-testid="search-results-header"
    >
      <span class="font-medium text-mnema-tertiary">
        {{ results.length > 0 ? $t('chat.resultsCount', { count: results.length }) : $t('chat.searchResults') }}
      </span>
      <div class="flex items-center gap-1.5" role="radiogroup" :aria-label="$t('chat.sortLabel')" @keydown="onSortKeydown">
        <span class="text-mnema-tertiary">{{ $t('chat.sortBy') }}:</span>
        <div class="flex rounded-md border border-mnema-border bg-mnema-surface p-0.5">
          <button
            v-for="s in (['newest', 'oldest', 'relevance'] as const)"
            :key="s"
            type="button"
            role="radio"
            :aria-checked="(sort ?? 'newest') === s ? 'true' : 'false'"
            :data-sort="s"
            :class="[
              'rounded-sm px-2 py-0.5 text-xs font-medium transition',
              (sort ?? 'newest') === s
                ? 'bg-mnema-accent/20 text-mnema-accent'
                : 'text-mnema-muted hover:text-mnema-text'
            ]"
            @click="emit('update:sort', s)"
          >
            {{ $t(`chat.sort${s.charAt(0).toUpperCase() + s.slice(1)}`) }}
          </button>
        </div>
      </div>
    </div>
    <div v-if="!hasCriteria" class="py-12 text-center text-sm text-mnema-tertiary">{{ $t('chat.searchHint') }}</div>
    <div v-else-if="failed && filterUnsupported" class="py-12 text-center text-sm text-mnema-tertiary" role="alert" data-testid="search-filter-unsupported">{{ $t('chat.searchFilterUnsupported') }}</div>
    <div v-else-if="failed" class="py-12 text-center text-sm text-mnema-tertiary" role="alert">{{ $t('chat.searchFailed') }}</div>
    <div v-else-if="!results.length && !searching" class="py-12 text-center text-sm text-mnema-tertiary">{{ $t('chat.noResults') }}</div>

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

    <button
      v-if="hasMore"
      type="button"
      :disabled="loadingMore"
      class="btn btn-ghost w-full flex items-center justify-center gap-2"
      @click="emit('more')"
    >
      <Loader2 v-if="loadingMore" class="w-4 h-4 animate-spin" />
      {{ $t('chat.searchMore') }}
    </button>
  </div>
</template>
