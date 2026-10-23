<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel } from '../types/domain'
import { Trash2, Edit2, ArrowUp, ArrowDown, Hash, Volume2, GripVertical } from '@lucide/vue'

// One channel of the layout editor: draggable, with arrows, rename and delete.
// Its drag events stop here, so the category around it keeps only its own.
defineProps({
  channel: { type: Object as PropType<Channel>, required: true },
  index: { type: Number, required: true },
  count: { type: Number, required: true }
})
const emit = defineEmits<{
  move: [direction: number]
  rename: []
  delete: []
  dragstart: [event: DragEvent]
  dragend: []
  drop: [event: DragEvent]
}>()
</script>

<template>
  <div
    :data-testid="`channel-item-${channel.id}`"
    draggable="true"
    class="flex items-center justify-between gap-2 p-2 rounded-md bg-mnema-canvas border border-mnema-hairline hover:border-mnema-border transition-colors group cursor-grab active:cursor-grabbing"
    @dragstart.stop="emit('dragstart', $event)"
    @dragend.stop="emit('dragend')"
    @dragover.prevent.stop
    @drop.stop="emit('drop', $event)"
  >
    <div class="flex items-center gap-2 min-w-0 flex-1">
      <GripVertical class="w-4 h-4 text-mnema-tertiary group-hover:text-mnema-muted shrink-0" />
      <Hash v-if="channel.type === 'text'" class="w-4 h-4 text-mnema-tertiary shrink-0" />
      <Volume2 v-else class="w-4 h-4 text-mnema-accent shrink-0" />
      <span class="text-sm font-medium text-mnema-text truncate">{{ channel.name }}</span>
      <span v-if="channel.topic" class="max-md:hidden text-xs text-mnema-tertiary truncate">({{ channel.topic }})</span>
    </div>

    <div class="flex items-center gap-1 shrink-0">
      <button
        data-testid="move-up-channel"
        type="button"
        :disabled="index === 0"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveUp')"
        :aria-label="$t('admin.moveUp')"
        @click.stop="emit('move', -1)"
      >
        <ArrowUp class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="move-down-channel"
        type="button"
        :disabled="index === count - 1"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveDown')"
        :aria-label="$t('admin.moveDown')"
        @click.stop="emit('move', 1)"
      >
        <ArrowDown class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="rename-channel"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.renameChannel')"
        :aria-label="$t('admin.renameChannel')"
        @click.stop="emit('rename')"
      >
        <Edit2 class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="delete-channel"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-danger"
        :title="$t('sidebar.deleteChannel')"
        :aria-label="$t('sidebar.deleteChannel')"
        @click.stop="emit('delete')"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</template>
