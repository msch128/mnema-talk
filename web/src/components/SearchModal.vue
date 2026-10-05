<script setup>
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import { Search, Hash, Volume2, Paperclip, Image as ImageIcon, Link as LinkIcon, X, Loader2, ArrowRight, CornerDownRight } from '@lucide/vue'
import { api } from '../lib/api'
import { useChatStore } from '../stores/chat'
import { locale } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import BaseDialog from './BaseDialog.vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue'])

const PAGE = 25

const chatStore = useChatStore()
const query = ref('')
const results = ref([])
const hasMore = ref(false)
const isSearching = ref(false)
const isLoadingMore = ref(false)
const failed = ref(false)
const channelId = ref('')
const authorId = ref('')
const has = ref('') // '' | 'file' | 'image' | 'link'
const selectedIndex = ref(0)
const searchInput = ref(null)
const listEl = ref(null)

const hasHas = [
  { value: 'file', label: 'chat.hasAttachment', icon: Paperclip },
  { value: 'image', label: 'chat.hasImage', icon: ImageIcon },
  { value: 'link', label: 'chat.hasLink', icon: LinkIcon }
]

// Every channel has a chat: text channels, then voice channels (their Talk's chat).
const textChannels = computed(() => chatStore.allChannels.filter(c => c.type !== 'voice'))
const voiceChannels = computed(() => chatStore.allChannels.filter(c => c.type === 'voice'))
const channelTypes = computed(() => new Map(chatStore.allChannels.map(c => [c.id, c.type])))
const channelNames = computed(() => new Map(chatStore.allChannels.map(c => [c.id, c.name])))
const hasCriteria = computed(() => !!(query.value.trim() || channelId.value || authorId.value || has.value))

let searchTimeout = null
let searchSeq = 0

function close() {
  emit('update:modelValue', false)
}

function buildParams(before) {
  const params = new URLSearchParams({ limit: String(PAGE) })
  const q = query.value.trim()
  if (q) params.set('q', q)
  if (channelId.value) params.set('channel_id', channelId.value)
  if (authorId.value) params.set('author_id', authorId.value)
  if (has.value) params.set('has', has.value)
  if (before) params.set('before', before)
  return params
}

async function performSearch() {
  clearTimeout(searchTimeout)
  const seq = ++searchSeq
  failed.value = false
  if (!hasCriteria.value) {
    results.value = []
    hasMore.value = false
    isSearching.value = false
    return
  }

  isSearching.value = true
  try {
    const data = await api(`/api/search?${buildParams().toString()}`)
    if (seq !== searchSeq) return
    results.value = data.messages || []
    hasMore.value = !!data.has_more
    selectedIndex.value = 0
  } catch (err) {
    if (seq !== searchSeq) return
    console.warn('Search error:', err)
    results.value = []
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
    const data = await api(`/api/search?${buildParams(last.id).toString()}`)
    if (seq !== searchSeq) return
    results.value = [...results.value, ...(data.messages || [])]
    hasMore.value = !!data.has_more
  } catch (err) {
    console.warn('Search error:', err)
  } finally {
    isLoadingMore.value = false
  }
}

function onInput() {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(performSearch, 250)
}

function toggleHas(value) {
  has.value = has.value === value ? '' : value
  performSearch()
}

function handleSelect(msg) {
  chatStore.goToMessage(msg)
  close()
}

