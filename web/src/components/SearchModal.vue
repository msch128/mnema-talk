<script setup lang="ts">
import type { Message } from '../types/domain'
import type { ChatSearchRoot } from '../types/rest'
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import { decodeSearchResult } from '../types/domain'
import { isChatSearchRoot } from '../types/rest'
import { api, isApiError } from '../lib/api'
import { searchScope, searchText } from '../lib/globalSearch'
import { dmAsChannel } from '../lib/dm'
import { buildSearchParams, isSearchable, parseSearchQuery, serializeSearchQuery, textTerms } from '../lib/searchQuery'
import { diagnoseSearch } from '../lib/searchProblems'
import { markFiltersSupported, markFiltersUnsupported, usesNewFilters } from '../lib/searchSupport'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useOnboardingStore } from '../stores/onboarding'
import BaseDialog from './BaseDialog.vue'
import SearchInput from './SearchInput.vue'
import SearchResults from './SearchResults.vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const PAGE = 25

const chatStore = useChatStore()
const authStore = useAuthStore()
const onboarding = useOnboardingStore()
const serverFeatures = useServerFeaturesStore()
const features = serverFeatures

// The server's feature list (loaded by the chat header) says whether it knows
// the filters of 1.1; a 1.0.x server sends none. A refused search teaches the same.
watch(() => [serverFeatures.loaded, serverFeatures.has('search.filters.v2')] as const, ([loaded, known]) => {
  if (!loaded) return
  if (known) markFiltersSupported()
  else markFiltersUnsupported()
}, { immediate: true })
const results = ref<Message[]>([])
const roots = ref<Record<string, ChatSearchRoot>>({})
const hasMore = ref(false)
const isSearching = ref(false)
const isLoadingMore = ref(false)
const failed = ref(false)
const filterUnsupported = ref(false)
const selectedIndex = ref(0)
const inputRef = ref<InstanceType<typeof SearchInput> | null>(null)

const inDm = computed(() => features.dm && searchScope.value === 'dm')
// The conversations take the channels' place in the "in:" filter when searching them.
const conversations = computed(() => chatStore.dms.map(d => dmAsChannel(d, authStore.user?.id)))
const lookup = computed(() => ({
  members: chatStore.members,
  // Every channel has a chat: text channels and the chat next to a voice channel's Talk.
  channels: inDm.value ? conversations.value : chatStore.allChannels,
}))
const parsed = computed(() => parseSearchQuery(searchText.value))
const currentSort = ref<'newest' | 'oldest' | 'relevance'>('newest')
const showV3 = computed(() => serverFeatures.has('search.filters.v3'))
const built = computed(() => {
  const extra: Record<string, string> = { limit: String(PAGE) }
  if (inDm.value) extra.dm = 'true'
  if (showV3.value && currentSort.value !== 'newest') {
    extra.sort = currentSort.value
  }
  return buildSearchParams(parsed.value, lookup.value, extra)
})
const diagnosis = computed(() => diagnoseSearch(searchText.value, parsed.value.filters, built.value.unresolved))
const hasCriteria = computed(() => isSearchable(built.value.params))
const terms = computed(() => textTerms(parsed.value.text))
const filterCount = computed(() => Object.keys(parsed.value.filters).length)
const channelNames = computed(() => new Map(lookup.value.channels.map(c => [c.id, c.name])))
const voiceChannels = computed(() => new Set(inDm.value ? [] : chatStore.allChannels.filter(c => c.type === 'voice').map(c => c.id)))
const activeResultId = computed(() => (results.value.length ? `search-result-${selectedIndex.value}` : undefined))

let searchTimeout: ReturnType<typeof setTimeout> | undefined
let searchSeq = 0

function close() {
  emit('update:modelValue', false)
}

function pageParams(before?: string) {
  const params = new URLSearchParams(built.value.params)
  if (before) params.set('before', before)
  return params
}

function keepRoots(raw: Record<string, unknown> | undefined) {
  const next: Record<string, ChatSearchRoot> = {}
  for (const [id, root] of Object.entries(raw ?? {})) if (isChatSearchRoot(root)) next[id] = root
  return next
}

async function performSearch() {
  clearTimeout(searchTimeout)
  const seq = ++searchSeq
  failed.value = false
  filterUnsupported.value = false
  // A token still being typed: keep what is shown until it is finished.
  if (diagnosis.value.pending && !diagnosis.value.problems.length) {
    isSearching.value = false
    return
  }
  // A filter that cannot be applied is explained, never searched without it.
  if (!hasCriteria.value || diagnosis.value.problems.length) {
    results.value = []
    roots.value = {}
    hasMore.value = false
    isSearching.value = false
    return
  }

  isSearching.value = true
  try {
    const data = await api(`/api/search?${pageParams().toString()}`, { decode: decodeSearchResult })
    if (seq !== searchSeq) return
    results.value = data.messages || []
    roots.value = keepRoots(data.roots)
    hasMore.value = !!data.has_more
    selectedIndex.value = 0
    // Whoever has searched knows the shortcut: the tip about it is done for this account.
    void onboarding.dismiss('search')
  } catch (err) {
    if (seq !== searchSeq) return
    console.warn('Search error:', err)
    results.value = []
    roots.value = {}
    hasMore.value = false
    // A 1.0.x server refuses the filters of 1.1: say so, and stop offering them.
    if (isApiError(err) && err.status === 400 && usesNewFilters(parsed.value.filters)) {
      markFiltersUnsupported()
      filterUnsupported.value = true
    }
    failed.value = true
  } finally {
    if (seq === searchSeq) isSearching.value = false
  }
}

