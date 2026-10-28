<script setup lang="ts">
// "React" in the Talk's controls: the quick reactions plus every emoji,
// custom ones of the community included. A reaction floats over my tile for
// everyone in the Talk for a moment and is kept nowhere.
import { ref, watch, onBeforeUnmount } from 'vue'
import { Smile } from '@lucide/vue'
import ReactionPalette from './ReactionPalette.vue'
import { useStage } from '../composables/useStage'
import { useVoiceStore } from '../stores/voice'
import { useDismissable } from '../composables/useDismissable'

defineProps({
  buttonClass: { type: String, default: '' }
})
const emit = defineEmits<{ 'update:open': [open: boolean] }>()

const voice = useVoiceStore()
const stage = useStage(() => voice.currentChannelId)
const open = ref(false)
watch(open, value => emit('update:open', value), { immediate: true })

// A press elsewhere, Escape (wherever the focus is) and a quiet moment close
// the palette; so does a pick, the button opens it again for a second one.
const root = ref<HTMLElement | null>(null)
const IDLE_MS = 8000
let idle: ReturnType<typeof setTimeout> | undefined
useDismissable(root, () => { open.value = false }, { active: open })
function restartIdle() {
  clearTimeout(idle)
  if (open.value) idle = setTimeout(() => { open.value = false }, IDLE_MS)
}
watch(open, restartIdle)
onBeforeUnmount(() => clearTimeout(idle))

function pick(emoji: string) {
  stage.react(emoji)
  open.value = false
}
</script>

<template>
  <div ref="root" class="relative shrink-0" @pointermove="restartIdle" @keydown="restartIdle" @focusin="restartIdle" @input="restartIdle" @wheel.passive="restartIdle" @scroll.capture="restartIdle">
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
    <ReactionPalette v-if="open" sheet align="left" @pick="pick" @close="open = false" />
  </div>
</template>
