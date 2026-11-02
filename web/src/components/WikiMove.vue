<script setup lang="ts">
// Rename or move a page (Manage the wiki): its address, its parent and its
// place among its siblings. The old address keeps working as a redirect.
import { computed, ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { changeWikiPage, type WikiPage } from '../lib/wikiApi'
import { goWiki } from '../lib/wikiNav'
import { buildTree, height, subtreeSlugs, type WikiNode } from '../lib/wikiTree'
import { t } from '../i18n'
import { WIKI_FIELD as FIELD } from '../lib/wikiUi'
import { useWikiStore } from '../stores/wiki'

const MAX_DEPTH = 5

const props = defineProps<{ page: WikiPage }>()
const emit = defineEmits<{ done: [] }>()
const wiki = useWikiStore()

const slug = ref(props.page.slug)
const parent = ref(props.page.parent_id ?? '')
const position = ref(props.page.position)
const error = ref('')
const busy = ref(false)

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

async function save() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const next = await changeWikiPage(props.page.slug, {
      base_rev: props.page.rev,
      ...(slug.value !== props.page.slug ? { slug: slug.value.trim() } : {}),
      ...(parent.value !== (props.page.parent_id ?? '') ? { parent_id: parent.value || '00000000-0000-0000-0000-000000000000' } : {}),
      ...(position.value !== props.page.position ? { position: Math.max(0, Math.trunc(position.value)) } : {})
    })
    wiki.applyPage(next)
    await wiki.loadTree()
    emit('done')
    if (next.slug !== props.page.slug) void goWiki({ mode: 'page', slug: next.slug }, true)
  } catch (e) {
    error.value = caughtErrorMessage(e, t('wiki.actionFailed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="flex flex-col gap-3 rounded-lg border border-mnema-border bg-mnema-raised p-4" data-testid="wiki-move-form" @submit.prevent="save">
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.address') }}</span>
      <input v-model="slug" type="text" maxlength="80" :class="[FIELD, 'font-mono']" data-testid="wiki-move-slug" />
      <span class="text-xs text-mnema-tertiary">{{ $t('wiki.addressHint') }}</span>
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.parent') }}</span>
      <select v-model="parent" :class="FIELD" data-testid="wiki-move-parent">
        <option value="">{{ $t('wiki.topLevel') }}</option>
        <option v-for="p in choices" :key="p.id" :value="p.id">{{ p.title }}</option>
      </select>
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.position') }}</span>
      <input v-model.number="position" type="number" min="0" :class="[FIELD, 'w-28']" data-testid="wiki-move-position" />
    </label>
    <p v-if="error" class="text-sm text-mnema-danger" role="alert" data-testid="wiki-move-error">{{ error }}</p>
    <div class="flex gap-2">
      <button type="submit" class="btn btn-primary btn-sm" :disabled="busy">{{ $t('common.save') }}</button>
      <button type="button" class="btn btn-ghost btn-sm" @click="emit('done')">{{ $t('common.cancel') }}</button>
    </div>
  </form>
</template>
