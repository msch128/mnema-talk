<script setup lang="ts">
// The wiki (doc/wiki.md): a dialog over the app with the page tree on the left
// and the page, its editor, its history or the search on the right. It lives at
// /wiki/…: the address says what it shows, and Back closes it.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { BookOpen, FilePlus2, Menu, Search, X } from '@lucide/vue'
import { useCompactLayout } from '../shared/composables/useCompactLayout'
import { useWikiLocation } from '../composables/useWikiLocation'
import { closeWiki, goWiki, mayLeaveWiki, wikiQuery } from '../lib/wikiNav'
import { navigate } from '../shared/lib/router'
import { wikiPath } from '../lib/wikiPaths'
import { useWikiStore } from '../stores/wiki'
import BaseDialog from '../shared/components/BaseDialog.vue'
import WikiTree from './WikiTree.vue'
import WikiHome from './WikiHome.vue'
import WikiPageView from './WikiPageView.vue'
import WikiEditor from './WikiEditor.vue'
import WikiHeldDraft from './WikiHeldDraft.vue'
import WikiHistory from './WikiHistory.vue'
import WikiSearch from './WikiSearch.vue'
import WikiDeleted from './WikiDeleted.vue'

const wiki = useWikiStore()
const { location, slug } = useWikiLocation()
const { isCompact } = useCompactLayout()
const treeOpen = ref(false)
const treeToggle = ref<HTMLButtonElement | null>(null)
const tree = ref<{ $el: HTMLElement } | null>(null)

const page = computed(() => wiki.page)
const needsPage = computed(() => ['page', 'history', 'edit'].includes(location.value.mode))
// Whether the address /edit may open an editor: the server checks again on every save.
const mayEdit = computed(() => wiki.canEdit && !!page.value && !page.value.deleted_at && (!page.value.locked || wiki.canManage))

onMounted(() => { void wiki.loadTree() })
// Managers also get the deleted pages: read the tree again when that right comes or goes.
watch(() => wiki.canManage, () => { void wiki.loadTree() })
// What was kept from an earlier visit may be old: the first read of a page always asks the server.
let opened = true

// The page the address names: read it, and move the address to its current name after a rename.
watch(slug, async name => {
  treeOpen.value = false
  if (!name) {
    wiki.clearPage()
    return
  }
  const fresh = opened
  opened = false
  if (!fresh && wiki.page?.slug === name && wiki.pageState === 'ready') return
  const read = await wiki.loadPage(name)
  if (read && read.slug !== name && slug.value === name) {
    navigate(wikiPath({ ...location.value, slug: read.slug } as Parameters<typeof wikiPath>[0]), { replace: true })
  }
}, { immediate: true })

watch(location, () => { treeOpen.value = false })

// The page list over the page (narrow screens): focus goes into it when it opens and back to its button when it
// closes from inside; a tap beside it (the scrim) closes it.
watch(treeOpen, async open => {
  const at = document.activeElement
  const inside = !!tree.value?.$el.contains(at) || at === document.body || (at instanceof HTMLElement && at.dataset.testid === 'wiki-tree-scrim')
  await nextTick()
  if (open) tree.value?.$el.querySelector<HTMLElement>('button')?.focus()
  else if (inside) treeToggle.value?.focus()
})

/** The close button asks about unsaved text first, like Escape and a click outside do. */
async function leave() {
  if (await mayLeaveWiki()) closeWiki()
}

function search(event: Event) {
  const value = (event.target as HTMLInputElement).value
  wikiQuery.value = value
  if (location.value.mode !== 'search') void goWiki({ mode: 'search' })
}
</script>

