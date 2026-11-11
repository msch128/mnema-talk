<script setup lang="ts">
// The history of a page: every saved state, who saved it and why, the change
// the chosen one made, and "Restore" to bring a good one back. A restore writes
// a new revision, so nothing is ever lost by trying.
import { computed } from 'vue'
import { ArrowLeft, Undo2 } from '@lucide/vue'
import { useWikiHistory } from '../composables/useWikiHistory'
import { goWiki } from '../lib/wikiNav'
import { dateTime } from '../../shared/lib/time'
import { locale } from '../../i18n'
import type { WikiPage } from '../lib/wikiApi'
import MarkdownContent from '../../shared/components/MarkdownContent.vue'
import WikiDiff from './WikiDiff.vue'
import { useWikiStore } from '../../stores/wiki'

const props = defineProps<{ page: WikiPage }>()
const wiki = useWikiStore()
const h = useWikiHistory(() => props.page)
const name = (r: { editor?: { display_name: string; username: string } | undefined }) => r.editor?.display_name || r.editor?.username || ''
const diffAgainst = computed(() => h.previous.value ?? '')
</script>

<template>
  <div class="mx-auto flex max-w-5xl flex-col gap-4 px-6 py-5 max-md:px-4" data-testid="wiki-history-view">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div class="min-w-0">
        <button type="button" class="mb-1 inline-flex items-center gap-1 text-xs text-mnema-tertiary hover:text-mnema-text focus-ring" @click="goWiki({ mode: 'page', slug: page.slug })"><ArrowLeft class="h-3 w-3" aria-hidden="true" />{{ page.title }}</button>
        <h1 class="text-xl font-semibold text-mnema-text">{{ $t('wiki.historyOf') }}</h1>
        <p v-if="h.mayRestore.value" class="mt-1 text-xs text-mnema-tertiary">{{ $t('wiki.revertHint') }}</p>
      </div>
    </header>

    <p v-if="h.failed.value" class="text-sm text-mnema-danger" role="alert">{{ $t('wiki.historyFailed') }}</p>

    <div class="grid grid-cols-[minmax(0,16rem)_minmax(0,1fr)] gap-4 max-md:grid-cols-1">
      <ol class="flex max-h-[28rem] flex-col overflow-auto rounded-lg border border-mnema-border max-md:max-h-60" :aria-label="$t('wiki.revisions')" data-testid="wiki-revisions">
        <li v-for="r in h.revisions.value" :key="r.rev" class="border-b border-mnema-hairline last:border-b-0">
          <button
            type="button"
            :class="['flex w-full flex-col gap-0.5 px-3 py-2 text-left text-sm transition-colors hover:bg-mnema-hover focus-ring', h.chosen.value?.rev === r.rev ? 'bg-mnema-hover' : '']"
            :aria-current="h.chosen.value?.rev === r.rev ? 'true' : undefined"
            :data-rev="r.rev"
            @click="h.choose(r.rev)"
          >
            <span class="flex items-center justify-between gap-2">
              <span class="font-medium text-mnema-text">{{ $t('wiki.revisionNumber', { n: r.rev }) }}<span v-if="r.rev === h.latest.value" class="ml-1.5 rounded bg-mnema-accent/20 px-1.5 text-xs text-mnema-accent">{{ $t('wiki.current') }}</span></span>
              <span class="text-xs text-mnema-tertiary">{{ $t('wiki.sizeKb', { kb: Math.max(1, Math.round(r.size / 1024)) }) }}</span>
            </span>
            <span class="truncate text-xs text-mnema-tertiary">{{ name(r) || $t('wiki.formerMember') }} · {{ dateTime(r.created_at, locale) }}</span>
            <span v-if="r.restored_from" class="text-xs text-mnema-muted">{{ $t('wiki.restoreOf', { n: r.restored_from }) }}</span>
            <span v-if="r.summary" class="truncate text-xs text-mnema-muted">{{ r.summary }}</span>
          </button>
        </li>
        <li v-if="h.more.value" class="p-2"><button type="button" class="btn btn-ghost btn-sm w-full" :disabled="h.loading.value" data-testid="wiki-older" @click="h.load(h.revisions.value.at(-1)?.rev)">{{ $t('wiki.older') }}</button></li>
        <li v-if="h.loading.value && !h.revisions.value.length" class="px-3 py-3 text-sm text-mnema-tertiary">{{ $t('wiki.loading') }}</li>
      </ol>

      <section v-if="h.chosen.value" class="flex min-w-0 flex-col gap-3" :aria-label="$t('wiki.revisionNumber', { n: h.chosen.value.rev })" data-testid="wiki-revision">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="text-base font-semibold text-mnema-text">{{ h.chosen.value.title }}</h2>
          <button v-if="h.mayRestore.value && h.chosen.value.rev !== h.latest.value" type="button" class="btn btn-primary btn-sm" :disabled="h.busy.value" data-testid="wiki-restore" @click="h.restore(h.chosen.value.rev)"><Undo2 class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('wiki.restoreThis') }}</button>
        </div>
        <p v-if="h.noCompare.value" class="text-sm text-mnema-tertiary" data-testid="wiki-no-compare">{{ $t('wiki.noCompare') }}</p>
        <template v-else-if="h.previous.value !== null">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('wiki.changesMade') }}</h3>
          <WikiDiff :before="diffAgainst" :after="h.chosen.value.content ?? ''" />
        </template>
        <h3 class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('wiki.textOfRevision') }}</h3>
        <div class="max-h-80 overflow-auto rounded-md border border-mnema-border p-3">
          <MarkdownContent :content="h.chosen.value.content ?? ''" :previews="false" :wiki-pages="wiki.live" />
        </div>
      </section>
    </div>
  </div>
</template>
