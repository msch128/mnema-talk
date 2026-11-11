<script setup lang="ts">
import type { FilterKey, SearchFilters, SearchLookup } from '../lib/searchQuery'
import type { FilterPopKind } from '../lib/searchSupport'
import { computed, nextTick, onMounted, ref } from 'vue'
import { Check } from '@lucide/vue'
import { AUTHOR_KINDS, HAS_KINDS, isoDay, quoteValue } from '../lib/searchQuery'
import { NEW_HAS_KINDS, filtersUnsupported } from '../lib/searchSupport'
import { valueItems } from '../lib/searchSuggest'

// The small panel behind one filter button: a list to choose from (members,
// channels, kinds), a day picker, or a file name field. It only reports the
// token to write; the query line stays the source of truth.
const props = defineProps<{ kind: FilterPopKind; lookup: SearchLookup; active: SearchFilters }>()
const emit = defineEmits<{ pick: [token: string]; clear: [key: FilterKey]; goto: [kind: FilterPopKind] }>()

const rootEl = ref<HTMLElement | null>(null)
const typed = ref('')
const dateKind = ref<'during' | 'before' | 'after'>('during')
const pickedDay = ref('')

const SEARCHABLE: Partial<Record<FilterPopKind, FilterKey>> = { from: 'from', in: 'in', mentions: 'mentions' }
const key = computed(() => SEARCHABLE[props.kind])
const searchable = computed(() => key.value !== undefined)
const items = computed(() => {
  const k = key.value
  return k ? valueItems(k, props.lookup, typed.value, 8) : []
})
const kinds = computed(() => (props.kind === 'has'
  ? HAS_KINDS.filter(k => !(filtersUnsupported.value && NEW_HAS_KINDS.includes(k)))
  : AUTHOR_KINDS))
const MORE: { kind: FilterPopKind; label: string }[] = [
  { kind: 'mentions', label: 'chat.filterMentions' },
  { kind: 'author', label: 'chat.filterAuthor' },
  { kind: 'filename', label: 'chat.filterFilename' },
]

function choose(filter: FilterKey, value: string) {
  if (props.active[filter] === value) emit('clear', filter)
  else emit('pick', `${filter}:${quoteValue(value)}`)
}

function chooseDay(day: string) {
  if (day) emit('pick', `${dateKind.value}:${day}`)
}

// A date field reports a value as soon as the first year digit is typed (0002-10-28); wait for a real year.
function chooseTypedDay() {
  if (Number(pickedDay.value.slice(0, 4)) >= 1000) chooseDay(pickedDay.value)
}

function applyTyped() {
  const first = items.value[0]
  if (key.value && first) choose(key.value, first.value)
  else if (props.kind === 'filename' && typed.value.trim()) emit('pick', `filename:${quoteValue(typed.value.trim())}`)
}

// Up and Down walk through the field and the choices; Enter in the field picks the first.
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && e.target instanceof HTMLInputElement && e.target.type === 'text') {
    e.preventDefault()
    applyTyped()
    return
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  const stops = [...(rootEl.value?.querySelectorAll<HTMLElement>('input[type="text"], [data-pop-item]') ?? [])]
  const at = stops.indexOf(document.activeElement as HTMLElement)
  const down = e.key === 'ArrowDown'
  // Focus outside the list (kind buttons, date input): Down enters at the top, Up at the bottom.
  const next = at < 0 ? stops[down ? 0 : stops.length - 1] : stops[(at + (down ? 1 : stops.length - 1)) % stops.length]
  if (!next) return
  e.preventDefault()
  next.focus()
}

const rowClass = 'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text focus-visible:bg-mnema-hover focus-visible:text-mnema-text'
const segClass = (on: boolean) => ['flex-1 rounded-sm px-2 py-1 text-xs font-medium transition', on ? 'bg-mnema-accent/20 text-mnema-accent' : 'text-mnema-muted hover:text-mnema-text']

onMounted(() => nextTick(() => {
  rootEl.value?.querySelector<HTMLElement>('input[type="text"], [data-pop-item]')?.focus()
}))
</script>

