<script setup lang="ts">
import type { FilterKey, SearchFilters, SearchLookup } from '../lib/searchQuery'
import type { FilterPopKind } from '../lib/searchSupport'
import type { SearchScope } from '../lib/globalSearch'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ChevronDown } from '@lucide/vue'
import SearchFilterPopover from './SearchFilterPopover.vue'
import SearchScopeToggle from './SearchScopeToggle.vue'

// One calm line under the query: a button per filter that opens a small
// popover, plus the where-to-search choice. It never changes the dialog's
// height. The buttons are one tab stop; the arrow keys move between them.
const props = defineProps<{
  lookup: SearchLookup
  active: SearchFilters
  /** Offer the channels / direct messages choice. */
  showScope?: boolean | undefined
  scope?: SearchScope | undefined
}>()
const emit = defineEmits<{ pick: [token: string]; clear: [key: FilterKey]; 'update:scope': [scope: SearchScope]; open: [open: boolean] }>()

interface RowButton { kind: FilterPopKind; label: string; keys: FilterKey[] }
const BUTTONS: RowButton[] = [
  { kind: 'from', label: 'chat.filterFrom', keys: ['from'] },
  { kind: 'in', label: 'chat.filterIn', keys: ['in'] },
  { kind: 'has', label: 'chat.filterHas', keys: ['has'] },
  { kind: 'date', label: 'chat.filterDate', keys: ['before', 'after', 'during'] },
  { kind: 'more', label: 'chat.filterMore', keys: ['mentions', 'author', 'filename', 'is'] },
]
// The "more" menu leads to these; they stay highlighted on the "more" button.
const MORE_KINDS: FilterPopKind[] = ['more', 'mentions', 'author', 'filename']

const open = ref<FilterPopKind | null>(null)
const popLeft = ref(0)
const rootEl = ref<HTMLElement | null>(null)
const scrollEl = ref<HTMLElement | null>(null)
const stop = ref(0)

const popOwner = computed(() => (open.value && MORE_KINDS.includes(open.value) ? 'more' : open.value))
const isActive = (b: RowButton) => b.keys.some(k => props.active[k] !== undefined)

function buttons(): HTMLElement[] {
  return [...(scrollEl.value?.querySelectorAll<HTMLElement>('[data-filter-button]') ?? [])]
}

function place(kind: FilterPopKind) {
  const btn = buttons().find(b => b.dataset.filterButton === (MORE_KINDS.includes(kind) ? 'more' : kind))
  const width = rootEl.value?.clientWidth ?? 0
  const left = btn ? btn.getBoundingClientRect().left - (rootEl.value?.getBoundingClientRect().left ?? 0) : 0
  popLeft.value = Math.max(8, Math.min(left, width - 288 - 8))
}

function toggle(kind: FilterPopKind) {
  if (open.value && popOwner.value === kind) { close(true); return }
  place(kind)
  open.value = kind
}

/** Closes the popover; true when there was one (the dialog then keeps itself open). */
function close(refocus = false): boolean {
  const was = open.value
  open.value = null
  if (was && refocus) {
    const kind = MORE_KINDS.includes(was) ? 'more' : was
    nextTick(() => buttons().find(b => b.dataset.filterButton === kind)?.focus())
  }
  return !!was
}

function pick(token: string) {
  emit('pick', token)
  open.value = null
}

function clear(key: FilterKey) {
  emit('clear', key)
  open.value = null
}

function goto(kind: FilterPopKind) {
  place('more')
  open.value = kind
}

// A press anywhere else in the dialog puts the popover away.
function onOutside(e: PointerEvent) {
  if (e.target instanceof Node && !rootEl.value?.contains(e.target)) open.value = null
}
watch(open, (now, was) => {
  emit('open', now !== null)
  if (now && !was) document.addEventListener('pointerdown', onOutside, true)
  else if (!now) document.removeEventListener('pointerdown', onOutside, true)
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onOutside, true))

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
  const all = buttons()
  const at = all.indexOf(document.activeElement as HTMLElement)
  if (at === -1) return
  e.preventDefault()
  const next = all[(at + (e.key === 'ArrowRight' ? 1 : all.length - 1)) % all.length]
  next?.focus()
}

const chip = (on: boolean, selected = false) => [
  'inline-flex shrink-0 items-center gap-1 rounded-full border px-3 py-1 text-xs font-medium transition max-md:min-h-9',
  on || selected ? 'border-mnema-accent/40 bg-mnema-accent/10 text-mnema-accent' : 'border-mnema-border text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text',
]

defineExpose({ close, isOpen: () => open.value !== null })
</script>

<template>
  <div ref="rootEl" class="relative border-b border-mnema-hairline" data-search-filter-row>
    <div
      ref="scrollEl"
      role="toolbar"
      :aria-label="$t('chat.filterSuggestionsTitle')"
      class="flex items-center gap-1.5 overflow-x-auto px-4 py-2 [scrollbar-width:none]"
      @keydown="onKeydown"
    >
      <SearchScopeToggle v-if="showScope" class="mr-1" :scope="scope" @update:scope="emit('update:scope', $event)" />
      <button
        v-for="(b, i) in BUTTONS"
        :key="b.kind"
        type="button"
        :data-filter-button="b.kind"
        :data-filter-active="isActive(b) ? 'true' : undefined"
        :aria-haspopup="'true'"
        :aria-expanded="popOwner === b.kind ? 'true' : 'false'"
        :tabindex="stop === i ? 0 : -1"
        :class="chip(isActive(b), popOwner === b.kind)"
        @mousedown.prevent
        @focus="stop = i"
        @click="toggle(b.kind)"
      >
        {{ $t(b.label) }}
        <ChevronDown class="h-3 w-3 opacity-60" aria-hidden="true" />
      </button>
    </div>

    <div v-if="open" class="absolute top-full z-20 mt-1" :style="{ left: `${popLeft}px` }">
      <SearchFilterPopover :key="open" :kind="open" :lookup="lookup" :active="active" @pick="pick" @clear="clear" @goto="goto" />
    </div>
  </div>
</template>
