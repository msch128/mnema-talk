<script setup lang="ts">
// Inline editor for a message. Takes focus when it opens, so editing from
// the keyboard (e on a message) can go straight on typing.
import { ref, onMounted, nextTick, useId } from 'vue'
import { Check, Loader2 } from '@lucide/vue'
import { isImeKey } from '../composables/useComposerKeys'
import { arrowStep } from '../composables/useOwnMessageNav'

defineProps({
  saving: { type: Boolean, default: false },
  // Tighter spacing for the thread panel.
  compact: { type: Boolean, default: false },
  label: { type: String, default: '' }
})
// navigate: ArrowUp/Down on the first/last line (-1 older, 1 newer); the parent calls preventDefault when it moves on.
const emit = defineEmits<{ save: []; cancel: []; navigate: [step: -1 | 1, event: KeyboardEvent] }>()
const text = defineModel<string>({ type: String, default: '' })

const input = ref<HTMLTextAreaElement | null>(null)
const hintId = useId()

onMounted(() => {
  nextTick(() => {
    const el = input.value
    if (!el) return
    el.focus()
    const end = el.value.length
    el.setSelectionRange?.(end, end)
  })
})

// Enter that confirms an IME candidate must not save the edit.
function onEnter(e: KeyboardEvent) {
  if (isImeKey(e)) return
  e.preventDefault()
  emit('save')
}

function onArrow(e: KeyboardEvent) {
  const step = arrowStep(e)
  if (step) emit('navigate', step, e)
}

defineExpose({ focus: () => input.value?.focus() })
</script>

<template>
  <div :class="['mt-1', compact ? 'space-y-1' : 'space-y-1.5']">
    <textarea
      ref="input"
      v-model="text"
      rows="2"
      :aria-label="label || undefined"
      data-testid="message-editor"
      :aria-describedby="hintId"
      @keydown.up="onArrow"
      @keydown.down="onArrow"
      @keydown.enter.exact="onEnter"
      @keydown.esc.prevent="emit('cancel')"
      :class="[
        'w-full text-message rounded-lg bg-mnema-surface border border-mnema-border-strong text-mnema-text resize-none',
        compact ? 'p-1.5' : 'p-2'
      ]"
    ></textarea>
    <div class="flex items-center justify-between text-xs text-mnema-tertiary">
      <span :id="hintId" class="font-mono">{{ $t('chat.editHint') }}</span>
      <div :class="['flex items-center', compact ? 'gap-1' : 'gap-1.5']">
        <button
          type="button"
          @click="emit('cancel')"
          :class="[compact ? 'px-1.5' : 'px-2 rounded-sm', 'py-0.5 text-mnema-muted hover:text-mnema-text']"
        >{{ $t('common.cancel') }}</button>
        <button
          type="button"
          @click="emit('save')"
          :disabled="saving"
          :class="[
            compact ? 'px-2 py-0.5' : 'px-2.5 py-1',
            'rounded-sm bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50'
          ]"
        >
          <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />
          <Check v-else class="w-3.5 h-3.5" />
          <span>{{ $t('common.save') }}</span>
        </button>
      </div>
    </div>
  </div>
</template>
