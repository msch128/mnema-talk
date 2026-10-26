<script setup lang="ts">
import type { FilterKey, SearchFilters, SearchLookup } from '../lib/searchQuery'
import type { SearchSuggestion } from '../lib/searchSuggest'
import { computed, ref, watch } from 'vue'
import { Search, X } from '@lucide/vue'
import { FILTER_KEYS, serializeSearchQuery, splitCommitted } from '../lib/searchQuery'
import { filtersUnsupported } from '../lib/searchSupport'
import { applySuggestion, suggestFor } from '../lib/searchSuggest'
import type { SearchScope } from '../lib/globalSearch'
import SearchFilterRow from './SearchFilterRow.vue'
import SearchScopeToggle from './SearchScopeToggle.vue'

// The query line with its filter chips, the filter shortcuts and the
// autocomplete of the filter token being typed. The text is the source of
// truth: chips only show what the parser read from it.
const props = defineProps<{
  modelValue: string
  lookup: SearchLookup
  loading: boolean
  /** Filters whose name matched no member or channel. */
  unresolved: FilterKey[]
  /** id of the highlighted result, for aria-activedescendant. */
  activeResultId?: string | undefined
  v3Supported?: boolean
  showScope?: boolean | undefined
  scope?: SearchScope | undefined
}>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; close: []; 'update:scope': [scope: SearchScope] }>()

const inputEl = ref<HTMLInputElement | null>(null)
const rowRef = ref<InstanceType<typeof SearchFilterRow> | null>(null)
const suggestIndex = ref(0)
const dismissed = ref(false)
const popoverOpen = ref(false)
// What is typed in the field: the line without its finished filters (those are chips).
const draft = ref(splitCommitted(props.modelValue).rest)
let emitted = props.modelValue

const committed = computed(() => splitCommitted(props.modelValue).filters)
const chips = computed(() => FILTER_KEYS.flatMap(key => {
  const value = committed.value[key]
  return value ? [{ key, value, bad: props.unresolved.includes(key) }] : []
}))
const suggestions = computed(() => (dismissed.value || popoverOpen.value ? null : suggestFor(draft.value, props.lookup)))
const activeDescendant = computed(() => (suggestions.value ? `search-suggestion-${suggestIndex.value}` : props.activeResultId))

// A change from outside (prefill, restored session query) replaces the field.
watch(() => props.modelValue, (v) => {
  if (v !== emitted) draft.value = splitCommitted(v).rest
  suggestIndex.value = 0
  dismissed.value = false
})

function line(filters: SearchFilters, text: string): string {
  const head = serializeSearchQuery({ text: '', filters })
  return head ? `${head} ${text}` : text
}

/** Takes finished filter tokens out of the field into chips and emits the whole line. */
function update(text: string, base: SearchFilters = committed.value) {
  const split = splitCommitted(text)
  const filters = { ...base, ...split.filters }
  draft.value = split.rest
  emitted = line(filters, split.rest)
  emit('update:modelValue', emitted)
}

function onInput(e: Event) {
  if (e.target instanceof HTMLInputElement) update(e.target.value)
}

function removeChip(key: FilterKey) {
  const filters = { ...committed.value }
  delete filters[key]
  update(draft.value, filters)
}

function pick(item: SearchSuggestion) {
  const ctx = suggestions.value
  if (!ctx) return
  update(applySuggestion(draft.value, ctx, item))
  inputEl.value?.focus()
}

function addShortcut(key: FilterKey) {
  const base = draft.value
  const sep = base && !/\s$/.test(base) ? ' ' : ''
  update(`${base}${sep}${key}:`)
  inputEl.value?.focus()
}

function onSelectFilter(token: string) {
  if (token.endsWith(':')) {
    addShortcut(token.slice(0, -1) as FilterKey)
  } else {
    const base = draft.value
    const sep = base && !/\s$/.test(base) ? ' ' : ''
    update(`${base}${sep}${token} `)
  }
  inputEl.value?.focus()
}

// Keys the suggestion list owns while it is open; everything else bubbles on
// to the dialog (result navigation, Enter on the selected result).
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Backspace' && draft.value === '' && chips.value.length > 0) {
    const last = chips.value[chips.value.length - 1]
    if (last) removeChip(last.key)
    return
  }
  const ctx = suggestions.value
  if (!ctx) return
  const last = ctx.items.length - 1
  if (e.key === 'ArrowDown') suggestIndex.value = suggestIndex.value >= last ? 0 : suggestIndex.value + 1
  else if (e.key === 'ArrowUp') suggestIndex.value = suggestIndex.value <= 0 ? last : suggestIndex.value - 1
  else if (e.key === 'Enter' || e.key === 'Tab') {
    const item = ctx.items[suggestIndex.value]
    if (!item) return
    pick(item)
  } else return
  e.preventDefault()
  e.stopPropagation()
}

