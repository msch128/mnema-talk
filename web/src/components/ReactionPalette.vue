<script setup>
// Quick reactions plus "more", which opens the full emoji picker in place.
import { ref } from 'vue'
import { Plus } from '@lucide/vue'
import EmojiPicker from './EmojiPicker.vue'

defineProps({
  align: { type: String, default: 'right' }
})
const emit = defineEmits(['pick', 'close'])

const QUICK_REACTIONS = ['👍', '❤️', '😂', '🔥', '🎉', '🚀']
const full = ref(false)
</script>

<template>
  <EmojiPicker
    v-if="full"
    :class="['absolute bottom-full mb-1', align === 'left' ? 'left-0' : 'right-0']"
    @pick="emit('pick', $event)"
    @close="emit('close')"
  />
  <div
    v-else
    data-testid="reaction-palette"
    :class="[
      'absolute bottom-full mb-1 flex items-center gap-1 bg-mnema-elevated border border-mnema-border rounded-lg p-1.5 shadow-xl z-30 after:absolute after:top-full after:left-0 after:right-0 after:h-2 after:content-[\'\']',
      align === 'left' ? 'left-0' : 'right-0'
    ]"
  >
    <button
      v-for="emoji in QUICK_REACTIONS"
      :key="emoji"
      type="button"
      class="hover:scale-125 transition p-1 text-base rounded hover:bg-mnema-surface active:scale-95"
      @click.stop="emit('pick', emoji)"
    >
      {{ emoji }}
    </button>
    <button
      type="button"
      data-testid="reaction-more"
      v-tooltip="$t('emoji.more')"
      :aria-label="$t('emoji.more')"
      class="flex h-7 w-7 items-center justify-center rounded text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text"
      @click.stop="full = true"
    >
      <Plus class="h-4 w-4" />
    </button>
  </div>
</template>
