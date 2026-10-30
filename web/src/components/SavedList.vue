<script setup lang="ts">
import type { ChatSavedMessage } from '../types/rest'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { Bookmark, Hash, Loader2, MessageCircle, Search, Volume2 } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useSavedStore } from '../stores/saved'
import { dmTitle } from '../lib/dm'
import { groupSaved, matchesSaved, type SavedContext, type SavedSort } from '../lib/savedGroups'
import SavedEntry from './SavedEntry.vue'
import StatePanel from './StatePanel.vue'

// The member's saved messages: newest saved first or grouped by channel, with
// a search over what is loaded. Opening the list reloads it; an entry can be
// asked to scroll into view (the toast after saving). A click jumps to the message.
const SORT_KEY = 'mnema_saved_sort'
const props = defineProps<{ focusId?: string | undefined }>()
const emit = defineEmits<{ navigate: [] }>()

const chatStore = useChatStore()
const auth = useAuthStore()
const saved = useSavedStore()
const root = ref<HTMLElement | null>(null)
const query = ref('')
const flashId = ref<string | null>(null)

function storedSort(): SavedSort {
  try { return localStorage.getItem(SORT_KEY) === 'channel' ? 'channel' : 'recent' } catch { return 'recent' }
}
const sort = ref<SavedSort>(storedSort())
function setSort(next: SavedSort) {
  sort.value = next
  try { localStorage.setItem(SORT_KEY, next) } catch { /* storage blocked: the choice lasts for this session */ }
}

const contexts = computed(() => {
  const byId = new Map<string, SavedContext>()
  for (const c of chatStore.allChannels) byId.set(c.id, { name: c.name, dm: false, voice: c.type === 'voice' })
  for (const dm of chatStore.dms) byId.set(dm.id, { name: dmTitle(dm, auth.user?.id), dm: true, voice: false })
  return byId
})
const contextOf = (channelId: string) => contexts.value.get(channelId)

const shown = computed(() => saved.items.filter(e => matchesSaved(e, query.value, contextOf)))
const groups = computed(() => groupSaved(shown.value, sort.value, contextOf))
const searching = computed(() => query.value.trim().length > 0)

onMounted(() => { void saved.load() })

function pick(entry: ChatSavedMessage) {
  if (!entry.message) return
  emit('navigate')
  chatStore.goToMessage(entry.message)
}

// Scrolls to the entry the toast asked for, once the list has it; asked for once, so a later reload or search is left alone.
const wanted = ref(props.focusId)
watch([wanted, () => saved.items, () => saved.loading], async ([id]) => {
  if (!id || saved.loading || !saved.items.some(e => e.message_id === id)) return
  query.value = ''
  await nextTick()
  const row = [...(root.value?.querySelectorAll<HTMLElement>('[data-message-id]') ?? [])].find(el => el.dataset['messageId'] === id)
  if (!row) return
  row.scrollIntoView?.({ block: 'nearest' })
  flashId.value = id
  wanted.value = undefined
}, { immediate: true })

const sorts: { id: SavedSort; label: string }[] = [
  { id: 'recent', label: 'saved.sortRecent' },
  { id: 'channel', label: 'saved.sortChannel' }
]
</script>

<template>
  <div ref="root" data-testid="saved-list">
    <StatePanel v-if="saved.loading && !saved.items.length" state="loading" />
    <StatePanel v-else-if="saved.failed && !saved.items.length" state="error" retryable @retry="saved.load()">{{ $t('saved.failed') }}</StatePanel>
    <div v-else-if="!saved.items.length" data-testid="saved-empty" class="flex flex-col items-center gap-2 px-5 py-8 text-center">
      <Bookmark class="h-5 w-5 text-mnema-tertiary" aria-hidden="true" />
      <p class="text-sm font-medium text-mnema-text">{{ $t('saved.emptyTitle') }}</p>
      <p class="max-w-[32ch] text-balance text-sm text-mnema-tertiary">{{ $t('saved.emptyBody') }}</p>
      <p class="max-w-[32ch] text-balance text-xs text-mnema-tertiary">{{ $t('saved.emptyExample') }}</p>
      <p class="max-w-[32ch] text-balance text-xs text-mnema-tertiary">{{ $t('saved.emptyHow') }}</p>
    </div>
    <template v-else>
      <div class="flex items-center gap-1.5 px-1 pb-1.5 pt-0.5">
        <label class="relative min-w-0 flex-1">
          <Search class="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-mnema-tertiary" aria-hidden="true" />
          <input
            v-model="query"
            data-testid="saved-search"
            type="search"
            :aria-label="$t('saved.search')"
            :placeholder="$t('saved.searchPlaceholder')"
            class="w-full rounded-md border border-mnema-border bg-mnema-surface py-1 pl-7 pr-2 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
          >
        </label>
        <div role="group" :aria-label="$t('saved.sortLabel')" class="flex shrink-0 rounded-md border border-mnema-border p-0.5">
          <button
            v-for="option in sorts"
            :key="option.id"
            type="button"
            :data-testid="`saved-sort-${option.id}`"
            :aria-pressed="sort === option.id ? 'true' : 'false'"
            :class="['rounded-sm px-2 py-0.5 text-xs transition max-md:py-2', sort === option.id ? 'bg-mnema-surface text-mnema-text' : 'text-mnema-tertiary hover:text-mnema-text']"
            @click="setSort(option.id)"
          >{{ $t(option.label) }}</button>
        </div>
      </div>

      <p v-if="!groups.length" data-testid="saved-no-match" class="px-4 py-6 text-center text-sm text-mnema-tertiary">{{ $t('saved.noMatches', { query: query.trim() }) }}</p>
      <section v-for="group in groups" :key="group.key" data-testid="saved-group" :aria-label="group.context?.name || undefined">
        <h3 v-if="group.gone" class="px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('saved.groupGone') }}</h3>
        <h3 v-else-if="group.context" class="flex items-center gap-1.5 px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">
          <MessageCircle v-if="group.context.dm" class="h-3 w-3" aria-hidden="true" />
          <Volume2 v-else-if="group.context.voice" class="h-3 w-3" aria-hidden="true" />
          <Hash v-else class="h-3 w-3" aria-hidden="true" />
          <span class="truncate">{{ group.context.name || $t('saved.groupOther') }}</span>
        </h3>
        <SavedEntry
          v-for="entry in group.entries"
          :key="entry.message_id"
          :entry="entry"
          :context="group.context ? null : (entry.message ? contextOf(entry.message.channel_id) ?? null : null)"
          :flash="flashId === entry.message_id"
          @open="pick(entry)"
        />
      </section>
      <p v-if="searching && saved.hasMore" class="px-3 pb-1 text-center text-xs text-mnema-tertiary">{{ $t('saved.searchLoaded') }}</p>
      <button
        v-if="saved.hasMore && !saved.loading"
        type="button"
        data-testid="saved-more"
        :disabled="saved.loadingMore"
        class="btn btn-ghost flex w-full items-center justify-center gap-2"
        @click="saved.loadMore()"
      >
        <Loader2 v-if="saved.loadingMore" class="h-3.5 w-3.5 animate-spin" />
        {{ $t('chat.searchMore') }}
      </button>
    </template>
  </div>
</template>
