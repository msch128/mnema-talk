<script setup lang="ts">
import { getCurrentInstance, onBeforeUnmount, onMounted, ref } from 'vue'
import { Upload } from '@lucide/vue'
import { dragHasFiles, filesOf } from '../lib/composerFiles'

// A calm veil over the chat while files are dragged onto it. It listens on its
// parent (which must be positioned) and hands the dropped files on.
const emit = defineEmits<{ files: [files: File[]] }>()
const dragging = ref(false)
let depth = 0
let host: HTMLElement | null = null

function enter(e: DragEvent) {
  if (!dragHasFiles(e.dataTransfer)) return
  e.preventDefault()
  depth++
  dragging.value = true
}
function over(e: DragEvent) {
  if (!dragHasFiles(e.dataTransfer)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
}
function leave(e: DragEvent) {
  if (!dragHasFiles(e.dataTransfer)) return
  depth = Math.max(0, depth - 1)
  if (!depth) dragging.value = false
}
function drop(e: DragEvent) {
  if (!dragHasFiles(e.dataTransfer)) return
  e.preventDefault()
  depth = 0
  dragging.value = false
  emit('files', filesOf(e.dataTransfer))
}

const events = { dragenter: enter, dragover: over, dragleave: leave, drop } as const
onMounted(() => {
  host = getCurrentInstance()?.proxy?.$el?.parentElement ?? null
  for (const [name, fn] of Object.entries(events)) host?.addEventListener(name, fn as EventListener)
})
onBeforeUnmount(() => {
  for (const [name, fn] of Object.entries(events)) host?.removeEventListener(name, fn as EventListener)
})
</script>

<template>
  <div
    v-show="dragging"
    data-testid="file-drop-overlay"
    class="pointer-events-none absolute inset-0 z-40 flex items-center justify-center bg-mnema-canvas/80 p-4"
    aria-hidden="true"
  >
    <div class="flex items-center gap-2 rounded-lg border border-dashed border-mnema-border-strong bg-mnema-elevated px-4 py-3 text-sm text-mnema-muted">
      <Upload class="w-4 h-4 text-mnema-accent" />
      {{ $t('chat.dropFiles') }}
    </div>
  </div>
</template>
