<script setup lang="ts">
import type { SearchLookup } from '../lib/searchQuery'
import { computed, ref } from 'vue'
import { User, Hash, AtSign, Bot, FileText, Check } from '@lucide/vue'

defineProps<{
  open: boolean
  lookup: SearchLookup
}>()

const emit = defineEmits<{
  select: [token: string]
  close: []
}>()

const dateKind = ref<'during' | 'before' | 'after'>('during')
const pickedDate = ref('')
const rootEl = ref<HTMLElement | null>(null)

const todayStr = computed(() => {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
})

function applyToken(token: string) {
  emit('select', token)
}

function applyDate() {
  if (!pickedDate.value) return
  emit('select', `${dateKind.value}:${pickedDate.value}`)
  pickedDate.value = ''
}

function applyToday() {
  emit('select', `${dateKind.value}:${todayStr.value}`)
}

const HAS_OPTIONS = ['link', 'file', 'image', 'video', 'embed', 'poll'] as const
const AUTHOR_OPTIONS = ['member', 'bot', 'webhook'] as const

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('close')
    return
  }
  if (!['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return

  const buttons = Array.from(rootEl.value?.querySelectorAll<HTMLElement>('button, input') ?? [])
  const currentIndex = buttons.indexOf(document.activeElement as HTMLElement)
  if (currentIndex === -1) {
    buttons[0]?.focus()
    e.preventDefault()
    return
  }

  let nextIndex = currentIndex
  if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
    nextIndex = (currentIndex + 1) % buttons.length
  } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
    nextIndex = (currentIndex - 1 + buttons.length) % buttons.length
  }
  buttons[nextIndex]?.focus()
  e.preventDefault()
}
</script>

