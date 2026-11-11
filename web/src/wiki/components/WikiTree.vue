<script setup lang="ts">
// The page tree on the left of the wiki: the pages by parent, folded or open,
// the open one marked, and the ways to the start page, a new page, the deleted
// pages (managers) and search.
import { computed, ref, watch } from 'vue'
import { ChevronDown, ChevronRight, FilePlus2, Home, Trash2 } from '@lucide/vue'
import { useWikiStore } from '../../stores/wiki'
import { useWikiLocation } from '../composables/useWikiLocation'
import { goWiki } from '../lib/wikiNav'
import { breadcrumbs, visibleNodes, type WikiNode } from '../lib/wikiTree'

const wiki = useWikiStore()
const { location, slug } = useWikiLocation()
const collapsed = ref(new Set<string>())

const rows = computed(() => visibleNodes(wiki.tree, collapsed.value))

// The page on screen is shown: its parents open when it is opened or moved, not
// again on every refresh of the list, so a branch the member folded stays folded.
let shown = ''
watch([slug, () => wiki.pages], () => {
  if (!slug.value) { shown = ''; return }
  const parents = breadcrumbs(wiki.pages, slug.value).slice(0, -1).map(p => p.id)
  const place = `${slug.value}|${parents.join(',')}`
  if (place === shown) return
  shown = place
  if (parents.some(id => collapsed.value.has(id))) collapsed.value = new Set([...collapsed.value].filter(id => !parents.includes(id)))
}, { immediate: true })

function toggle(node: WikiNode) {
  const next = new Set(collapsed.value)
  if (next.has(node.id)) next.delete(node.id)
  else next.add(node.id)
  collapsed.value = next
}
</script>

<template>
  <nav class="flex min-h-0 flex-col gap-1 overflow-y-auto p-2" :aria-label="$t('wiki.pages')" data-testid="wiki-tree">
    <button
      type="button"
      :class="['flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-mnema-hover focus-ring', location.mode === 'home' ? 'bg-mnema-hover font-medium text-mnema-text' : 'text-mnema-muted']"
      data-testid="wiki-home"
      @click="goWiki({ mode: 'home' })"
    ><Home class="h-4 w-4 shrink-0" aria-hidden="true" />{{ $t('wiki.home') }}</button>

    <p v-if="wiki.treeLoaded && !wiki.treeFailed && rows.length === 0" class="px-2 py-2 text-xs italic text-mnema-tertiary" data-testid="wiki-tree-empty">{{ $t('wiki.noPages') }}</p>
    <p v-if="wiki.treeFailed" class="px-2 py-2 text-xs text-mnema-danger" role="alert">{{ $t('wiki.treeFailed') }}</p>

    <ul class="space-y-px">
      <li v-for="node in rows" :key="node.id" :data-depth="node.depth">
        <div :class="['flex items-center rounded-md transition-colors hover:bg-mnema-hover', slug === node.slug ? 'bg-mnema-hover' : '']" :style="{ paddingLeft: `${(node.depth - 1) * 14}px` }">
          <button
            v-if="node.children.length"
            type="button"
            class="flex h-7 w-6 shrink-0 items-center justify-center text-mnema-tertiary hover:text-mnema-text focus-ring"
            :aria-label="$t(collapsed.has(node.id) ? 'wiki.expand' : 'wiki.collapse', { title: node.title })"
            :aria-expanded="!collapsed.has(node.id)"
            @click="toggle(node)"
          >
            <ChevronRight v-if="collapsed.has(node.id)" class="h-3.5 w-3.5" />
            <ChevronDown v-else class="h-3.5 w-3.5" />
          </button>
          <span v-else class="w-6 shrink-0" aria-hidden="true"></span>
          <button
            type="button"
            :class="['min-w-0 flex-1 truncate py-1.5 pr-2 text-left text-sm focus-ring', slug === node.slug ? 'font-medium text-mnema-text' : 'text-mnema-muted']"
            :aria-current="slug === node.slug ? 'page' : undefined"
            :data-wiki-page="node.slug"
            @click="goWiki({ mode: 'page', slug: node.slug })"
          >{{ node.title }}<span v-if="node.locked" class="sr-only"> ({{ $t('wiki.locked') }})</span></button>
        </div>
      </li>
    </ul>

    <div class="mt-2 flex flex-col gap-1 border-t border-mnema-hairline pt-2">
      <button
        v-if="wiki.canEdit"
        type="button"
        class="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-mnema-muted transition-colors hover:bg-mnema-hover hover:text-mnema-text focus-ring"
        data-testid="wiki-new"
        @click="goWiki({ mode: 'new' })"
      ><FilePlus2 class="h-4 w-4 shrink-0" aria-hidden="true" />{{ $t('wiki.newPage') }}</button>
      <button
        v-if="wiki.canManage"
        type="button"
        :class="['flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-mnema-hover focus-ring', location.mode === 'deleted' ? 'text-mnema-text' : 'text-mnema-muted']"
        data-testid="wiki-deleted"
        @click="goWiki({ mode: 'deleted' })"
      ><Trash2 class="h-4 w-4 shrink-0" aria-hidden="true" />{{ $t('wiki.deletedPages') }}<span v-if="wiki.deleted.length" class="ml-auto text-xs text-mnema-tertiary">{{ wiki.deleted.length }}</span></button>
    </div>
  </nav>
</template>
