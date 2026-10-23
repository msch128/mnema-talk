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
  hasCriteria: boolean
  selectedIndex: number
  terms: string[]
  channelNames: Map<string, string>
  voiceChannels: Set<string>
  // The results are direct messages, not channel messages.
  conversations?: boolean
}>()
const emit = defineEmits<{ select: [msg: Message]; more: [] }>()

const listEl = ref<HTMLElement | null>(null)

watch(() => props.selectedIndex, async () => {
  await nextTick()
  listEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
})
</script>

<template>
  <div id="search-results" ref="listEl" role="listbox" class="flex-1 overflow-y-auto p-2 space-y-1">
    <div v-if="!hasCriteria" class="py-12 text-center text-sm text-mnema-tertiary">{{ $t('chat.searchHint') }}</div>
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
      class="w-full py-2 rounded-lg text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition flex items-center justify-center gap-2 disabled:opacity-50"
      @click="emit('more')"
    >
      <Loader2 v-if="loadingMore" class="w-4 h-4 animate-spin" />
      {{ $t('chat.searchMore') }}
    </button>
  </div>
</template>