async function loadMore() {
  const last = results.value[results.value.length - 1]
  if (!last || isLoadingMore.value || !hasMore.value) return
  const seq = searchSeq
  isLoadingMore.value = true
  try {
    const data = await api(`/api/search?${pageParams(last.id).toString()}`, { decode: decodeSearchResult })
    if (seq !== searchSeq) return
    results.value = [...results.value, ...(data.messages || [])]
    roots.value = { ...roots.value, ...keepRoots(data.roots) }
    hasMore.value = !!data.has_more
  } catch (err) {
    console.warn('Search error:', err)
  } finally {
    isLoadingMore.value = false
  }
}

function handleSelect(msg: Message) {
  chatStore.goToMessage(msg)
  close()
}

// The input owns the keyboard: Up and Down move through the results (the
// suggestion list and the filter popover take their own keys first), Enter
// opens the selected one. Focus on the sort choice or "load more" keeps the
// button's own Enter. Escape peels one layer (popover, suggestions) before the
// dialog closes.
function handleKeydown(e: KeyboardEvent) {
  if (!results.value.length || !(e.target instanceof HTMLElement)) return
  if (e.target.closest('[data-search-popover]')) return
  const n = results.value.length

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value + 1) % n
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value - 1 + n) % n
  } else if (e.key === 'Enter' && (e.target instanceof HTMLInputElement || e.target.id === 'search-results')) {
    e.preventDefault()
    const selected = results.value[selectedIndex.value]
    if (selected) handleSelect(selected)
  }
}

function dismiss() {
  if (!inputRef.value?.dismissLayer()) close()
}

function clearFilters() {
  searchText.value = serializeSearchQuery({ text: parsed.value.text, filters: {} })
}

// The query line drives the search; the request is sent when typing pauses.
watch(searchText, () => {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(performSearch, 250)
})

// Switching between channels and direct messages searches the other set at once.
watch(inDm, () => {
  if (props.modelValue) void performSearch()
})

watch(currentSort, () => {
  if (props.modelValue && hasCriteria.value) void performSearch()
})

watch(() => props.modelValue, (open) => {
  if (open) {
    selectedIndex.value = 0
    currentSort.value = 'newest'
    nextTick(() => {
      if (hasCriteria.value) performSearch()
    })
  } else {
    clearTimeout(searchTimeout)
  }
})

onUnmounted(() => {
  clearTimeout(searchTimeout)
})
</script>

<template>
  <Teleport to="body">
    <BaseDialog
      v-if="modelValue"
      align="top"
      panel-class="max-w-2xl h-192"
      initial-focus="[data-search-input]"
      @close="dismiss"
    >
      <template #default="{ titleId }">
        <h2 :id="titleId" class="sr-only">{{ $t('chat.search') }}</h2>
        <div class="flex h-full min-h-0 flex-col" @keydown="handleKeydown">
          <SearchInput
            ref="inputRef"
            v-model="searchText"
            :lookup="lookup"
            :loading="isSearching"
            :unresolved="built.unresolved"
            :active-result-id="activeResultId"
            :v3-supported="showV3"
            :show-scope="features.dm"
            :scope="searchScope"
            @update:scope="searchScope = $event"
            @close="close"
          />
          <SearchResults
            :sort="currentSort"
            :show-v3="showV3"
            :results="results"
            :roots="roots"
            :has-more="hasMore"
            :loading-more="isLoadingMore"
            :searching="isSearching"
            :failed="failed"
            :filter-unsupported="filterUnsupported"
            :has-criteria="hasCriteria"
            :problems="diagnosis.problems"
            :query-text="parsed.text"
            :filter-count="filterCount"
            :selected-index="selectedIndex"
            :active-result-id="activeResultId"
            :terms="terms"
            :channel-names="channelNames"
            :voice-channels="voiceChannels"
            :conversations="inDm"
            @update:sort="currentSort = $event"
            @select="handleSelect"
            @more="loadMore"
            @retry="performSearch"
            @clear-filters="clearFilters"
          />
          <div v-if="showV3" class="hidden shrink-0 items-center gap-4 border-t border-mnema-hairline bg-mnema-raised px-4 py-1.5 font-mono text-xs text-mnema-tertiary md:flex" data-testid="search-keys">
            <span>↑↓ {{ $t('chat.searchKeyNavigate') }}</span>
            <span>↵ {{ $t('chat.searchKeyOpen') }}</span>
            <span class="ml-auto">Esc {{ $t('common.close') }}</span>
          </div>
        </div>
      </template>
    </BaseDialog>
  </Teleport>
</template>
