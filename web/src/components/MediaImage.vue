<script setup lang="ts">
// A picture from the media endpoint that knows when it is gone. While it
// loads it is an ordinary <img> (every attribute of the caller goes to it).
// When it fails, the address is asked why (see lib/mediaStatus): deleted for
// good shows `fallback`, which by default is the calm "This image was deleted"
// box; a failure that may pass shows a box that loads it again. A caller that
// has a better stand-in (initials, :name:, the default banner) fills `fallback`.
import { useMediaImage } from '../composables/useMediaImage'
import MediaPlaceholder from './MediaPlaceholder.vue'

defineOptions({ inheritAttrs: false })
const props = defineProps({
  src: { type: String, required: true },
  // The server already said so (the attachment's is_deleted).
  deleted: { type: Boolean, default: false },
  // Classes of the default box: the size the picture would have had.
  placeholderClass: { type: String, default: '' },
  compact: { type: Boolean, default: false },
  // Only a retry is wanted when the stand-in cannot offer one itself.
  retryable: { type: Boolean, default: true }
})

const { state, attempt, onError, retry } = useMediaImage(() => props.src, () => props.deleted)
</script>

<template>
  <img v-if="state === 'ok'" :key="attempt" v-bind="$attrs" :src="src" @error="onError">
  <slot v-else name="fallback" :state="state" :retry="retry">
    <MediaPlaceholder
      :state="state === 'gone' || !retryable ? 'gone' : 'failed'"
      :compact="compact"
      :class="placeholderClass"
      @retry="retry"
    />
  </slot>
</template>
