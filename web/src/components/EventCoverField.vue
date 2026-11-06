<script setup lang="ts">
import { ref } from 'vue'
import { Image as ImageIcon, Trash2 } from '@lucide/vue'

// The cover picker of the event dialog: the file input stays out of the tab order
// and a real button opens it, so the keyboard can reach it.
defineProps<{ preview: string | null }>()
const emit = defineEmits<{ pick: [file: File]; remove: [] }>()
const fileInput = ref<HTMLInputElement | null>(null)

function onSelected(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (file) emit('pick', file)
}
</script>

<template>
  <div>
    <span class="mb-1 block text-sm font-medium text-mnema-text">
      {{ $t('events.coverImage') }}
    </span>
    <div v-if="preview" class="relative mb-2 aspect-[3/1] w-full shrink-0 overflow-hidden rounded-lg border border-mnema-border bg-mnema-surface">
      <img :src="preview" :alt="$t('events.coverAlt')" class="h-full w-full object-cover" />
      <button
        type="button"
        :aria-label="$t('events.removeCover')"
        class="absolute top-2 right-2 rounded-full bg-black/60 p-1.5 text-white hover:bg-black/80 transition-colors"
        @click="emit('remove')"
      >
        <Trash2 class="w-4 h-4" />
      </button>
    </div>
    <div class="flex items-center gap-3">
      <input ref="fileInput" type="file" accept="image/*" tabindex="-1" aria-hidden="true" data-testid="event-cover-file" class="sr-only" @change="onSelected" />
      <button
        type="button"
        data-testid="event-cover-choose"
        class="flex cursor-pointer items-center gap-2 rounded-lg border border-mnema-border bg-mnema-surface px-3 py-1.5 text-sm font-medium text-mnema-text hover:bg-mnema-hover transition-colors"
        @click="fileInput?.click()"
      >
        <ImageIcon class="w-4 h-4 text-mnema-tertiary" />
        <span>{{ preview ? $t('events.changeCover') : $t('events.uploadCover') }}</span>
      </button>
      <button v-if="preview" type="button" class="text-sm text-red-400 hover:underline" @click="emit('remove')">
        {{ $t('events.removeCover') }}
      </button>
    </div>
  </div>
</template>
