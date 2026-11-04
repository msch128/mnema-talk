<script setup lang="ts">
// What changed between two texts, line by line: removed lines in red, added in
// green, unchanged ones dim. Marks and words carry the meaning, not colour alone.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { diffStats, lineDiff } from '../lib/lineDiff'

const props = defineProps<{ before: string; after: string }>()
// While the writer types on one side (the conflict view), the diff follows a moment after the last key,
// not on every keystroke; a different text to compare against shows at once.
const SETTLE_MS = 300
const shown = ref({ before: props.before, after: props.after })
let timer: ReturnType<typeof setTimeout> | undefined
watch(() => [props.before, props.after] as const, ([before, after]) => {
  clearTimeout(timer)
  const settle = () => { shown.value = { before, after } }
  if (before !== shown.value.before) settle()
  else timer = setTimeout(settle, SETTLE_MS)
})
onBeforeUnmount(() => clearTimeout(timer))
const ops = computed(() => lineDiff(shown.value.before, shown.value.after))
const stats = computed(() => (ops.value ? diffStats(ops.value) : null))
const changed = computed(() => !!ops.value && ops.value.some(o => o.kind !== 'same'))
</script>

<template>
  <div class="overflow-hidden rounded-md border border-mnema-border text-sm" data-testid="wiki-diff">
    <p v-if="!ops" class="px-3 py-2 text-mnema-tertiary">{{ $t('wiki.diffTooBig') }}</p>
    <template v-else>
      <p class="border-b border-mnema-border bg-mnema-raised px-3 py-1.5 text-xs text-mnema-tertiary" data-testid="wiki-diff-stats">
        <template v-if="changed">{{ $t('wiki.diffStats', { added: stats?.added ?? 0, removed: stats?.removed ?? 0 }) }}</template>
        <template v-else-if="shown.before !== shown.after">{{ $t('wiki.diffWhitespace') }}</template>
        <template v-else>{{ $t('wiki.diffSame') }}</template>
      </p>
      <ol v-if="changed" class="max-h-96 overflow-auto font-mono text-xs leading-5">
        <li
          v-for="(op, i) in ops"
          :key="i"
          :class="['flex gap-2 whitespace-pre-wrap break-all px-3', op.kind === 'add' ? 'bg-mnema-accent/15 text-mnema-text' : op.kind === 'del' ? 'bg-mnema-danger/15 text-mnema-text' : 'text-mnema-tertiary']"
          :data-op="op.kind"
        >
          <span class="w-3 shrink-0 select-none" aria-hidden="true">{{ op.kind === 'add' ? '+' : op.kind === 'del' ? '−' : ' ' }}</span>
          <span class="sr-only">{{ op.kind === 'add' ? $t('wiki.diffAdded') : op.kind === 'del' ? $t('wiki.diffRemoved') : '' }}</span>
          <span class="min-w-0">{{ op.text || ' ' }}</span>
        </li>
      </ol>
    </template>
  </div>
</template>
