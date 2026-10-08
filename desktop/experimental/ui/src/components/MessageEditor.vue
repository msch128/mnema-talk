<script setup lang="ts">
// Inline editor for a message. Takes focus when it opens, so editing from
// the keyboard (e on a message) can go straight on typing.
import { ref, onMounted, nextTick } from 'vue'
import { Check, Loader2 } from '@lucide/vue'

defineProps({
  saving: { type: Boolean, default: false },
  // Tighter spacing for the thread panel.
  compact: { type: Boolean, default: false },
  label: { type: String, default: '' }
})
const emit = defineEmits<{ save: []; cancel: [] }>()
const text = defineModel<string>({ type: String, default: '' })

const input = ref<HTMLTextAreaElement | null>(null)

onMounted(() => {
  nextTick(() => {
    const el = input.value
    if (!el) return
    el.focus()
    const end = el.value.length
    el.setSelectionRange?.(end, end)
  })
})

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
      @keydown.enter.exact.prevent="emit('save')"
      @keydown.esc.prevent="emit('cancel')"
      :class="[
        'w-full text-message rounded-lg bg-mnema-surface border border-mnema-accent text-mnema-text focus:outline-none resize-none',
        compact ? 'p-1.5' : 'p-2'
      ]"
    ></textarea>
    <div class="flex items-center justify-between text-xs text-mnema-tertiary">
      <span class="font-mono">{{ $t('chat.editHint') }}</span>
      <div :class="['flex items-center', compact ? 'gap-1' : 'gap-1.5']">
        <button
          type="button"
          @click="emit('cancel')"
          :class="[compact ? 'px-1.5' : 'px-2 rounded', 'py-0.5 text-mnema-muted hover:text-mnema-text']"
        >{{ $t('common.cancel') }}</button>
        <button
          type="button"
          @click="emit('save')"
          :disabled="saving"
          :class="[
            compact ? 'px-2 py-0.5' : 'px-2.5 py-1',
            'rounded bg-mnema-accent text-mnema-canvas font-bold flex items-center gap-1 hover:brightness-110 active:scale-95 disabled:opacity-50'
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
