<script setup lang="ts">
import type { PropType } from 'vue'
import type { Category } from '../types/domain'
import { Trash2, Edit2, ArrowUp, ArrowDown, GripVertical } from '@lucide/vue'

// The header of a category in the layout editor: name, count and its arrows, rename and delete.
defineProps({
  category: { type: Object as PropType<Category>, required: true },
  index: { type: Number, required: true },
  count: { type: Number, required: true }
})
const emit = defineEmits<{ move: [direction: number]; rename: []; delete: [] }>()
</script>

<template>
  <div class="flex items-center justify-between gap-2 border-b border-mnema-hairline/60 pb-2">
    <div class="flex items-center gap-2 min-w-0">
      <GripVertical class="w-4 h-4 text-mnema-tertiary cursor-grab active:cursor-grabbing shrink-0" />
      <span class="text-xs font-semibold uppercase font-mono tracking-wider text-mnema-text truncate">{{ category.name }}</span>
      <span class="text-xs text-mnema-tertiary font-mono">({{ category.channels?.length || 0 }})</span>
    </div>

    <div class="flex items-center gap-1 shrink-0">
      <button
        data-testid="move-up-category"
        type="button"
        :disabled="index === 0"
        class="p-1 rounded-sm text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition disabled:opacity-30"
        :title="$t('admin.moveUp')"
        :aria-label="$t('admin.moveUp')"
        @click="emit('move', -1)"
      >
        <ArrowUp class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="move-down-category"
        type="button"
        :disabled="index === count - 1"
        class="p-1 rounded-sm text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition disabled:opacity-30"
        :title="$t('admin.moveDown')"
        :aria-label="$t('admin.moveDown')"
        @click="emit('move', 1)"
      >
        <ArrowDown class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="rename-category"
        type="button"
        class="p-1 rounded-sm text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition"
        :title="$t('admin.renameCategory')"
        :aria-label="$t('admin.renameCategory')"
        @click="emit('rename')"
      >
        <Edit2 class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="delete-category"
        type="button"
        class="p-1 rounded-sm text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-elevated transition"
        :title="$t('sidebar.deleteCategory')"
        :aria-label="$t('sidebar.deleteCategory')"
        @click="emit('delete')"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</template>
