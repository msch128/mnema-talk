<script setup lang="ts">
import type { PropType } from 'vue'
import type { ContextMenuItem } from '../../shared/components/menuTypes'
import { ref } from 'vue'
import { Plus, ChartNoAxesColumn } from '@lucide/vue'
import ContextMenu from '../../shared/components/ContextMenu.vue'

// The tools in front of the composer's field: "+" (a file, or on a phone the
// menu with file, poll and "send later") and the poll button.
const props = defineProps({
  hasPlus: { type: Boolean, default: false },
  compact: { type: Boolean, default: false },
  canSend: { type: Boolean, default: false },
  sending: { type: Boolean, default: false },
  uploadLabel: { type: String, default: '' },
  plusItems: { type: Array as PropType<ContextMenuItem[]>, default: () => [] }
})
const emit = defineEmits<{ upload: []; poll: [] }>()

const plusButton = ref<HTMLElement | null>(null)
const plusMenuOpen = ref(false)
function onPlus() {
  if (props.compact) plusMenuOpen.value = !plusMenuOpen.value
  else emit('upload')
}
</script>

<template>
  <button
    v-if="hasPlus"
    ref="plusButton"
    type="button"
    data-testid="composer-plus"
    @click="onPlus"
    :disabled="compact ? false : sending"
    :aria-haspopup="compact ? 'menu' : undefined"
    :aria-expanded="compact ? (plusMenuOpen ? 'true' : 'false') : undefined"
    class="focus-ring w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
    v-tooltip="uploadLabel"
    :aria-label="uploadLabel"
  >
    <Plus class="w-5 h-5" />
  </button>
  <ContextMenu v-if="compact" v-model="plusMenuOpen" :anchor="plusButton" :items="plusItems" :aria-label="$t('chat.moreActions')" />

  <button
    v-if="canSend && !compact"
    type="button"
    data-testid="poll-button"
    @click="emit('poll')"
    :disabled="sending"
    class="focus-ring w-8 h-8 max-md:w-11 max-md:h-11 flex items-center justify-center shrink-0 rounded-full hover:bg-mnema-surface text-mnema-tertiary hover:text-mnema-text transition disabled:opacity-50"
    v-tooltip="$t('poll.create')"
    :aria-label="$t('poll.create')"
  >
    <ChartNoAxesColumn class="w-5 h-5" />
  </button>
</template>