<template>
  <div
    ref="rootEl"
    data-search-popover
    :data-popover-kind="kind"
    role="group"
    class="w-72 max-w-[calc(100vw-2rem)] rounded-xl border border-mnema-border bg-mnema-raised p-2 shadow-lg"
    @keydown="onKeydown"
  >
    <template v-if="searchable || kind === 'filename'">
      <input
        v-model="typed"
        type="text"
        autocomplete="off"
        data-popover-input
        :aria-label="$t(kind === 'in' ? 'chat.filterPickChannel' : kind === 'filename' ? 'chat.filterFilenamePlaceholder' : 'chat.filterPickMember')"
        :placeholder="$t(kind === 'in' ? 'chat.filterPickChannel' : kind === 'filename' ? 'chat.filterFilenamePlaceholder' : 'chat.filterPickMember')"
        class="mb-1 w-full rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary focus:border-mnema-accent focus:outline-hidden"
      />
      <ul v-if="searchable" class="max-h-56 overflow-y-auto">
        <li v-for="item in items" :key="item.value">
          <button type="button" data-pop-item :data-pop-value="item.value" :class="rowClass" @mousedown.prevent @click="choose(key as FilterKey, item.value)">
            <span class="truncate">{{ item.label }}</span>
            <span v-if="item.detail" class="truncate text-xs text-mnema-tertiary">{{ item.detail }}</span>
            <Check v-if="key && active[key] === item.value" class="ml-auto h-3.5 w-3.5 shrink-0 text-mnema-accent" />
          </button>
        </li>
        <li v-if="!items.length" class="px-2 py-1.5 text-sm text-mnema-tertiary">{{ $t('chat.filterNoMatch') }}</li>
      </ul>
      <button v-else type="button" class="btn btn-primary btn-sm mt-1 w-full" :disabled="!typed.trim()" @click="applyTyped">{{ $t('chat.filterApply') }}</button>
    </template>

    <template v-else-if="kind === 'has' || kind === 'author'">
      <button
        v-for="k in kinds"
        :key="k"
        type="button"
        data-pop-item
        :data-pop-value="k"
        :class="rowClass"
        @mousedown.prevent
        @click="choose(kind === 'has' ? 'has' : 'author', k)"
      >
        <span>{{ $t(kind === 'has' ? `chat.filterHasKinds.${k}` : `chat.filterAuthorKinds.${k}`) }}</span>
        <Check v-if="active[kind === 'has' ? 'has' : 'author'] === k" class="ml-auto h-3.5 w-3.5 shrink-0 text-mnema-accent" />
      </button>
    </template>

    <template v-else-if="kind === 'date'">
      <div class="mb-2 flex rounded-md border border-mnema-border bg-mnema-surface p-0.5" role="radiogroup" :aria-label="$t('chat.filterDate')">
        <button
          v-for="d in (['during', 'before', 'after'] as const)"
          :key="d"
          type="button"
          role="radio"
          :aria-checked="dateKind === d ? 'true' : 'false'"
          :data-date-kind="d"
          :class="segClass(dateKind === d)"
          @click="dateKind = d"
        >{{ $t(`chat.filterDate${d.charAt(0).toUpperCase() + d.slice(1)}`) }}</button>
      </div>
      <button type="button" data-pop-item data-date-today :class="rowClass" @click="chooseDay(isoDay(0))">{{ $t('chat.filterDateToday') }}</button>
      <button type="button" data-pop-item data-date-yesterday :class="rowClass" @click="chooseDay(isoDay(-1))">{{ $t('chat.filterDateYesterday') }}</button>
      <input
        v-model="pickedDay"
        type="date"
        data-date-input
        :aria-label="$t('chat.chooseDate')"
        class="mt-1 w-full rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        @change="chooseTypedDay"
      />
    </template>

    <template v-else>
      <button v-for="m in MORE" :key="m.kind" type="button" data-pop-item :data-more="m.kind" :class="rowClass" @mousedown.prevent @click="emit('goto', m.kind)">{{ $t(m.label) }}</button>
      <button type="button" data-pop-item data-more="pinned" :class="rowClass" @mousedown.prevent @click="choose('is', 'pinned')">
        <span>{{ $t('chat.filterPinned') }}</span>
        <Check v-if="active.is === 'pinned'" class="ml-auto h-3.5 w-3.5 shrink-0 text-mnema-accent" />
      </button>
    </template>
  </div>
</template>
