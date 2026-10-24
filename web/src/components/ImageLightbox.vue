<script setup lang="ts">
import { ref } from 'vue'
import { ExternalLink, X } from '@lucide/vue'
import { useDialog } from '../composables/useDialog'

defineProps({ src: { type: String, required: true } })
const emit = defineEmits<{ close: [] }>()

const root = ref<HTMLElement | null>(null)
useDialog(root, { onClose: () => emit('close'), initialFocus: '[data-autofocus]' })
</script>

<template>
  <div
    ref="root"
    role="dialog"
    aria-modal="true"
    :aria-label="$t('chat.enlargedImage')"
    tabindex="-1"
    class="fixed inset-0 z-50 flex cursor-pointer items-center justify-center bg-black/90 p-4 focus:outline-hidden"
    @click="emit('close')"
  >
    <button
      type="button"
      data-autofocus
      data-dialog-close
      v-tooltip="$t('common.close')"
      class="absolute right-4 top-4 flex h-10 w-10 items-center justify-center rounded-md text-mnema-text hover:bg-mnema-hover"
      @click.stop="emit('close')"
    >
      <X class="h-5 w-5" />
    </button>
    <a
      :href="src"
      target="_blank"
      rel="noopener noreferrer"
      class="absolute right-16 top-4 flex h-10 items-center gap-2 rounded-md px-3 text-sm text-mnema-text hover:bg-mnema-hover"
      @click.stop
    >
      <ExternalLink class="h-4 w-4" />
      {{ $t('chat.openOriginal') }}
    </a>
    <img
      :src="src"
      :alt="$t('chat.enlargedImage')"
      class="max-h-[90vh] max-w-[90vw] rounded-lg border border-mnema-border object-contain shadow-2xl"
      @click.stop
    />
  </div>
</template>