// Escape, the focus trap and focus restore come from BaseDialog.
function handleKeydown(e) {
  if (e.target?.tagName === 'SELECT') return
  if (!results.value.length) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value + 1) % results.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value - 1 + results.value.length) % results.value.length
  } else if (e.key === 'Enter' && e.target === searchInput.value) {
    e.preventDefault()
    if (selectedIndex.value >= 0 && selectedIndex.value < results.value.length) {
      handleSelect(results.value[selectedIndex.value])
    }
  }
}

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return d.toLocaleDateString(locale.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

watch(selectedIndex, async () => {
  await nextTick()
  listEl.value?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
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
          <!-- Search Input Bar -->
          <div class="flex items-center gap-3 px-4 py-3.5 border-b border-mnema-border bg-mnema-surface/50">
            <Search class="w-5 h-5 text-mnema-tertiary flex-shrink-0" />
            <input
              ref="searchInput"
              data-search-input
              v-model="query"
              type="text"
              role="combobox"
              aria-expanded="true"
              aria-controls="search-results"
              :aria-activedescendant="results.length ? `search-result-${selectedIndex}` : undefined"
              :placeholder="$t('chat.searchPlaceholder')"
              :aria-label="$t('chat.search')"
              class="flex-1 bg-transparent text-base text-mnema-text placeholder-mnema-tertiary focus:outline-none"
              @input="onInput"
            />
            <Loader2 v-if="isSearching" class="w-4 h-4 animate-spin text-mnema-accent flex-shrink-0" />
            <button
              type="button"
              data-dialog-close
              class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
              v-tooltip="$t('common.close')"
              @click="close"
            >
              <X class="w-4 h-4" />
            </button>
          </div>

          <!-- Filters -->
          <div class="flex flex-wrap items-center gap-2 px-4 py-2 border-b border-mnema-hairline bg-mnema-raised text-xs text-mnema-muted">
            <span>{{ $t('chat.filter') }}:</span>

            <select
              v-model="channelId"
              :aria-label="$t('chat.searchChannel')"
              class="max-w-[10rem] px-2 py-1 rounded-md border border-mnema-border bg-mnema-surface text-mnema-muted focus:outline-none focus:border-mnema-accent"
              @change="performSearch"
            >
              <option value="">{{ $t('chat.searchAllChannels') }}</option>
              <option v-for="ch in textChannels" :key="ch.id" :value="ch.id">#{{ ch.name }}</option>
              <optgroup v-if="voiceChannels.length" :label="$t('channel.typeVoice')">
                <option v-for="ch in voiceChannels" :key="ch.id" :value="ch.id">{{ ch.name }}</option>
              </optgroup>
            </select>

            <select
              v-model="authorId"
              :aria-label="$t('chat.searchAuthor')"
              class="max-w-[10rem] px-2 py-1 rounded-md border border-mnema-border bg-mnema-surface text-mnema-muted focus:outline-none focus:border-mnema-accent"
              @change="performSearch"
            >
              <option value="">{{ $t('chat.searchAnyAuthor') }}</option>
              <option v-for="m in chatStore.members" :key="m.id" :value="m.id">{{ m.display_name || m.username }}</option>
            </select>

            <button
              v-for="opt in hasHas"
              :key="opt.value"
              type="button"
              :aria-pressed="has === opt.value ? 'true' : 'false'"
              :class="[
                'px-2 py-1 rounded-md border flex items-center gap-1 transition',
                has === opt.value
                  ? 'bg-mnema-accent/10 border-mnema-accent text-mnema-accent'
                  : 'border-mnema-border hover:bg-mnema-hover text-mnema-muted'
              ]"
              @click="toggleHas(opt.value)"
            >
              <component :is="opt.icon" class="w-3 h-3" />
              <span>{{ $t(opt.label) }}</span>
            </button>

            <span class="ml-auto text-mnema-tertiary font-mono">Esc {{ $t('common.close') }}</span>
          </div>

          <!-- Results List -->
          <div id="search-results" ref="listEl" role="listbox" class="flex-1 overflow-y-auto p-2 space-y-1">
            <div v-if="!hasCriteria" class="py-12 text-center text-sm text-mnema-tertiary">
              {{ $t('chat.searchHint') }}
            </div>

            <div v-else-if="failed" class="py-12 text-center text-sm text-mnema-tertiary" role="alert">
              {{ $t('chat.searchFailed') }}
            </div>

            <div v-else-if="!results.length && !isSearching" class="py-12 text-center text-sm text-mnema-tertiary">
              {{ $t('chat.noResults') }}
            </div>

            <button
              v-for="(msg, idx) in results"
              :id="`search-result-${idx}`"
              :key="msg.id"
              type="button"
              role="option"
              :aria-selected="selectedIndex === idx ? 'true' : 'false'"
              :class="[
                'w-full p-3 rounded-lg text-left transition flex items-start gap-3 group',
                selectedIndex === idx ? 'bg-mnema-hover' : 'hover:bg-mnema-hover/60'
              ]"
              @click="handleSelect(msg)"
            >
              <UserAvatar :user="{ username: msg.username, display_name: msg.display_name, avatar_url: msg.avatar_url }" size="sm" class="mt-0.5" />

              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2 mb-1">
                  <span class="font-medium text-sm text-mnema-text truncate">
                    {{ msg.display_name || msg.username }}
                  </span>
                  <span v-if="channelNames.get(msg.channel_id)" class="px-1.5 py-0.5 rounded bg-mnema-surface border border-mnema-border text-xs text-mnema-muted flex items-center gap-1">
                    <Volume2 v-if="channelTypes.get(msg.channel_id) === 'voice'" class="w-2.5 h-2.5 text-mnema-tertiary" />
                    <Hash v-else class="w-2.5 h-2.5 text-mnema-tertiary" />
                    {{ channelNames.get(msg.channel_id) }}
                  </span>
                  <span v-if="msg.parent_id" class="text-xs text-mnema-tertiary flex items-center gap-1">
                    <CornerDownRight class="w-3 h-3" />
                    {{ $t('chat.inThread') }}
                  </span>
                  <span class="text-xs text-mnema-tertiary ml-auto">
                    {{ formatDate(msg.created_at) }}
                  </span>
                </div>

                <p class="text-sm text-mnema-muted line-clamp-2 break-words">
                  {{ msg.content }}
                </p>

                <div v-if="msg.attachments?.length" class="mt-1.5 flex items-center gap-1 text-xs text-mnema-accent">
                  <Paperclip class="w-3 h-3" />
                  <span>{{ msg.attachments.length }} {{ $t('chat.attachment') }}</span>
                </div>
              </div>

              <ArrowRight class="w-4 h-4 text-mnema-tertiary group-hover:text-mnema-text mt-1 opacity-0 group-hover:opacity-100 transition flex-shrink-0" />
            </button>

            <button
              v-if="hasMore"
              type="button"
              :disabled="isLoadingMore"
              class="w-full py-2 rounded-lg text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition flex items-center justify-center gap-2 disabled:opacity-50"
              @click="loadMore"
            >
              <Loader2 v-if="isLoadingMore" class="w-4 h-4 animate-spin" />
              {{ $t('chat.searchMore') }}
            </button>
          </div>
        </div>
      </template>
    </BaseDialog>
  </Teleport>
</template>
