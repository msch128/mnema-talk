<script setup>
import { computed } from 'vue'

// Drag handle sitting on a panel edge: a vertical bar for side panels, a
// horizontal one for top/bottom panels (axis 'y'). `panel` is one entry
// returned by useResizable(); all sizing logic lives there.
const props = defineProps({
  panel: { type: Object, required: true },
  label: { type: String, default: '' }
})

const horizontal = computed(() => props.panel.axis === 'y')
const edge = {
  left: '-right-1',
  right: '-left-1',
  top: '-bottom-1',
  bottom: '-top-1'
}
</script>

<template>
  <div
    role="separator"
    :aria-orientation="horizontal ? 'horizontal' : 'vertical'"
    :aria-label="label || $t('resize.default')"
    :aria-valuenow="panel.width"
    :aria-valuemin="panel.min"
    :aria-valuemax="panel.maxNow"
    tabindex="0"
    :class="[
      'group/handle absolute z-30 touch-none select-none outline-none',
      horizontal ? 'inset-x-0 h-2 cursor-row-resize' : 'inset-y-0 w-2 cursor-col-resize',
      edge[panel.side] || '-left-1'
    ]"
    @pointerdown="panel.startDrag"
    @keydown="panel.onKeydown"
    @dblclick="panel.reset"
  >
    <span
      :class="[
        'pointer-events-none absolute transition-colors duration-150',
        horizontal ? 'inset-x-0 top-1/2 h-0.5 -translate-y-1/2' : 'inset-y-0 left-1/2 w-0.5 -translate-x-1/2',
        panel.dragging
          ? 'bg-mnema-accent'
          : 'bg-transparent group-hover/handle:bg-mnema-accent/60 group-focus-visible/handle:bg-mnema-accent'
      ]"
    ></span>
  </div>
</template>
