<script setup>
import { ref, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { Search, Hash, Paperclip, X, Loader2, ArrowRight } from '@lucide/vue'
import { api } from '../lib/api'
import { useChatStore } from '../stores/chat'
import UserAvatar from './UserAvatar.vue'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue', 'select-message'])

const chatStore = useChatStore()
const query = ref('')
const results = ref([])
const isSearching = ref(false)
const filterCurrentChannel = ref(false)
const filterHasAttachment = ref(false)
const selectedIndex = ref(0)
const searchInput = ref(null)

let searchTimeout = null

function close() {
  emit('update:modelValue', false)
}

async function performSearch() {
  const q = query.value.trim()
  if (!q) {
    results.value = []
    isSearching.value = false
    return
  }

  isSearching.value = true
  try {
    const params = new URLSearchParams({ q, limit: '25' })
    if (filterCurrentChannel.value && chatStore.activeChannel?.id) {
      params.set('channel_id', chatStore.activeChannel.id)
    }
    if (filterHasAttachment.value) {
      params.set('has_attachment', 'true')
    }

    const data = await api(`/api/search?${params.toString()}`)
    results.value = data.messages || []
    selectedIndex.value = 0
  } catch (err) {
    console.warn('Search error:', err)
    results.value = []
  } finally {
    isSearching.value = false
  }
}

function onInput() {
  clearTimeout(searchTimeout)
  searchTimeout = setTimeout(performSearch, 250)
}

function getChannelName(channelId) {
  for (const cat of chatStore.categories) {
    for (const ch of cat.channels || []) {
      if (ch.id === channelId) return ch.name
    }
  }
  for (const ch of chatStore.uncategorized) {
    if (ch.id === channelId) return ch.name
  }
  return ''
}

function handleSelect(msg) {
  emit('select-message', msg)
  close()
}

function handleKeydown(e) {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
    return
  }
  if (!results.value.length) return

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value + 1) % results.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = (selectedIndex.value - 1 + results.value.length) % results.value.length
  } else if (e.key === 'Enter') {
    e.preventDefault()
    if (selectedIndex.value >= 0 && selectedIndex.value < results.value.length) {
      handleSelect(results.value[selectedIndex.value])
    }
  }
}

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

watch(() => props.modelValue, (open) => {
  if (open) {
    selectedIndex.value = 0
    nextTick(() => {
      searchInput.value?.focus()
      if (query.value.trim()) performSearch()
    })
  } else {
    clearTimeout(searchTimeout)
  }
})

function handleGlobalKeydown(e) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    emit('update:modelValue', !props.modelValue)
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleGlobalKeydown)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleGlobalKeydown)
  clearTimeout(searchTimeout)
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="fixed inset-0 z-50 flex items-start justify-center pt-20 px-4 bg-black/60 backdrop-blur-sm animate-in fade-in duration-100"
      @click.self="close"
    >
      <div
        role="dialog"
        aria-modal="true"
        :aria-label="$t('chat.search')"
        class="w-full max-w-2xl bg-mnema-elevated border border-mnema-border rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[75vh]"
        @keydown="handleKeydown"
      >
        <!-- Search Input Bar -->
        <div class="flex items-center gap-3 px-4 py-3.5 border-b border-mnema-border bg-mnema-surface/50">
          <Search class="w-5 h-5 text-mnema-tertiary flex-shrink-0" />
          <input
            ref="searchInput"
            v-model="query"
            type="text"
            :placeholder="$t('chat.searchPlaceholder')"
            class="flex-1 bg-transparent text-base text-mnema-text placeholder-mnema-tertiary focus:outline-none"
            @input="onInput"
          />
          <Loader2 v-if="isSearching" class="w-4 h-4 animate-spin text-mnema-accent flex-shrink-0" />
          <button
            type="button"
            class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
            @click="close"
          >
            <X class="w-4 h-4" />
          </button>
        </div>

        <!-- Filter Badges -->
        <div class="flex items-center gap-2 px-4 py-2 border-b border-mnema-hairline bg-mnema-raised text-xs text-mnema-muted">
          <span>{{ $t('chat.filter') }}:</span>
          <button
            type="button"
            :class="[
              'px-2 py-1 rounded-md border flex items-center gap-1 transition',
              filterCurrentChannel
                ? 'bg-mnema-accent/10 border-mnema-accent text-mnema-accent'
                : 'border-mnema-border hover:bg-mnema-hover text-mnema-muted'
            ]"
            @click="filterCurrentChannel = !filterCurrentChannel; performSearch()"
          >
            <Hash class="w-3 h-3" />
            <span>{{ chatStore.activeChannel?.name ? `#${chatStore.activeChannel.name}` : $t('chat.currentChannel') }}</span>
          </button>

          <button
            type="button"
            :class="[
              'px-2 py-1 rounded-md border flex items-center gap-1 transition',
              filterHasAttachment
                ? 'bg-mnema-accent/10 border-mnema-accent text-mnema-accent'
                : 'border-mnema-border hover:bg-mnema-hover text-mnema-muted'
            ]"
            @click="filterHasAttachment = !filterHasAttachment; performSearch()"
          >
            <Paperclip class="w-3 h-3" />
            <span>{{ $t('chat.hasAttachment') }}</span>
          </button>

          <span class="ml-auto text-mnema-tertiary font-mono">Esc {{ $t('common.close') }}</span>
        </div>

        <!-- Results List -->
        <div class="flex-1 overflow-y-auto p-2 space-y-1">
          <div v-if="!query.trim()" class="py-12 text-center text-sm text-mnema-tertiary">
            {{ $t('chat.searchHint') }}
          </div>

          <div v-else-if="!results.length && !isSearching" class="py-12 text-center text-sm text-mnema-tertiary">
            {{ $t('chat.noResults') }}
          </div>

          <button
            v-for="(msg, idx) in results"
            :key="msg.id"
            type="button"
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
                <span v-if="getChannelName(msg.channel_id)" class="px-1.5 py-0.5 rounded bg-mnema-surface border border-mnema-border text-xs text-mnema-muted flex items-center gap-1">
                  <Hash class="w-2.5 h-2.5 text-mnema-tertiary" />
                  {{ getChannelName(msg.channel_id) }}
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
        </div>
      </div>
    </div>
  </Teleport>
</template>
