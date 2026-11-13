<script setup lang="ts">
import type { PropType } from 'vue'
import type { Channel } from '../../types/domain'
import { useCompactLayout } from '../../shared/composables/useCompactLayout'
import type { ContextMenuItem } from '../../shared/components/menuTypes'
import RowActionsMenu from '../../shared/components/RowActionsMenu.vue'
import { t } from '../../i18n'
import { Trash2, Edit2, ArrowUp, ArrowDown, Hash, Volume2, MessagesSquare, GripVertical } from '@lucide/vue'

// One channel of the layout editor: draggable, with arrows, rename and delete.
// Its drag events stop here, so the category around it keeps only its own.
const props = defineProps({
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

const { isCompact } = useCompactLayout()
// On a phone the icons have no tooltip: the same actions as one labelled menu.
function menuItems(): ContextMenuItem[] {
  const name = props.channel.name
  return [
    { id: 'up', label: t('admin.moveUpNamed', { name }), icon: ArrowUp, disabled: props.index === 0, action: () => emit('move', -1) },
    { id: 'down', label: t('admin.moveDownNamed', { name }), icon: ArrowDown, disabled: props.index === props.count - 1, action: () => emit('move', 1) },
    { id: 'rename', label: t('admin.renameChannelNamed', { name }), icon: Edit2, action: () => emit('rename') },
    { id: 'delete', label: t('admin.deleteChannelNamed', { name }), icon: Trash2, danger: true, action: () => emit('delete') }
  ]
}
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
    <!-- A container: the topic steps aside where the row is too narrow for it beside the name. -->
    <div class="@container flex items-center gap-2 min-w-0 flex-1">
      <GripVertical class="w-4 h-4 text-mnema-tertiary group-hover:text-mnema-muted shrink-0" />
      <Hash v-if="channel.type === 'text'" class="w-4 h-4 text-mnema-tertiary shrink-0" />
      <Volume2 v-else-if="channel.type === 'voice'" class="w-4 h-4 text-mnema-accent shrink-0" />
      <MessagesSquare v-else class="w-4 h-4 text-mnema-accent shrink-0" />
      <span v-full-text class="min-w-[3em] text-sm font-medium text-mnema-text truncate">{{ channel.name }}</span>
      <span v-full-text v-if="channel.topic" class="min-w-[3em] max-md:hidden @max-[28rem]:hidden text-xs text-mnema-tertiary truncate">({{ channel.topic }})</span>
    </div>

    <RowActionsMenu v-if="isCompact" :items="menuItems()" :label="$t('admin.rowActionsNamed', { name: channel.name })" />
    <div v-else class="flex items-center gap-1 shrink-0">
      <button
        data-testid="move-up-channel"
        type="button"
        :disabled="index === 0"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveUpNamed', { name: channel.name })"
        :aria-label="$t('admin.moveUpNamed', { name: channel.name })"
        @click.stop="emit('move', -1)"
      >
        <ArrowUp class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="move-down-channel"
        type="button"
        :disabled="index === count - 1"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.moveDownNamed', { name: channel.name })"
        :aria-label="$t('admin.moveDownNamed', { name: channel.name })"
        @click.stop="emit('move', 1)"
      >
        <ArrowDown class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="rename-channel"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-text"
        :title="$t('admin.renameChannelNamed', { name: channel.name })"
        :aria-label="$t('admin.renameChannelNamed', { name: channel.name })"
        @click.stop="emit('rename')"
      >
        <Edit2 class="w-3.5 h-3.5" />
      </button>
      <button
        data-testid="delete-channel"
        type="button"
        class="btn btn-icon btn-sm hover:text-mnema-danger"
        :title="$t('admin.deleteChannelNamed', { name: channel.name })"
        :aria-label="$t('admin.deleteChannelNamed', { name: channel.name })"
        @click.stop="emit('delete')"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</template>
