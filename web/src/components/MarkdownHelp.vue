<script setup lang="ts">
// Formatting reference next to the composer: what to type, and how it looks.
// Examples go through the message renderer, so the sheet cannot drift from it.
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ALargeSmall, X } from '@lucide/vue'
import MarkdownContent from './MarkdownContent.vue'
import { useDismissable } from '../composables/useDismissable'
import { boxOf, placeNear } from '../lib/floatingPosition'

defineProps({ disabled: { type: Boolean, default: false } })
const emit = defineEmits<{ insert: [text: string, options: { block: boolean }] }>()

// [label key, what to type]; the result column renders the same text.
type Row = readonly [string, string]
const GROUPS: ReadonlyArray<{ key: string; rows: ReadonlyArray<Row> }> = [
  {
    key: 'groupText',
    rows: [
      ['bold', '**bold**'],
      ['italic', '*italic*'],
      ['underline', '__underline__'],
      ['strike', '~~strike~~'],
      ['spoiler', '||spoiler||'],
      ['code', '`code`']
    ]
  },
  {
    key: 'groupBlocks',
    rows: [
      ['codeBlock', '```js\nlet a = 1\n```'],
      ['heading', '# Heading'],
      ['subtext', '-# Small text'],
      ['list', '- item'],
      ['numbered', '1. item'],
      ['quote', '> quote'],
      ['blockQuote', '>>> quote']
    ]
  },
  {
    key: 'groupLinks',
    rows: [
      ['link', '[text](https://example.com)'],
      ['noEmbed', '<https://example.com>'],
      ['mentionAll', '@all']
    ]
  }
]

// Rows that only render when they start a line.
const BLOCK_ROWS = new Set(['codeBlock', 'heading', 'subtext', 'list', 'numbered', 'quote', 'blockQuote'])

const MD_BREAKPOINT = 768
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const button = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const placement = ref<{ left: string; top: string } | null>(null)

function close(reason?: 'outside' | 'escape') {
  open.value = false
  if (reason !== 'outside') button.value?.focus()
}

// From md up the panel floats beside its button but is placed against the
// window, so a narrow thread sidebar cannot clip it; below md it is a sheet.
function place() {
  const anchor = button.value
  const el = panel.value
  if (!anchor || !el || window.innerWidth < MD_BREAKPOINT) {
    placement.value = null
    return
  }
  const { x, y } = placeNear(boxOf(anchor.getBoundingClientRect()), { width: el.offsetWidth, height: el.offsetHeight }, { width: window.innerWidth, height: window.innerHeight }, { gap: 8 })
  placement.value = { left: `${Math.round(x)}px`, top: `${Math.round(y)}px` }
}

function insert([key, source]: Row) {
  // A mention is followed by more text: leave the space for it.
  emit('insert', key === 'mentionAll' ? `${source} ` : source, { block: BLOCK_ROWS.has(key) })
  open.value = false
}

useDismissable(() => [root.value], (_event, reason) => close(reason), { active: open })

watch(open, async isOpen => {
  if (!isOpen) {
    window.removeEventListener('resize', place)
    return
  }
  await nextTick()
  place()
  window.addEventListener('resize', place)
  panel.value?.focus()
})
onBeforeUnmount(() => window.removeEventListener('resize', place))
</script>

<template>
  <div ref="root" class="relative shrink-0">
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
      <ALargeSmall class="h-5 w-5" />
    </button>
    <div
      v-if="open"
      ref="panel"
      role="dialog"
      tabindex="-1"
      :aria-label="$t('format.title')"
      :style="placement ?? undefined"
      class="fixed z-40 flex max-h-[min(34rem,calc(100dvh-1.5rem))] w-[26rem] max-w-[calc(100vw-1rem)] flex-col overflow-hidden rounded-xl bg-mnema-elevated shadow-popover outline-hidden pop-in max-md:inset-x-2 max-md:bottom-[max(0.5rem,env(safe-area-inset-bottom))] max-md:w-auto"
    >
      <div class="flex items-start justify-between gap-3 px-4 pb-2 pt-3">
        <div class="min-w-0">
          <h2 class="text-sm font-semibold text-mnema-text">{{ $t('format.title') }}</h2>
          <p class="mt-0.5 text-xs text-mnema-muted">{{ $t('format.intro') }}</p>
        </div>
        <button type="button" class="btn btn-icon h-7 w-7 shrink-0" :aria-label="$t('format.close')" @click="close()">
          <X class="h-4 w-4" />
        </button>
      </div>
      <div aria-hidden="true" class="grid grid-cols-2 gap-3 border-b border-mnema-hairline px-4 pb-1.5 text-[0.6875rem] font-medium uppercase tracking-wide text-mnema-tertiary">
        <span>{{ $t('format.type') }}</span>
        <span>{{ $t('format.result') }}</span>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pb-3">
        <section v-for="group in GROUPS" :key="group.key" :aria-labelledby="`format-${group.key}`" class="mt-4 first:mt-3">
          <h3 :id="`format-${group.key}`" class="mb-1 text-xs font-semibold text-mnema-muted">{{ $t(`format.${group.key}`) }}</h3>
          <ul>
            <li v-for="row in group.rows" :key="row[0]" class="grid grid-cols-2 items-start gap-3 border-t border-mnema-hairline py-1.5 text-sm first:border-t-0">
              <button
                type="button"
                data-testid="format-example"
                class="focus-ring group/ex -mx-1.5 min-w-0 rounded-md px-1.5 py-0.5 text-left transition hover:bg-mnema-surface"
                :aria-label="`${$t(`format.${row[0]}`)}: ${$t('format.insert')}`"
                v-tooltip="$t('format.insert')"
                @click="insert(row)"
              >
                <code class="block whitespace-pre-wrap break-all font-mono text-xs text-mnema-text">{{ row[1] }}</code>
                <span class="mt-0.5 block text-xs text-mnema-tertiary">{{ $t(`format.${row[0]}`) }}</span>
              </button>
              <div class="min-w-0 py-0.5 text-mnema-text" data-testid="format-result">
                <MarkdownContent :content="row[1]" :previews="false" />
              </div>
            </li>
          </ul>
        </section>
      </div>
      <p class="border-t border-mnema-hairline px-4 py-2.5 text-xs text-mnema-muted">{{ $t('format.tip') }}</p>
    </div>
  </div>
</template>
