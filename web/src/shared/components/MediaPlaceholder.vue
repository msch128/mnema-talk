<script setup lang="ts">
import type { PropType } from 'vue'
// What stands where a file cannot be shown: a calm tile in the surface colour
// saying the file was removed (by a moderator, auto-delete, retention, or
// missing in storage), or that a picture could not be loaded, with a way to
// try again. The parent gives it the size the file had; `compact` is for
// thumbnails too small for words (the text then names the tile for screen
// readers and tooltips). Drawn with the design system's tokens and an icon,
// never a bitmap.
import { computed } from 'vue'
import { FileX, ImageOff, RotateCw, VideoOff } from '@lucide/vue'
import { t } from '../../i18n'

const props = defineProps({
  state: { type: String as PropType<'gone' | 'failed' | 'none'>, required: true },
  compact: { type: Boolean, default: false },
  // What the file was, for an attachment: the icon follows it and the tile
  // says "File removed". Without it (a link card's picture) it says the image
  // was deleted.
  kind: { type: String as PropType<'' | 'image' | 'video' | 'file'>, default: '' },
  // The file's name, for screen readers.
  name: { type: String, default: '' }
})
const emit = defineEmits<{ retry: [] }>()

const icon = computed(() => ({ '': ImageOff, image: ImageOff, video: VideoOff, file: FileX })[props.kind])
const goneText = computed(() => (props.kind ? t('media.fileRemoved') : t('media.imageDeleted')))
const goneLabel = computed(() => (props.kind && props.name ? t('media.fileRemovedNamed', { name: props.name }) : goneText.value))
</script>

<template>
  <div
    v-if="state === 'gone'"
    role="img"
    :aria-label="goneLabel"
    :title="compact ? goneLabel : undefined"
    data-media-placeholder="gone"
    :class="['flex items-center justify-center rounded-lg border border-mnema-border bg-mnema-surface text-mnema-tertiary', compact ? '' : 'flex-col gap-2 p-3 text-center']"
  >
    <component :is="icon" :class="compact ? 'h-5 w-5' : 'h-6 w-6'" aria-hidden="true" />
    <span v-if="!compact" class="text-sm text-mnema-muted" aria-hidden="true">{{ goneText }}</span>
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
