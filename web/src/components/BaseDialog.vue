<script setup>
// The one modal shell: scrim, dialog semantics, focus trap, Escape, focus
// restore. Slot content provides the heading with :id="titleId".
import { ref, useId } from 'vue'
import { X } from '@lucide/vue'
import { useDialog } from '../composables/useDialog'

const props = defineProps({
  // Standard header (title + close button). Omit to render your own header
  // inside the slot and label the dialog with the titleId slot prop.
  title: { type: String, default: '' },
  subtitle: { type: String, default: '' },
  role: { type: String, default: 'dialog' }, // 'dialog' | 'alertdialog'
  describedby: { type: String, default: '' },
  // Tailwind classes for the panel (width etc.)
  panelClass: { type: String, default: 'max-w-md' },
  closeOnScrim: { type: Boolean, default: true },
  // 'center' (default) or 'top' for palette-style dialogs such as search
  align: { type: String, default: 'center' },
  initialFocus: { type: [String, Function], default: undefined }
})
const emit = defineEmits(['close'])

const panel = ref(null)
const titleId = useId()
const close = () => emit('close')

useDialog(panel, { onClose: close, initialFocus: props.initialFocus })

// Close on scrim only when the press both started and ended on the scrim, so
// selecting text inside the dialog and releasing outside never closes it.
let downOnScrim = false
function onScrimDown(e) { downOnScrim = e.target === e.currentTarget }
function onScrimUp(e) {
  if (props.closeOnScrim && downOnScrim && e.target === e.currentTarget) close()
  downOnScrim = false
}
</script>

<template>
  <div
    :class="['fixed inset-0 z-50 flex justify-center bg-black/60 p-4', align === 'top' ? 'items-start pt-20' : 'items-center']"
    @pointerdown="onScrimDown"
    @pointerup="onScrimUp"
  >
    <div
      ref="panel"
      :role="role"
      aria-modal="true"
      :aria-labelledby="titleId"
      :aria-describedby="describedby || undefined"
      tabindex="-1"
      :class="['flex max-h-full w-full flex-col overflow-hidden rounded-[14px] border border-mnema-border bg-mnema-elevated shadow-[0_24px_64px_rgba(0,0,0,0.6)] focus:outline-none', panelClass]"
    >
      <header v-if="title" class="flex items-center justify-between gap-3 border-b border-mnema-hairline bg-mnema-raised px-5 py-4">
        <div class="min-w-0">
          <div class="flex min-w-0 items-center gap-2">
            <h2 :id="titleId" class="truncate text-lg font-semibold text-mnema-text">{{ title }}</h2>
            <slot name="badge" />
          </div>
          <p v-if="subtitle" class="truncate font-mono text-xs text-mnema-tertiary">{{ subtitle }}</p>
        </div>
        <button
          type="button"
          data-dialog-close
          v-tooltip="$t('common.close')"
          class="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md text-mnema-tertiary transition hover:bg-mnema-hover hover:text-mnema-text"
          @click="close"
        >
          <X class="h-[18px] w-[18px]" />
        </button>
      </header>
      <slot :title-id="titleId" :close="close" />
    </div>
  </div>
</template>
