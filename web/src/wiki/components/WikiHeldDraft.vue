<script setup lang="ts">
// What a member wrote in the editor when they lost the right to edit (a role
// taken away, a timeout, the page locked meanwhile): the text stays visible and
// can be copied; it is not thrown away with the editor.
import { ref } from 'vue'
import { confirm } from '../../shared/lib/confirm'
import { clearWikiDraft, loadWikiDraft, wikiDraftKey } from '../lib/wikiDraft'
import { wikiDirty } from '../lib/wikiNav'
import { t } from '../../i18n'
import { useAuthStore } from '../../stores/auth'
import { WIKI_FIELD as FIELD } from '../lib/wikiUi'

const props = defineProps<{ slug: string | null }>()
const key = wikiDraftKey(useAuthStore().user?.id ?? '', props.slug)
const draft = ref(loadWikiDraft(key))

async function discard() {
  if (!(await confirm({ title: t('wiki.discardTitle'), body: t('wiki.discardBody'), confirmLabel: t('wiki.discard'), danger: true }))) return
  clearWikiDraft(key)
  wikiDirty.value = false
  draft.value = null
}
</script>

<template>
  <section v-if="draft" class="mx-auto flex max-w-3xl flex-col gap-2 px-6 pb-8 max-md:px-4" data-testid="wiki-held-draft">
    <h2 class="text-sm font-semibold text-mnema-text">{{ $t('wiki.heldTitle') }}</h2>
    <p class="text-xs text-mnema-tertiary">{{ $t('wiki.heldBody') }}</p>
    <textarea readonly :value="`${draft.title}\n\n${draft.content}`" :class="[FIELD, 'min-h-48 font-mono']" :aria-label="$t('wiki.heldTitle')" data-testid="wiki-held-text"></textarea>
    <div><button type="button" class="btn btn-ghost btn-sm" data-testid="wiki-held-discard" @click="discard">{{ $t('wiki.heldDiscard') }}</button></div>
  </section>
</template>
