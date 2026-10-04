<script setup>
import { X } from 'lucide-vue-next'

// "Antwort an <Name>" strip that sits on top of a composer while replying.
defineProps({
  target: { type: Object, required: true }
})

defineEmits(['cancel'])
</script>

<template>
  <div class="flex items-center justify-between gap-2 px-4 h-9 bg-mnema-raised border border-b-0 border-mnema-border rounded-t-lg text-sm text-mnema-muted">
    <span class="truncate min-w-0">
      Antwort an
      <span class="font-semibold text-mnema-text">{{ target.display_name || target.username }}</span>
    </span>
    <button
      type="button"
      class="w-6 h-6 flex items-center justify-center flex-shrink-0 rounded-full text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
      title="Antworten abbrechen (Esc)"
      @click="$emit('cancel')"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
