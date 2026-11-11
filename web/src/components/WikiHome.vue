<script setup lang="ts">
// The wiki's start: the top pages and what changed lately, or, in an empty
// wiki, the way to the first page.
import { FilePlus2 } from '@lucide/vue'
import { useWikiStore } from '../stores/wiki'
import { goWiki } from '../lib/wikiNav'
import { relative } from '../shared/lib/time'
import { locale } from '../i18n'

const wiki = useWikiStore()
</script>

<template>
  <div class="mx-auto flex max-w-3xl flex-col gap-6 px-6 py-6 max-md:px-4" data-testid="wiki-start">
    <header>
      <h1 class="text-2xl font-semibold text-mnema-text">{{ $t('wiki.title') }}</h1>
      <p class="mt-1 text-sm text-mnema-tertiary">{{ $t('wiki.intro') }}</p>
    </header>

    <div v-if="wiki.treeLoaded && wiki.live.length === 0" class="rounded-lg border border-dashed border-mnema-border px-6 py-10 text-center" data-testid="wiki-start-empty">
      <p class="text-sm text-mnema-muted">{{ $t(wiki.canEdit ? 'wiki.emptyEditor' : 'wiki.emptyReader') }}</p>
      <button v-if="wiki.canEdit" type="button" class="btn btn-primary mt-4" data-testid="wiki-first-page" @click="goWiki({ mode: 'new' })"><FilePlus2 class="h-4 w-4" aria-hidden="true" />{{ $t('wiki.firstPage') }}</button>
    </div>

    <template v-else>
      <section v-if="wiki.tree.length" :aria-label="$t('wiki.topPages')">
        <h2 class="mb-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('wiki.topPages') }}</h2>
        <ul class="grid grid-cols-2 gap-2 max-md:grid-cols-1">
          <li v-for="node in wiki.tree" :key="node.id">
            <button type="button" class="flex w-full flex-col rounded-lg border border-mnema-border bg-mnema-elevated/60 px-4 py-3 text-left transition-colors hover:bg-mnema-hover focus-ring" @click="goWiki({ mode: 'page', slug: node.slug })">
              <span class="truncate font-medium text-mnema-text">{{ node.title }}</span>
              <span v-if="node.children.length" class="text-xs text-mnema-tertiary">{{ $t('wiki.subpageCount', { count: node.children.length }) }}</span>
            </button>
          </li>
        </ul>
      </section>
      <section v-if="wiki.recent.length" :aria-label="$t('wiki.recent')">
        <h2 class="mb-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('wiki.recent') }}</h2>
        <ul class="divide-y divide-mnema-hairline rounded-lg border border-mnema-border">
          <li v-for="p in wiki.recent" :key="p.id">
            <button type="button" class="flex w-full items-center justify-between gap-3 px-4 py-2 text-left text-sm transition-colors hover:bg-mnema-hover focus-ring" @click="goWiki({ mode: 'page', slug: p.slug })">
              <span class="truncate text-mnema-text">{{ p.title }}</span>
              <time class="shrink-0 text-xs text-mnema-tertiary" :datetime="p.updated_at">{{ relative(p.updated_at, locale) }}</time>
            </button>
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>
