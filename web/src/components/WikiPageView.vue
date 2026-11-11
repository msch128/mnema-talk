<script setup lang="ts">
// One wiki page: where it sits, who last changed it, its text, its subpages,
// and what the member may do with it (edit, history, lock, move, delete).
import { computed, ref, watch } from 'vue'
import { Clock, History, Link2, Lock, LockOpen, Pencil, Trash2, Undo2 } from '@lucide/vue'
import { caughtErrorMessage } from '../shared/lib/api'
import { confirm } from '../shared/lib/confirm'
import { deleteWikiPage, lockWikiPage, undeleteWikiPage, type WikiPage } from '../lib/wikiApi'
import { copyToClipboard } from '../shared/lib/clipboard'
import { goWiki } from '../lib/wikiNav'
import { breadcrumbs } from '../lib/wikiTree'
import { relative } from '../shared/lib/time'
import { locale, t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'
import MarkdownContent from '../shared/components/MarkdownContent.vue'
import WikiMove from './WikiMove.vue'

const props = defineProps<{ page: WikiPage }>()
const wiki = useWikiStore()
const toasts = useToastStore()
const moving = ref(false)
const busy = ref(false)

const trail = computed(() => breadcrumbs(wiki.pages, props.page.slug).slice(0, -1))
const children = computed(() => wiki.live.filter(p => p.parent_id === props.page.id))
const deleted = computed(() => !!props.page.deleted_at)
// A locked page changes only for managers.
const mayEdit = computed(() => wiki.canEdit && !deleted.value && (!props.page.locked || wiki.canManage))
const editor = computed(() => props.page.updated_by?.display_name || props.page.updated_by?.username || '')
const when = computed(() => relative(props.page.updated_at, locale.value))

watch(() => props.page.slug, () => { moving.value = false })

async function run(action: () => Promise<WikiPage>, done?: string) {
  if (busy.value) return
  busy.value = true
  try {
    const id = props.page.id
    // An answer that comes after the member went to another page only updates the tree.
    wiki.applyPage(await action(), wiki.isOpen(id))
    if (done) toasts.success(done)
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('wiki.actionFailed')))
  } finally {
    busy.value = false
  }
}

async function remove() {
  const ok = await confirm({ title: t('wiki.deleteTitle', { title: props.page.title }), body: t('wiki.deleteBody'), confirmLabel: t('wiki.delete'), danger: true })
  if (!ok) return
  await run(() => deleteWikiPage(props.page.slug))
  if (wiki.pageState === 'ready' && wiki.page?.deleted_at) void goWiki({ mode: 'home' }, true)
}

function copyLink() {
  void copyToClipboard(`${window.location.origin}/wiki/${encodeURIComponent(props.page.slug)}`, t('wiki.linkCopied'))
}
</script>

<template>
  <article class="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-6 max-md:px-4" data-testid="wiki-page">
    <nav v-if="trail.length" class="flex flex-wrap items-center gap-1 text-xs text-mnema-tertiary" :aria-label="$t('wiki.breadcrumbs')">
      <template v-for="p in trail" :key="p.id">
        <button type="button" class="hover:text-mnema-text focus-ring" @click="goWiki({ mode: 'page', slug: p.slug })">{{ p.title }}</button>
        <span aria-hidden="true">/</span>
      </template>
    </nav>

    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h1 class="wrap-break-word text-2xl font-semibold text-mnema-text" data-testid="wiki-title">{{ page.title }}</h1>
        <p class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-mnema-tertiary">
          <span class="inline-flex items-center gap-1"><Clock class="h-3 w-3" aria-hidden="true" />{{ editor ? $t('wiki.lastEdited', { name: editor, when }) : $t('wiki.lastEditedFormer', { when }) }}</span>
          <span>{{ $t('wiki.revisionNumber', { n: page.rev }) }}</span>
          <span v-if="page.locked" class="inline-flex items-center gap-1 text-mnema-warning" data-testid="wiki-locked"><Lock class="h-3 w-3" aria-hidden="true" />{{ $t('wiki.locked') }}</span>
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-1.5">
        <button v-if="mayEdit" type="button" class="btn btn-primary btn-sm" data-testid="wiki-edit" @click="goWiki({ mode: 'edit', slug: page.slug })"><Pencil class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.edit') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" data-testid="wiki-history" @click="goWiki({ mode: 'history', slug: page.slug })"><History class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.history') }}</button>
        <button type="button" class="btn btn-ghost btn-icon btn-sm" :aria-label="$t('wiki.copyLink')" v-tooltip="$t('wiki.copyLink')" @click="copyLink"><Link2 class="h-4 w-4" /></button>
        <template v-if="wiki.canManage && !deleted">
          <button type="button" class="btn btn-ghost btn-sm" data-testid="wiki-lock" :disabled="busy" @click="run(() => lockWikiPage(page.slug, !page.locked))">
            <LockOpen v-if="page.locked" class="h-3.5 w-3.5" aria-hidden="true" /><Lock v-else class="h-3.5 w-3.5" aria-hidden="true" />{{ $t(page.locked ? 'wiki.unlock' : 'wiki.lock') }}
          </button>
          <button type="button" class="btn btn-ghost btn-sm" data-testid="wiki-move" :aria-expanded="moving" @click="moving = !moving">{{ $t('wiki.move') }}</button>
          <button type="button" class="btn btn-ghost btn-sm text-mnema-danger" data-testid="wiki-delete" :disabled="busy" @click="remove"><Trash2 class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.delete') }}</button>
        </template>
      </div>
    </header>

    <p v-if="page.redirected_from" class="rounded-md bg-mnema-raised px-3 py-2 text-xs text-mnema-tertiary" data-testid="wiki-redirected">{{ $t('wiki.redirectedFrom', { slug: page.redirected_from }) }}</p>
    <div v-if="deleted" class="flex flex-wrap items-center gap-3 rounded-md border border-mnema-danger/40 bg-mnema-danger/10 px-3 py-2 text-sm" role="status" data-testid="wiki-deleted-banner">
      <span class="min-w-0 flex-1">{{ $t('wiki.deletedNotice') }}</span>
      <button v-if="wiki.canManage" type="button" class="btn btn-secondary btn-sm" data-testid="wiki-undelete" :disabled="busy" @click="run(() => undeleteWikiPage(page.slug), $t('wiki.restored'))"><Undo2 class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.restorePage') }}</button>
    </div>

    <WikiMove v-if="moving" :page="page" @done="moving = false" />

    <MarkdownContent v-if="page.content" :content="page.content" :previews="false" :wiki-pages="wiki.live" data-testid="wiki-content" />
    <p v-else class="text-sm italic text-mnema-tertiary" data-testid="wiki-empty-page">{{ $t('wiki.emptyPage') }}</p>

    <section v-if="children.length" class="border-t border-mnema-hairline pt-4" :aria-label="$t('wiki.subpages')">
      <h2 class="mb-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('wiki.subpages') }}</h2>
      <ul class="flex flex-wrap gap-2">
        <li v-for="child in children" :key="child.id">
          <button type="button" class="rounded-md border border-mnema-border px-3 py-1.5 text-sm text-mnema-text transition-colors hover:bg-mnema-hover focus-ring" @click="goWiki({ mode: 'page', slug: child.slug })">{{ child.title }}</button>
        </li>
      </ul>
    </section>
  </article>
</template>
