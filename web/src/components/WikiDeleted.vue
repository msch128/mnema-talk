<script setup lang="ts">
// The deleted pages (Manage the wiki): open one to read it, or restore it.
import { ref } from 'vue'
import { Undo2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { undeleteWikiPage } from '../lib/wikiApi'
import { goWiki } from '../lib/wikiNav'
import { relative } from '../lib/time'
import { locale, t } from '../i18n'
import { useToastStore } from '../stores/toast'
import { useWikiStore } from '../stores/wiki'

const wiki = useWikiStore()
const toasts = useToastStore()
const busy = ref('')

async function restore(slug: string) {
  if (busy.value) return
  busy.value = slug
  try {
    wiki.applyPage(await undeleteWikiPage(slug), false)
    await wiki.loadTree()
    toasts.success(t('wiki.restored'))
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('wiki.actionFailed')))
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <div class="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-6 max-md:px-4" data-testid="wiki-deleted-view">
    <h1 class="text-xl font-semibold text-mnema-text">{{ $t('wiki.deletedPages') }}</h1>
    <p class="text-sm text-mnema-tertiary">{{ $t('wiki.deletedIntro') }}</p>
    <p v-if="!wiki.canManage" class="text-sm text-mnema-danger" role="alert">{{ $t('wiki.noAccess') }}</p>
    <p v-else-if="wiki.deleted.length === 0" class="text-sm italic text-mnema-tertiary" data-testid="wiki-no-deleted">{{ $t('wiki.noDeleted') }}</p>
    <ul v-else class="divide-y divide-mnema-hairline rounded-lg border border-mnema-border">
      <li v-for="p in wiki.deleted" :key="p.id" class="flex items-center justify-between gap-3 px-4 py-2.5">
        <button type="button" class="min-w-0 truncate text-left text-sm text-mnema-text hover:underline focus-ring" @click="goWiki({ mode: 'page', slug: p.slug })">{{ p.title }}</button>
        <span class="flex shrink-0 items-center gap-3">
          <time class="text-xs text-mnema-tertiary" :datetime="p.deleted_at">{{ relative(p.deleted_at, locale) }}</time>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="!!busy" :data-restore="p.slug" @click="restore(p.slug)"><Undo2 class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.restorePage') }}</button>
        </span>
      </li>
    </ul>
  </div>
</template>
