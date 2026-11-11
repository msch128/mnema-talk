<script setup lang="ts">
import type { PropType } from 'vue'
import type { Category } from '../types/domain'
import { useCompactLayout } from '../shared/composables/useCompactLayout'
import type { ContextMenuItem } from '../shared/components/menuTypes'
import RowActionsMenu from '../shared/components/RowActionsMenu.vue'
import { t } from '../i18n'
import { Trash2, Edit2, ArrowUp, ArrowDown, GripVertical, Plus } from '@lucide/vue'

// The header of a category in the layout editor: name, count and its arrows, rename and delete.
const props = defineProps({
  category: { type: Object as PropType<Category>, required: true },
  index: { type: Number, required: true },
  count: { type: Number, required: true }
})
const emit = defineEmits<{ move: [direction: number]; rename: []; delete: []; 'add-channel': [] }>()

const { isCompact } = useCompactLayout()
// On a phone the icons have no tooltip: the same actions as one labelled menu.
function menuItems(): ContextMenuItem[] {
  const name = props.category.name
  return [
    { id: 'add', label: t('admin.addChannelTo', { name }), icon: Plus, action: () => emit('add-channel') },
    { id: 'up', label: t('admin.moveUpNamed', { name }), icon: ArrowUp, disabled: props.index === 0, action: () => emit('move', -1) },
    { id: 'down', label: t('admin.moveDownNamed', { name }), icon: ArrowDown, disabled: props.index === props.count - 1, action: () => emit('move', 1) },
    { id: 'rename', label: t('admin.renameCategoryNamed', { name }), icon: Edit2, action: () => emit('rename') },
    { id: 'delete', label: t('admin.deleteCategoryNamed', { name }), icon: Trash2, danger: true, action: () => emit('delete') }
  ]
}
</script>

<template>
  <div class="flex items-center justify-between gap-2 border-b border-mnema-hairline/60 pb-2">
    <div class="flex items-center gap-2 min-w-0">
      <GripVertical class="w-4 h-4 text-mnema-tertiary cursor-grab active:cursor-grabbing shrink-0" />
      <span class="text-xs font-semibold uppercase font-mono tracking-wider text-mnema-text truncate">{{ category.name }}</span>
      <span class="text-xs text-mnema-tertiary font-mono">({{ category.channels?.length || 0 }})</span>
    </div>

    <RowActionsMenu v-if="isCompact" :items="menuItems()" :label="$t('admin.rowActionsNamed', { name: category.name })" />
    <div v-else class="flex items-center gap-1 shrink-0">
      <button
        data-testid="add-channel-category"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('channel.create')"
        :aria-label="$t('admin.addChannelTo', { name: category.name })"
        @click="emit('add-channel')"
      >
        <Plus class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="move-up-category"
        type="button"
        :disabled="index === 0"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveUpNamed', { name: category.name })"
        :aria-label="$t('admin.moveUpNamed', { name: category.name })"
        @click="emit('move', -1)"
      >
        <ArrowUp class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="move-down-category"
        type="button"
        :disabled="index === count - 1"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveDownNamed', { name: category.name })"
        :aria-label="$t('admin.moveDownNamed', { name: category.name })"
        @click="emit('move', 1)"
      >
        <ArrowDown class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="rename-category"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.renameCategoryNamed', { name: category.name })"
        :aria-label="$t('admin.renameCategoryNamed', { name: category.name })"
        @click="emit('rename')"
      >
        <Edit2 class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="delete-category"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-danger"
        :title="$t('admin.deleteCategoryNamed', { name: category.name })"
        :aria-label="$t('admin.deleteCategoryNamed', { name: category.name })"
        @click="emit('delete')"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</template>
