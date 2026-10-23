<script setup lang="ts">
import type { Message } from '../types/domain'
import type { ChatSearchRoot } from '../types/rest'
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import { decodeSearchResult } from '../types/domain'
import { isChatSearchRoot } from '../types/rest'
import { api } from '../lib/api'
import { searchScope, searchText } from '../lib/globalSearch'
import { dmAsChannel } from '../lib/dm'
import { buildSearchParams, isSearchable, parseSearchQuery } from '../lib/searchQuery'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { useServerFeaturesStore } from '../stores/serverFeatures'
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
const features = useServerFeaturesStore()
const results = ref<Message[]>([])
const roots = ref<Record<string, ChatSearchRoot>>({})
const hasMore = ref(false)
const isSearching = ref(false)
const isLoadingMore = ref(false)
const failed = ref(false)
const selectedIndex = ref(0)

const inDm = computed(() => features.dm && searchScope.value === 'dm')
// The conversations take the channels' place in the "in:" filter when searching them.
const conversations = computed(() => chatStore.dms.map(d => dmAsChannel(d, authStore.user?.id)))
const lookup = computed(() => ({
  members: chatStore.members,
  // Every channel has a chat: text channels and the chat next to a voice channel's Talk.
  channels: inDm.value ? conversations.value : chatStore.allChannels,
}))
const parsed = computed(() => parseSearchQuery(searchText.value))
const built = computed(() => buildSearchParams(parsed.value, lookup.value, inDm.value ? { limit: String(PAGE), dm: 'true' } : { limit: String(PAGE) }))
const hasCriteria = computed(() => isSearchable(built.value.params))
const terms = computed(() => parsed.value.text.split(/\s+/).filter(Boolean))
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
  if (!hasCriteria.value) {
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
  } catch (err) {
    if (seq !== searchSeq) return
    console.warn('Search error:', err)
    results.value = []
    roots.value = {}
    hasMore.value = false
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

// Escape, the focus trap and focus restore come from BaseDialog. The input
// handles its own suggestion keys and stops them before they reach this.
function handleKeydown(e: KeyboardEvent) {
  if (!results.value.length) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value + 1) % results.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value - 1 + results.value.length) % results.value.length
  } else if (e.key === 'Enter' && e.target instanceof HTMLInputElement) {
    e.preventDefault()
    const selected = results.value[selectedIndex.value]
    if (selected) handleSelect(selected)
  }
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

watch(() => props.modelValue, (open) => {
  if (open) {
    selectedIndex.value = 0
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
      panel-class="max-w-2xl"
      initial-focus="[data-search-input]"
      @close="close"
    >
      <template #default="{ titleId }">
        <h2 :id="titleId" class="sr-only">{{ $t('chat.search') }}</h2>
        <div class="flex max-h-[75vh] min-h-0 flex-col" @keydown="handleKeydown">
          <div v-if="features.dm" role="group" :aria-label="$t('dm.searchScope')" class="flex gap-1 px-3 pt-3" data-testid="search-scope">
            <button
              v-for="scope in (['channels', 'dm'] as const)"
              :key="scope"
              type="button"
              :aria-pressed="(scope === 'dm') === inDm ? 'true' : 'false'"
              :data-scope="scope"
              :class="['rounded-md border px-2.5 py-1 text-xs font-semibold transition max-md:min-h-11', (scope === 'dm') === inDm ? 'border-mnema-accent/40 bg-mnema-accent/15 text-mnema-accent' : 'border-transparent text-mnema-muted hover:text-mnema-text']"
              @click="searchScope = scope"
            >{{ $t(scope === 'dm' ? 'dm.section' : 'sidebar.channels') }}</button>
          </div>
          <SearchInput
            v-model="searchText"
            :lookup="lookup"
            :loading="isSearching"
            :unresolved="built.unresolved"
            :active-result-id="activeResultId"
            @close="close"
          />
          <SearchResults
            :results="results"
            :roots="roots"
            :has-more="hasMore"
            :loading-more="isLoadingMore"
            :searching="isSearching"
            :failed="failed"
            :has-criteria="hasCriteria"
            :selected-index="selectedIndex"
            :terms="terms"
            :channel-names="channelNames"
            :voice-channels="voiceChannels"
            :conversations="inDm"
            @select="handleSelect"
            @more="loadMore"
          />
        </div>
      </template>
    </BaseDialog>
  </Teleport>
</template>
