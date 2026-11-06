<script setup lang="ts">
import type { PropType } from 'vue'
import { Ellipsis } from '@lucide/vue'
import { useButtonMenu } from '../composables/useButtonMenu'
import type { ContextMenuItem } from './menuTypes'
import ContextMenu from './ContextMenu.vue'

// The actions of one admin row on a phone, where icon-only buttons have no tooltip and
// so no name: a single "..." button that opens the same actions as a labelled menu.
// The parent builds the items from the labels it already gives its desktop icons.
const props = defineProps({
  items: { type: Array as PropType<ContextMenuItem[]>, required: true },
  label: { type: String, required: true }
})

const { menu, button, onPointerDown, toggle } = useButtonMenu(() => props.items)
</script>

<template>
  <button
    v-if="items.length"
    ref="button"
    type="button"
    data-testid="row-actions"
    aria-haspopup="menu"
    :aria-expanded="menu.state.open ? 'true' : 'false'"
    :aria-label="label"
    class="btn btn-icon max-md:h-10 max-md:w-10"
    @pointerdown="onPointerDown"
    @click.stop="toggle"
  >
    <Ellipsis class="h-5 w-5" />
  </button>
  <ContextMenu v-model="menu.state.open" :x="menu.state.x" :y="menu.state.y" :anchor="menu.state.anchor" :items="menu.items.value" :min-width="200" :aria-label="label" />
</template>
