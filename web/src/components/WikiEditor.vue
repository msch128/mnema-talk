<script setup lang="ts">
// Writing a wiki page: title, Markdown with a small toolbar and a live preview,
// an optional summary of the change, and the size against the 50 KB limit. A
// save the server refuses because somebody saved meanwhile never overwrites
// anything: the editor shows the difference and lets the writer decide.
import { computed, nextTick, ref } from 'vue'
import { Bold, Code, Heading2, ImagePlus, Italic, Link, List, Quote, BookMarked } from '@lucide/vue'
import { applyEdit, type EditAction } from '../lib/wikiEditorActions'
import { MAX_BYTES, useWikiEditor } from '../composables/useWikiEditor'
import { useWikiImages } from '../composables/useWikiImages'
import { deriveSlug } from '../lib/wikiSlug'
import { WIKI_FIELD as FIELD } from '../lib/wikiUi'
import { goWiki } from '../lib/wikiNav'
import { useWikiStore } from '../stores/wiki'
import type { WikiPage } from '../lib/wikiApi'
import MarkdownContent from './MarkdownContent.vue'
import WikiDiff from './WikiDiff.vue'

const props = defineProps<{ page: WikiPage | null; parentId?: string | null }>()
const wiki = useWikiStore()
const editor = useWikiEditor(props.page, props.parentId ?? null)
const { title, content, summary, slug, parent, error, busy, theirs, restored, bytes, tooLong, canSave } = editor
const area = ref<HTMLTextAreaElement | null>(null)
const preview = ref(false)
const picker = ref<HTMLInputElement | null>(null)
const images = useWikiImages(area, content)

const TOOLS: { action: EditAction; icon: typeof Bold; key: string }[] = [
  { action: 'heading', icon: Heading2, key: 'wiki.toolHeading' }, { action: 'bold', icon: Bold, key: 'wiki.toolBold' },
  { action: 'italic', icon: Italic, key: 'wiki.toolItalic' }, { action: 'code', icon: Code, key: 'wiki.toolCode' },
  { action: 'list', icon: List, key: 'wiki.toolList' }, { action: 'quote', icon: Quote, key: 'wiki.toolQuote' },
  { action: 'link', icon: Link, key: 'wiki.toolLink' }, { action: 'wikilink', icon: BookMarked, key: 'wiki.toolWikiLink' }
]

// A new page can go below any page that leaves a level (at most five).
const MAX_DEPTH = 5
const parents = computed(() => wiki.pages.filter(p => !p.deleted_at && depth(p.id) < MAX_DEPTH))
const suggested = computed(() => deriveSlug(title.value))

function depth(id: string): number {
  let n = 0
  let at = wiki.pages.find(p => p.id === id)
  while (at && n < 10) {
    n++
    const up: string | null = at.parent_id
    at = up ? wiki.pages.find(p => p.id === up) : undefined
  }
  return n
}

function picked(event: Event) {
  const input = event.target as HTMLInputElement
  void images.addFiles([...(input.files ?? [])])
  input.value = ''
}

async function tool(action: EditAction) {
  const el = area.value
  if (!el) return
  const next = applyEdit(content.value, el.selectionStart, el.selectionEnd, action)
  content.value = next.text
  await nextTick()
  el.focus()
  el.setSelectionRange(next.start, next.end)
}

function cancel() {
  void goWiki(props.page ? { mode: 'page', slug: props.page.slug } : { mode: 'home' })
}
</script>

