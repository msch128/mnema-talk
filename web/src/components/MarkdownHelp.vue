<script setup lang="ts">
// Formatting cheat sheet next to the composer: what to type, and how it looks.
import { onBeforeUnmount, ref, watch } from 'vue'
import { Type, X } from '@lucide/vue'
import MarkdownContent from './MarkdownContent.vue'

defineProps({ disabled: { type: Boolean, default: false } })

// [label key, what to type]; the result column renders the same text.
const ROWS: ReadonlyArray<readonly [string, string]> = [
  ['bold', '**bold**'],
  ['italic', '*italic*'],
  ['underline', '__underline__'],
  ['strike', '~~strike~~'],
  ['spoiler', '||spoiler||'],
  ['code', '`code`'],
  ['codeBlock', '```js\nlet a = 1\n```'],
  ['heading', '# Heading'],
  ['subtext', '-# Small text'],
  ['list', '- item'],
  ['numbered', '1. item'],
  ['quote', '> quote'],
  ['blockQuote', '>>> quote'],
  ['link', '[text](https://example.com)'],
  ['noEmbed', '<https://example.com>']
]

const open = ref(false)
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)

function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) button.value?.focus()
}

function onDocumentPointer(event: Event) {
  if (root.value && event.target instanceof Node && !root.value.contains(event.target)) close()
}

watch(open, isOpen => {
  if (isOpen) document.addEventListener('pointerdown', onDocumentPointer)
  else document.removeEventListener('pointerdown', onDocumentPointer)
})
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocumentPointer))
</script>

<template>
  <div ref="root" class="relative shrink-0" @keydown.esc.stop="close(true)">
    <button
      ref="button"
      type="button"
      data-testid="markdown-help-button"
      :disabled="disabled"
      v-tooltip="$t('format.help')"
      :aria-label="$t('format.help')"
      aria-haspopup="dialog"
      :aria-expanded="open ? 'true' : 'false'"
      :class="[
        'flex h-8 w-8 items-center justify-center rounded-full transition disabled:opacity-50',
        open ? 'bg-mnema-surface text-mnema-accent' : 'text-mnema-tertiary hover:bg-mnema-surface hover:text-mnema-text'
      ]"
      @click="open = !open"
    >
      <Type class="h-5 w-5" />
    </button>
    <div
      v-if="open"
      role="dialog"
      :aria-label="$t('format.title')"
      class="absolute bottom-full right-0 z-30 mb-2 max-h-[min(28rem,60vh)] w-88 max-w-[calc(100vw-1.5rem)] overflow-y-auto rounded-lg border border-mnema-border bg-mnema-elevated p-3 shadow-lg"
    >
      <div class="mb-2 flex items-start justify-between gap-2">
        <p class="text-xs text-mnema-muted">{{ $t('format.intro') }}</p>
        <button
          type="button"
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-sm text-mnema-tertiary hover:bg-mnema-surface hover:text-mnema-text"
          :aria-label="$t('format.close')"
          @click="close(true)"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
      <dl class="space-y-2">
        <div v-for="[key, source] in ROWS" :key="key" class="grid grid-cols-[1fr_1fr] items-start gap-2 border-t border-mnema-hairline pt-2 text-sm">
          <dt class="min-w-0">
            <span class="mb-0.5 block text-[0.6875rem] uppercase tracking-wide text-mnema-tertiary">{{ $t(`format.${key}`) }}</span>
            <code class="block whitespace-pre-wrap break-all font-mono text-xs text-mnema-muted">{{ source }}</code>
          </dt>
          <dd class="min-w-0 text-mnema-text">
            <MarkdownContent :content="source" :previews="false" />
          </dd>
        </div>
      </dl>
    </div>
  </div>
</template>
