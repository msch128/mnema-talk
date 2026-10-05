<script setup>
// Smiley button that opens the emoji picker above it.
import { ref } from 'vue'
import { Smile } from '@lucide/vue'
import EmojiPicker from './EmojiPicker.vue'

defineProps({
  // Which side of the button the picker aligns to.
  align: { type: String, default: 'right' },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['pick'])

const open = ref(false)
const button = ref(null)

function pick(emoji) {
  open.value = false
  emit('pick', emoji)
}
</script>

<template>
  <div class="relative flex-shrink-0">
    <button
      ref="button"
      type="button"
      data-testid="emoji-button"
      :disabled="disabled"
      v-tooltip="$t('emoji.open')"
      :aria-label="$t('emoji.open')"
      aria-haspopup="dialog"
      :aria-expanded="open ? 'true' : 'false'"
      :class="[
        'flex h-8 w-8 items-center justify-center rounded-full transition disabled:opacity-50',
        open ? 'bg-mnema-surface text-mnema-accent' : 'text-mnema-tertiary hover:bg-mnema-surface hover:text-mnema-text'
      ]"
      @click="open = !open"
    >
      <Smile class="h-5 w-5" />
    </button>
    <EmojiPicker
      v-if="open"
      :trigger="button"
      :class="['absolute bottom-full mb-2', align === 'left' ? 'left-0' : 'right-0']"
      @pick="pick"
      @close="open = false"
    />
  </div>
</template>