<template>
  <form class="mx-auto flex h-full max-w-5xl flex-col gap-3 px-6 py-5 max-md:px-4" data-testid="wiki-editor" @submit.prevent="editor.save()">
    <h1 class="sr-only">{{ $t(page ? 'wiki.editing' : 'wiki.newPage') }}</h1>
    <p v-if="restored" class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-mnema-raised px-3 py-2 text-xs text-mnema-muted" role="status" data-testid="wiki-draft-restored">
      <span class="min-w-0 flex-1">{{ $t('wiki.draftRestored') }}</span>
      <button type="button" class="btn btn-ghost btn-sm" data-testid="wiki-draft-discard" @click="editor.discardDraft()">{{ $t('wiki.draftDiscard') }}</button>
    </p>
    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.titleLabel') }}</span>
      <input v-model="title" type="text" maxlength="100" required :class="[FIELD, 'text-base']" data-testid="wiki-title-input" :placeholder="$t('wiki.titlePlaceholder')" />
    </label>

    <div v-if="!page" class="flex flex-wrap items-end gap-3 text-sm">
      <label class="flex min-w-48 flex-1 flex-col gap-1">
        <span class="font-medium text-mnema-text">{{ $t('wiki.parent') }}</span>
        <select v-model="parent" :class="FIELD" data-testid="wiki-parent-input">
          <option value="">{{ $t('wiki.topLevel') }}</option>
          <option v-for="p in parents" :key="p.id" :value="p.id">{{ p.title }}</option>
        </select>
      </label>
      <label class="flex min-w-48 flex-1 flex-col gap-1">
        <span class="font-medium text-mnema-text">{{ $t('wiki.address') }}</span>
        <input v-model="slug" type="text" maxlength="80" :class="[FIELD, 'font-mono']" :placeholder="suggested" data-testid="wiki-slug-input" />
      </label>
    </div>

    <div class="flex flex-wrap items-center gap-1" role="toolbar" :aria-label="$t('wiki.toolbar')">
      <button v-for="tl in TOOLS" :key="tl.action" type="button" :data-tool="tl.action" class="btn btn-ghost btn-icon btn-sm" :aria-label="$t(tl.key)" v-tooltip="$t(tl.key)" :disabled="preview" @click="tool(tl.action)"><component :is="tl.icon" class="h-4 w-4" /></button>
      <button type="button" class="btn btn-ghost btn-icon btn-sm" :aria-label="$t('wiki.toolImage')" v-tooltip="$t('wiki.toolImage')" :disabled="preview" data-testid="wiki-image-button" @click="picker?.click()"><ImagePlus class="h-4 w-4" /></button>
      <input ref="picker" type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple class="hidden" tabindex="-1" aria-hidden="true" data-testid="wiki-image-input" @change="picked" />
      <button type="button" class="btn btn-ghost btn-sm ml-auto md:hidden" :aria-pressed="preview" data-testid="wiki-preview-toggle" @click="preview = !preview">{{ $t(preview ? 'wiki.write' : 'wiki.preview') }}</button>
    </div>

    <div class="grid min-h-64 flex-1 grid-cols-2 gap-3 max-md:grid-cols-1">
      <textarea
        ref="area"
        v-model="content"
        :class="[FIELD, 'min-h-64 resize-none font-mono leading-6 max-md:min-h-80', preview ? 'max-md:hidden' : '']"
        spellcheck="true"
        :aria-label="$t('wiki.textLabel')"
        data-testid="wiki-text-input"
        @paste="images.onPaste"
        @drop="images.onDrop"
        @dragover.prevent
      ></textarea>
      <div :class="['min-h-64 overflow-auto rounded-md border border-mnema-border bg-mnema-canvas/40 p-3', preview ? '' : 'max-md:hidden']" data-testid="wiki-preview" :aria-label="$t('wiki.preview')">
        <MarkdownContent v-if="content" :content="content" :previews="false" :wiki-pages="wiki.live" />
        <p v-else class="text-sm italic text-mnema-tertiary">{{ $t('wiki.previewEmpty') }}</p>
      </div>
    </div>

    <div class="flex flex-wrap items-center gap-2 text-xs text-mnema-tertiary">
      <span :class="tooLong ? 'text-mnema-danger' : ''" data-testid="wiki-size" :role="tooLong ? 'alert' : undefined">{{ tooLong ? $t('wiki.tooLong', { kb: MAX_BYTES / 1024 }) : $t('wiki.size', { used: Math.ceil(bytes / 1024), kb: MAX_BYTES / 1024 }) }}</span>
      <span v-if="images.uploading.value > 0" role="status" data-testid="wiki-uploading">{{ $t('wiki.imageUploading') }}</span>
    </div>

    <label class="flex flex-col gap-1 text-sm">
      <span class="font-medium text-mnema-text">{{ $t('wiki.summaryLabel') }}</span>
      <input v-model="summary" type="text" maxlength="200" :class="FIELD" :placeholder="$t('wiki.summaryPlaceholder')" data-testid="wiki-summary-input" />
    </label>

    <section v-if="theirs" class="flex flex-col gap-3 rounded-lg border border-mnema-warning/50 bg-mnema-warning/10 p-4" role="alert" data-testid="wiki-conflict">
      <h2 class="font-semibold text-mnema-text">{{ $t('wiki.conflictTitle') }}</h2>
      <p class="text-sm text-mnema-muted">{{ $t('wiki.conflictBody', { name: theirs.updated_by?.display_name || theirs.updated_by?.username || $t('wiki.someone') }) }}</p>
      <WikiDiff :before="theirs.content" :after="content" />
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-primary btn-sm" data-testid="wiki-conflict-mine" :disabled="busy" @click="editor.saveOnTop()">{{ $t('wiki.conflictMine') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" data-testid="wiki-conflict-theirs" @click="editor.takeTheirs()">{{ $t('wiki.conflictTheirs') }}</button>
      </div>
    </section>

    <p v-if="error" class="text-sm text-mnema-danger" role="alert" data-testid="wiki-error">{{ error }}</p>
    <div class="flex gap-2 border-t border-mnema-hairline pt-3">
      <button type="submit" class="btn btn-primary" :disabled="!canSave || images.uploading.value > 0" data-testid="wiki-save">{{ $t('common.save') }}</button>
      <button type="button" class="btn btn-ghost" data-testid="wiki-cancel" @click="cancel">{{ $t('common.cancel') }}</button>
    </div>
  </form>
</template>
