<script setup lang="ts">
// "React" in the Talk's controls: the quick reactions plus every emoji,
// custom ones of the community included. A reaction floats over my tile for
// everyone in the Talk for a moment and is kept nowhere.
import { ref, watch } from 'vue'
import { Smile } from '@lucide/vue'
import ReactionPalette from './ReactionPalette.vue'
import { useStage } from '../composables/useStage'
import { useVoiceStore } from '../stores/voice'

defineProps({
  buttonClass: { type: String, default: '' }
})
const emit = defineEmits<{ 'update:open': [open: boolean] }>()

const voice = useVoiceStore()
const stage = useStage(() => voice.currentChannelId)
const open = ref(false)
watch(open, value => emit('update:open', value), { immediate: true })

function pick(emoji: string) {
  stage.react(emoji)
  // The palette stays open for a second reaction; Escape or the button closes it.
}
</script>

<template>
  <div class="relative shrink-0" @keydown.esc="open = false">
    <button
      type="button"
      data-testid="talk-react"
      :class="[buttonClass, open ? 'bg-mnema-hover text-mnema-text' : '']"
      aria-haspopup="dialog"
      :aria-expanded="open ? 'true' : 'false'"
      v-tooltip="$t('talk.react')"
      :aria-label="$t('talk.react')"
      @click="open = !open"
    >
      <Smile class="w-5 h-5" aria-hidden="true" />
    </button>
    <ReactionPalette v-if="open" align="left" @pick="pick" @close="open = false" />
  </div>
</template>
