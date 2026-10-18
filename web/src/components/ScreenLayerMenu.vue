<script setup lang="ts">
// What I receive of a screen share I watch: Auto or one of its layers (see
// useScreenLayerMenu). A button on the stage, shown only when the share has
// more than one layer.
import { watch } from 'vue'
import { Layers } from '@lucide/vue'
import ContextMenu from './ContextMenu.vue'
import { useButtonMenu } from '../composables/useButtonMenu'
import { useScreenLayerMenu } from '../composables/useScreenLayerMenu'
import { t } from '../i18n'

const props = withDefaults(defineProps<{
  userId: string | null
  /** Where the menu is attached; the full-screen stage, as the page behind it is not shown. */
  host?: HTMLElement | null
  /** Show the current choice next to the icon. */
  showLabel?: boolean
}>(), { host: null, showLabel: true })
// open-change: the menu opened or closed (the Talk keeps its controls shown).
const emit = defineEmits<{ 'open-change': [open: boolean] }>()

const layerMenu = useScreenLayerMenu(() => props.userId)
const { menu, button, onPointerDown, toggle } = useButtonMenu(() => layerMenu.menuItems())

watch(() => menu.state.open, open => emit('open-change', open))
// A share that loses its layers closes the menu.
watch(layerMenu.available, available => { if (!available) menu.state.open = false })
</script>

<template>
  <button
    v-if="layerMenu.available.value"
    ref="button"
    type="button"
    data-testid="screen-layer-button"
    aria-haspopup="menu"
    :aria-expanded="menu.state.open ? 'true' : 'false'"
    :aria-label="`${t('talk.layers.title')}: ${layerMenu.buttonLabel.value}`"
    v-tooltip="t('talk.layers.title')"
    class="h-8 px-2 flex items-center justify-center gap-1.5 rounded-lg bg-black/75 hover:bg-black/90 text-white transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/80"
    @pointerdown="onPointerDown"
    @click="toggle"
  >
    <Layers class="w-4 h-4" />
    <span v-if="props.showLabel" data-testid="screen-layer-label" class="text-xs font-semibold tabular-nums">{{ layerMenu.buttonLabel.value }}</span>
  </button>
  <ContextMenu
    v-model="menu.state.open"
    :x="menu.state.x"
    :y="menu.state.y"
    :anchor="menu.state.anchor"
    :items="menu.items.value"
    :min-width="240"
    :aria-label="t('talk.layers.title')"
    :teleport-to="props.host || 'body'"
  />
</template>
