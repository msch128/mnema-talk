<script setup lang="ts">
import type { PropType } from 'vue'
// What stands where a picture cannot be shown: a calm box saying the image was
// deleted, or that it could not be loaded, with a way to try again. The parent
// gives it the size the picture would have had; `compact` is for thumbnails
// too small for words (the text then names the box for screen readers and
// tooltips).
import { ImageOff, RotateCw } from '@lucide/vue'

defineProps({
  state: { type: String as PropType<'gone' | 'failed' | 'none'>, required: true },
  compact: { type: Boolean, default: false }
})
const emit = defineEmits<{ retry: [] }>()
</script>

<template>
  <div
    v-if="state === 'gone'"
    role="img"
    :aria-label="$t('media.imageDeleted')"
    :title="compact ? $t('media.imageDeleted') : undefined"
    data-media-placeholder="gone"
    :class="['flex items-center justify-center rounded-lg border border-mnema-border bg-mnema-surface text-mnema-tertiary', compact ? '' : 'flex-col gap-2 p-3 text-center']"
  >
    <ImageOff :class="compact ? 'h-5 w-5' : 'h-6 w-6'" aria-hidden="true" />
    <span v-if="!compact" class="text-sm text-mnema-muted">{{ $t('media.imageDeleted') }}</span>
  </div>
  <button
    v-else-if="state === 'failed'"
    type="button"
    data-media-placeholder="failed"
    :aria-label="compact ? `${$t('media.imageFailed')}. ${$t('media.retry')}` : undefined"
    :title="compact ? $t('media.retry') : undefined"
    :class="['flex items-center justify-center rounded-lg border border-mnema-border bg-mnema-surface text-mnema-tertiary transition hover:bg-mnema-hover hover:text-mnema-text focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent', compact ? '' : 'flex-col gap-2 p-3 text-center']"
    @click.stop.prevent="emit('retry')"
  >
    <RotateCw class="h-5 w-5" aria-hidden="true" />
    <template v-if="!compact">
      <span class="text-sm text-mnema-muted">{{ $t('media.imageFailed') }}</span>
      <span class="text-xs text-mnema-accent">{{ $t('media.retry') }}</span>
    </template>
  </button>
</template>
