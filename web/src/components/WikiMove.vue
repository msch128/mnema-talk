<script setup lang="ts">
// Rename or move a page (Manage the wiki): its address, its parent and its
// place among its siblings. The old address keeps working as a redirect.
import { computed } from 'vue'
import { normalise, useSettingsDraft } from '../shared/composables/useSettingsDraft'
import { changeWikiPage, type WikiPage } from '../lib/wikiApi'
import { goWiki } from '../lib/wikiNav'
import { buildTree, height, subtreeSlugs, type WikiNode } from '../lib/wikiTree'
import { t } from '../i18n'
import { WIKI_FIELD as FIELD } from '../lib/wikiUi'
import { useWikiStore } from '../stores/wiki'
import { useToastStore } from '../stores/toast'
import UnsavedChangesBar from '../shared/components/UnsavedChangesBar.vue'

const MAX_DEPTH = 5

const props = defineProps<{ page: WikiPage }>()
const emit = defineEmits<{ done: [] }>()
const wiki = useWikiStore()

const toasts = useToastStore()

interface MoveFields { slug: string; parent: string; position: number | '' }
const NO_PARENT = '00000000-0000-0000-0000-000000000000'

const form = useSettingsDraft<MoveFields>({
  source: () => ({ slug: props.page.slug, parent: props.page.parent_id ?? '', position: props.page.position }),
  // An emptied position field (v-model.number leaves '') is no position, not position 0: it stays as it was.
  normalisers: { slug: normalise.text, position: v => (typeof v === 'number' && Number.isFinite(v) ? Math.max(0, Math.trunc(v)) : props.page.position) },
  validate: d => (d.slug.trim() ? {} : { slug: t('wiki.address') }),
  save: async ({ changes }) => {
    const next = await changeWikiPage(props.page.slug, {
      base_rev: props.page.rev,
      ...(changes.slug !== undefined ? { slug: changes.slug.trim() } : {}),
      ...(changes.parent !== undefined ? { parent_id: changes.parent || NO_PARENT } : {}),
      ...(typeof changes.position === 'number' ? { position: Math.max(0, Math.trunc(changes.position)) } : {})
    })
    wiki.applyPage(next, wiki.isOpen(props.page.id))
    await wiki.loadTree()
    toasts.success(t('wiki.moved'))
    emit('done')
    if (next.slug !== props.page.slug) void goWiki({ mode: 'page', slug: next.slug }, true)
  }
})
const { draft } = form

/** Cancel asks first while the form holds unsaved changes. */
async function cancel() {
  if (await form.guard()) emit('done')
}

const roots = computed(() => buildTree(wiki.live))
const own = computed<WikiNode | undefined>(() => {
  const find = (list: readonly WikiNode[]): WikiNode | undefined => {
    for (const n of list) {
      if (n.slug === props.page.slug) return n
      const inner = find(n.children)
      if (inner) return inner
    }
    return undefined
  }
  return find(roots.value)
})
// Not under itself or its subpages, and not so deep that the subtree passes five levels.
const choices = computed(() => {
  const excluded = subtreeSlugs(roots.value, props.page.slug)
  const room = MAX_DEPTH - (own.value ? height(own.value) : 1)
  return wiki.live.filter(p => !excluded.has(p.slug) && depthOf(p.id) <= room)
})

function depthOf(id: string): number {
  let depth = 0
  let at = wiki.live.find(p => p.id === id)
  while (at && depth < 10) {
    depth++
    const up: string | null = at.parent_id
    at = up ? wiki.live.find(p => p.id === up) : undefined
  }
  return depth
}

</script>

<template>
  <form class="flex flex-col gap-3 rounded-lg border border-mnema-border bg-mnema-raised p-4" data-testid="wiki-move-form" @submit.prevent="form.save" @keydown="form.onKeydown">
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.address') }}</span>
      <input v-model="draft.slug" type="text" maxlength="80" :class="[FIELD, 'font-mono']" data-testid="wiki-move-slug" />
      <span class="text-xs text-mnema-tertiary">{{ $t('wiki.addressHint') }}</span>
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.parent') }}</span>
      <select v-model="draft.parent" :class="FIELD" data-testid="wiki-move-parent">
        <option value="">{{ $t('wiki.topLevel') }}</option>
        <option v-for="p in choices" :key="p.id" :value="p.id">{{ p.title }}</option>
      </select>
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.position') }}</span>
      <input v-model.number="draft.position" type="number" min="0" :class="[FIELD, 'w-28']" data-testid="wiki-move-position" />
    </label>
    <UnsavedChangesBar
      inline
      :dirty="form.dirty.value"
      :can-save="form.canSave.value"
      :saving="form.saving.value"
      :error="form.error.value"
      @save="form.save"
      @reset="form.reset"
    >
      <template #extra>
        <button type="button" class="btn btn-ghost btn-sm" @click="cancel">{{ $t('common.cancel') }}</button>
      </template>
    </UnsavedChangesBar>
  </form>
</template>