<template>
  <BaseDialog sheet panel-class="max-w-6xl h-[min(92vh,54rem)]" :guard="mayLeaveWiki" @close="closeWiki">
    <template #default="{ titleId }">
      <header class="flex shrink-0 items-center gap-2 border-b border-mnema-hairline bg-mnema-raised px-4 py-3">
        <button v-if="isCompact" ref="treeToggle" type="button" class="btn btn-ghost btn-icon" :aria-expanded="treeOpen" aria-controls="wiki-tree" :aria-label="$t('wiki.pages')" data-testid="wiki-tree-toggle" @click="treeOpen = !treeOpen"><Menu class="h-5 w-5" /></button>
        <BookOpen class="h-5 w-5 shrink-0 text-mnema-accent max-md:hidden" aria-hidden="true" />
        <h2 :id="titleId" class="text-lg font-semibold text-mnema-text">{{ $t('wiki.title') }}</h2>
        <label class="relative ml-auto flex min-w-0 max-w-xs flex-1 items-center">
          <Search class="pointer-events-none absolute left-2.5 h-4 w-4 text-mnema-tertiary" aria-hidden="true" />
          <input type="search" :value="wikiQuery" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface py-1.5 pl-8 pr-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent" :placeholder="$t('wiki.searchPlaceholder')" :aria-label="$t('wiki.searchLabel')" data-testid="wiki-search-input" @input="search" />
        </label>
        <button v-if="wiki.canEdit" type="button" class="btn btn-primary btn-sm max-md:hidden" data-testid="wiki-new-top" @click="goWiki({ mode: 'new' })"><FilePlus2 class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.newPage') }}</button>
        <button type="button" class="btn btn-ghost btn-icon" :aria-label="$t('common.close')" data-testid="wiki-close" @click="leave"><X class="h-5 w-5" /></button>
      </header>

      <div class="relative flex min-h-0 flex-1">
        <button v-if="isCompact && treeOpen" type="button" class="absolute inset-0 z-5 bg-black/40" tabindex="-1" aria-hidden="true" data-testid="wiki-tree-scrim" @click="treeOpen = false"></button>
        <WikiTree id="wiki-tree" ref="tree" :class="['w-64 shrink-0 border-r border-mnema-hairline bg-mnema-raised/60', isCompact ? 'absolute inset-y-0 left-0 z-10 w-72 max-w-[85%] bg-mnema-raised shadow-xl' : '', isCompact && !treeOpen ? 'hidden' : '']" />
        <main class="min-w-0 flex-1 overflow-y-auto" data-testid="wiki-main">
          <WikiSearch v-if="location.mode === 'search'" />
          <WikiDeleted v-else-if="location.mode === 'deleted'" />
          <template v-else-if="location.mode === 'new' && !wiki.canEdit">
            <p class="px-6 py-8 text-sm text-mnema-danger" role="alert" data-testid="wiki-no-edit">{{ $t('wiki.noEdit') }}</p>
            <WikiHeldDraft :key="'new'" :slug="null" />
          </template>
          <WikiEditor v-else-if="location.mode === 'new'" :key="'new'" :page="null" />
          <template v-else-if="needsPage">
            <p v-if="wiki.pageState === 'loading' || wiki.pageState === 'idle'" class="px-6 py-6 text-sm text-mnema-tertiary" role="status">{{ $t('wiki.loading') }}</p>
            <div v-else-if="!page" class="mx-auto max-w-3xl px-6 py-8" role="alert" data-testid="wiki-page-error">
              <h1 class="text-lg font-semibold text-mnema-text">{{ $t(wiki.pageState === 'missing' ? 'wiki.missingTitle' : wiki.pageState === 'denied' ? 'wiki.noAccess' : 'wiki.loadFailed') }}</h1>
              <p v-if="wiki.pageState === 'missing'" class="mt-1 text-sm text-mnema-tertiary">{{ $t('wiki.missingBody') }}</p>
              <button type="button" class="btn btn-secondary btn-sm mt-4" @click="goWiki({ mode: 'home' })">{{ $t('wiki.home') }}</button>
            </div>
            <template v-else-if="location.mode === 'edit' && !mayEdit">
              <p class="px-6 py-8 text-sm text-mnema-danger" role="alert" data-testid="wiki-no-edit">{{ $t(page.locked ? 'wiki.pageLockedNoEdit' : 'wiki.noEdit') }}</p>
              <WikiHeldDraft :key="page.slug" :slug="page.slug" />
            </template>
            <WikiEditor v-else-if="location.mode === 'edit'" :key="`edit-${page.slug}-${page.id}`" :page="page" />
            <WikiHistory v-else-if="location.mode === 'history'" :key="`history-${page.slug}`" :page="page" />
            <WikiPageView v-else :page="page" />
          </template>
          <WikiHome v-else />
        </main>
      </div>
    </template>
  </BaseDialog>
</template>
