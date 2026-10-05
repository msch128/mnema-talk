<script setup>
import { X } from '@lucide/vue'

// "Replying to <name>" strip that sits on top of a composer while replying.
defineProps({
  target: { type: Object, required: true }
})

defineEmits(['cancel'])
</script>

<template>
  <div class="flex items-center justify-between gap-2 px-4 h-9 bg-mnema-raised border border-b-0 border-mnema-border rounded-t-lg text-sm text-mnema-muted">
    <span class="truncate min-w-0">
      {{ $t('chat.replyingTo') }}
      <span class="font-semibold text-mnema-text">{{ target.display_name || target.username }}</span>
    </span>
    <button
      type="button"
      class="w-6 h-6 flex items-center justify-center flex-shrink-0 rounded-full text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
      v-tooltip="{ text: $t('chat.cancelReply'), shortcut: 'Esc' }"
      @click="$emit('cancel')"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
