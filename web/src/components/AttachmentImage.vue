<script setup lang="ts">
import type { PropType } from 'vue'
import type { MediaAttachment } from '../types/domain'
// One image under a message: a button that opens it, or, when it was deleted
// from storage (the attachment says so, or the file is no longer served), a
// box of the size the image would have had saying so. A failure that may pass
// shows a box that tries again.
import { useMediaImage } from '../composables/useMediaImage'
import MediaPlaceholder from './MediaPlaceholder.vue'

defineOptions({ inheritAttrs: false })
const props = defineProps({
  attachment: { type: Object as PropType<MediaAttachment>, required: true },
  buttonClass: { type: [String, Array] as PropType<string | unknown[]>, default: '' },
  imageClass: { type: String, default: '' },
  placeholderClass: { type: String, default: '' },
  compact: { type: Boolean, default: false },
  decoding: { type: String as PropType<'async' | 'auto' | 'sync'>, default: undefined }
})
const emit = defineEmits<{ open: [] }>()

const { state, attempt, onError, retry } = useMediaImage(() => props.attachment.url, () => props.attachment.is_deleted)
</script>

<template>
  <!-- The image's alt text (file name) names the button. -->
  <button
    v-if="state === 'ok'"
    type="button"
    v-bind="$attrs"
    :class="[buttonClass, 'focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent']"
    @click="emit('open')"
  >
    <img
      :key="attempt"
      :src="attachment.url"
      :alt="attachment.original_filename"
      :title="attachment.original_filename"
      :class="imageClass"
      loading="lazy"
      :decoding="decoding"
      @error="onError"
    />
    <slot />
  </button>
  <MediaPlaceholder v-else :state="state" :compact="compact" :class="placeholderClass" @retry="retry" />
</template>
