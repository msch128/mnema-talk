<script setup lang="ts">
// Searching the wiki: titles and text, titles first, with the words around the
// match. The query comes from the box in the dialog's header.
import { onBeforeUnmount, ref, watch } from 'vue'
import { searchWiki, type WikiSearchHit } from '../lib/wikiApi'
import { goWiki, wikiQuery } from '../lib/wikiNav'

const DEBOUNCE_MS = 250
const hits = ref<WikiSearchHit[]>([])
const state = ref<'idle' | 'loading' | 'done' | 'failed'>('idle')
let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0

async function run(query: string) {
  const mine = ++seq
  state.value = 'loading'
  try {
    const found = await searchWiki(query)
    if (mine !== seq) return
    hits.value = found
    state.value = 'done'
  } catch {
    if (mine === seq) state.value = 'failed'
  }
}

watch(wikiQuery, query => {
  clearTimeout(timer)
  if (query.trim().length < 3) {
    seq++
    hits.value = []
    state.value = 'idle'
    return
  }
  timer = setTimeout(() => { void run(query.trim()) }, DEBOUNCE_MS)
}, { immediate: true })

onBeforeUnmount(() => { clearTimeout(timer); seq++ })
</script>

<template>
  <div class="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-6 max-md:px-4" data-testid="wiki-search">
    <h1 class="text-xl font-semibold text-mnema-text">{{ $t('wiki.searchTitle') }}</h1>
    <p v-if="state === 'idle'" class="text-sm text-mnema-tertiary">{{ $t('wiki.searchHint') }}</p>
    <p v-else-if="state === 'loading'" class="text-sm text-mnema-tertiary" role="status">{{ $t('wiki.searching') }}</p>
    <p v-else-if="state === 'failed'" class="text-sm text-mnema-danger" role="alert">{{ $t('wiki.searchFailed') }}</p>
    <p v-else-if="hits.length === 0" class="text-sm text-mnema-tertiary" role="status" data-testid="wiki-no-hits">{{ $t('wiki.noHits', { query: wikiQuery.trim() }) }}</p>
    <ul v-else class="flex flex-col gap-2" role="list">
      <li v-for="hit in hits" :key="hit.slug">
        <button type="button" class="flex w-full flex-col gap-0.5 rounded-lg border border-mnema-border px-4 py-3 text-left transition-colors hover:bg-mnema-hover focus-ring" data-testid="wiki-hit" @click="goWiki({ mode: 'page', slug: hit.slug })">
          <span class="font-medium text-mnema-text">{{ hit.title }}</span>
          <span class="text-sm text-mnema-muted wrap-break-word">{{ hit.snippet }}</span>
        </button>
      </li>
    </ul>
  </div>
</template>