<template>
  <div
    v-if="open"
    ref="rootEl"
    data-search-filter-dropdown
    role="region"
    :aria-label="$t('chat.filterOptions')"
    class="absolute left-4 right-4 top-full z-20 mt-1 space-y-3 rounded-lg border border-mnema-border bg-mnema-raised p-3 shadow-xl"
    @keydown="onKeydown"
  >
    <div>
      <div class="mb-1.5 text-xs font-semibold uppercase tracking-wider text-mnema-tertiary">
        {{ $t('chat.filterSuggestionsTitle') }}
      </div>
      <div class="flex flex-wrap gap-1.5">
        <button
          type="button"
          data-filter-chip="from"
          class="inline-flex items-center gap-1.5 rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1 text-xs text-mnema-text transition hover:border-mnema-accent/40 hover:bg-mnema-hover"
          :aria-label="$t('chat.filterFrom')"
          @click="applyToken('from:')"
        >
          <User class="w-3.5 h-3.5 text-mnema-tertiary" />
          <span class="font-medium">{{ $t('chat.filterFrom') }}</span>
          <span class="text-mnema-tertiary">{{ 'from:' }}</span>
        </button>

        <button
          type="button"
          data-filter-chip="in"
          class="inline-flex items-center gap-1.5 rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1 text-xs text-mnema-text transition hover:border-mnema-accent/40 hover:bg-mnema-hover"
          :aria-label="$t('chat.filterIn')"
          @click="applyToken('in:')"
        >
          <Hash class="w-3.5 h-3.5 text-mnema-tertiary" />
          <span class="font-medium">{{ $t('chat.filterIn') }}</span>
          <span class="text-mnema-tertiary">{{ 'in:' }}</span>
        </button>

        <button
          type="button"
          data-filter-chip="mentions"
          class="inline-flex items-center gap-1.5 rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1 text-xs text-mnema-text transition hover:border-mnema-accent/40 hover:bg-mnema-hover"
          :aria-label="$t('chat.filterMentions')"
          @click="applyToken('mentions:')"
        >
          <AtSign class="w-3.5 h-3.5 text-mnema-tertiary" />
          <span class="font-medium">{{ $t('chat.filterMentions') }}</span>
          <span class="text-mnema-tertiary">{{ 'mentions:' }}</span>
        </button>

        <button
          type="button"
          data-filter-chip="filename"
          class="inline-flex items-center gap-1.5 rounded-md border border-mnema-border bg-mnema-surface px-2.5 py-1 text-xs text-mnema-text transition hover:border-mnema-accent/40 hover:bg-mnema-hover"
          :aria-label="$t('chat.filterFilename')"
          @click="applyToken('filename:')"
        >
          <FileText class="w-3.5 h-3.5 text-mnema-tertiary" />
          <span class="font-medium">{{ $t('chat.filterFilename') }}</span>
          <span class="text-mnema-tertiary">{{ 'filename:' }}</span>
        </button>
      </div>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-semibold text-mnema-muted">
        {{ $t('chat.filterHas') }} {{ '(has:)' }}
      </div>
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="kind in HAS_OPTIONS"
          :key="kind"
          type="button"
          :data-filter-has="kind"
          class="rounded-md border border-mnema-border/80 bg-mnema-surface/60 px-2 py-0.5 text-xs text-mnema-muted transition hover:border-mnema-accent/40 hover:bg-mnema-hover hover:text-mnema-text"
          :aria-label="`${$t('chat.filterHas')} ${kind}`"
          @click="applyToken(`has:${kind}`)"
        >
          {{ 'has:' + kind }}
        </button>
      </div>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-semibold text-mnema-muted">
        {{ $t('chat.filterAuthor') }} {{ '(author:)' }}
      </div>
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="kind in AUTHOR_OPTIONS"
          :key="kind"
          type="button"
          :data-filter-author="kind"
          class="inline-flex items-center gap-1 rounded-md border border-mnema-border/80 bg-mnema-surface/60 px-2 py-0.5 text-xs text-mnema-muted transition hover:border-mnema-accent/40 hover:bg-mnema-hover hover:text-mnema-text"
          :aria-label="`${$t('chat.filterAuthor')} ${kind}`"
          @click="applyToken(`author:${kind}`)"
        >
          <Bot v-if="kind === 'bot'" class="w-3 h-3 text-mnema-tertiary" />
          <span>{{ 'author:' + kind }}</span>
        </button>
      </div>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-semibold text-mnema-muted">
        {{ $t('chat.filterDate') }}
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="flex rounded-md border border-mnema-border bg-mnema-surface p-0.5" role="radiogroup" :aria-label="$t('chat.filterDate')">
          <button
            v-for="d in (['during', 'before', 'after'] as const)"
            :key="d"
            type="button"
            role="radio"
            :aria-checked="dateKind === d ? 'true' : 'false'"
            :data-date-kind="d"
            :class="[
              'rounded-sm px-2 py-0.5 text-xs font-medium transition',
              dateKind === d ? 'bg-mnema-accent/20 text-mnema-accent' : 'text-mnema-muted hover:text-mnema-text'
            ]"
            @click="dateKind = d"
          >
            {{ $t(`chat.filterDate${d.charAt(0).toUpperCase() + d.slice(1)}`) }}
          </button>
        </div>

        <input
          v-model="pickedDate"
          type="date"
          data-date-input
          :aria-label="$t('chat.chooseDate')"
          class="rounded-md border border-mnema-border bg-mnema-surface px-2 py-0.5 text-xs text-mnema-text focus:border-mnema-accent focus:outline-hidden"
          @change="applyDate"
        />

        <button
          type="button"
          data-date-today
          class="rounded-md border border-mnema-border bg-mnema-surface px-2 py-0.5 text-xs text-mnema-muted transition hover:bg-mnema-hover hover:text-mnema-text"
          :aria-label="$t('chat.filterDateToday')"
          @click="applyToday"
        >
          {{ $t('chat.filterDateToday') }}
        </button>

        <button
          v-if="pickedDate"
          type="button"
          data-date-apply
          class="inline-flex items-center gap-1 rounded-md bg-mnema-accent px-2 py-0.5 text-xs font-medium text-mnema-text-contrast transition hover:bg-mnema-accent-hover"
          :aria-label="$t('chat.applyDate')"
          @click="applyDate"
        >
          <Check class="w-3 h-3" />
          {{ $t('chat.applyDate') }}
        </button>
      </div>
    </div>
  </div>
</template>