/** Puts away the topmost layer (filter popover, then suggestions); false when there is none. */
function dismissLayer(): boolean {
  if (rowRef.value?.close(true)) return true
  if (!suggestions.value) return false
  dismissed.value = true
  return true
}

defineExpose({ focus: () => inputEl.value?.focus(), dismissLayer })
</script>

<template>
  <div class="shrink-0 border-b border-mnema-border bg-mnema-surface/50">
    <div class="relative flex items-center gap-3 px-4 py-3">
      <Search class="w-5 h-5 text-mnema-tertiary shrink-0" />
      <div class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
        <span
          v-for="chip in chips"
          :key="chip.key"
          data-search-chip
          :class="[
            'inline-flex max-w-56 items-center gap-1 rounded-md border py-0.5 pl-2 pr-1 text-xs',
            chip.bad ? 'border-mnema-danger text-mnema-danger' : 'border-mnema-accent/50 bg-mnema-accent/10 text-mnema-accent'
          ]"
          :title="chip.bad ? $t('chat.searchUnknownFilter') : undefined"
        >
          <span class="truncate font-mono">{{ chip.key }}:{{ chip.value }}</span>
          <button
            type="button"
            class="btn btn-icon btn-sm"
            :aria-label="$t('chat.searchRemoveFilter', { filter: chip.key })"
            @mousedown.prevent
            @click="removeChip(chip.key)"
          >
            <X class="w-3 h-3" />
          </button>
        </span>
        <input
          ref="inputEl"
          data-search-input
          data-bare
          :value="draft"
          type="text"
          role="combobox"
          autocomplete="off"
          aria-expanded="true"
          :aria-controls="suggestions ? 'search-suggestions' : 'search-results'"
          :aria-activedescendant="activeDescendant"
          :placeholder="$t('chat.searchPlaceholder')"
          :aria-label="$t('chat.search')"
          class="min-w-32 flex-1 bg-transparent text-base text-mnema-text placeholder-mnema-tertiary focus:outline-hidden"
          @input="onInput"
          @keydown="onKeydown"
        />
      </div>
      <button
        type="button"
        data-dialog-close
        tabindex="-1"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        v-tooltip="$t('common.close')"
        :aria-label="$t('common.close')"
        @mousedown.prevent
        @click="emit('close')"
      >
        <X class="w-4 h-4" />
      </button>

      <div v-if="loading" class="absolute inset-x-0 bottom-0 h-0.5 animate-pulse bg-mnema-accent/60" role="progressbar" :aria-label="$t('chat.searchSearching')" data-search-progress></div>

      <ul
        v-if="suggestions"
        id="search-suggestions"
        role="listbox"
        data-search-suggestions
        class="absolute inset-x-4 top-full z-30 mt-1 max-h-56 overflow-y-auto rounded-lg border border-mnema-border bg-mnema-raised p-1 shadow-lg"
      >
        <li
          v-for="(item, i) in suggestions.items"
          :id="`search-suggestion-${i}`"
          :key="item.value"
          role="option"
          :aria-selected="i === suggestIndex ? 'true' : 'false'"
          :class="['flex cursor-pointer items-baseline gap-2 rounded-md px-2 py-1.5 text-sm', i === suggestIndex ? 'bg-mnema-hover text-mnema-text' : 'text-mnema-muted hover:bg-mnema-hover']"
          @mousedown.prevent="pick(item)"
        >
          <span class="truncate">{{ item.label }}</span>
          <span v-if="item.detail" class="truncate text-xs text-mnema-tertiary">{{ item.detail }}</span>
        </li>
      </ul>
    </div>

    <SearchFilterRow
      v-if="v3Supported"
      ref="rowRef"
      :lookup="lookup"
      :active="committed"
      :show-scope="showScope"
      :scope="scope"
      @pick="onSelectFilter"
      @clear="removeChip"
      @update:scope="emit('update:scope', $event)"
      @open="popoverOpen = $event"
    />
    <div v-else class="flex flex-wrap items-center gap-1.5 border-t border-mnema-hairline bg-mnema-raised px-4 py-2 text-xs text-mnema-muted">
      <SearchScopeToggle v-if="showScope" :scope="scope" @update:scope="emit('update:scope', $event)" />
      <span>{{ $t('chat.filter') }}:</span>
      <button
        v-for="key in FILTER_KEYS.filter(k => !(k === 'is' && filtersUnsupported))"
        :key="key"
        type="button"
        data-search-shortcut
        class="rounded-md border border-mnema-border px-2 py-0.5 font-mono transition hover:bg-mnema-hover"
        @click="addShortcut(key)"
      >{{ key }}:</button>
      <span class="ml-auto font-mono text-mnema-tertiary max-md:hidden">Esc {{ $t('common.close') }}</span>
    </div>
  </div>
</template>
