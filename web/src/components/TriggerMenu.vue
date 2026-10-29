<script setup lang="ts">
import type { PropType } from 'vue'
// The menu of one useContextMenuTrigger: put it next to the element that binds `trigger.on`.
import ContextMenu from './ContextMenu.vue'
import type { useContextMenuTrigger } from '../composables/useContextMenuTrigger'

defineProps({
  trigger: { type: Object as PropType<ReturnType<typeof useContextMenuTrigger>>, required: true },
  ariaLabel: { type: String, default: '' },
  minWidth: { type: Number, default: 240 }
})
</script>

<template>
  <ContextMenu
    :model-value="trigger.menu.state.open"
    :x="trigger.menu.state.x"
    :y="trigger.menu.state.y"
    :anchor="trigger.menu.state.anchor"
    :items="trigger.menu.items.value"
    :min-width="minWidth"
    :aria-label="ariaLabel"
    @update:model-value="open => { if (!open) trigger.close() }"
  />
</template>
