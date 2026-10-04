<script setup>
// Vertical drag handle sitting on a panel edge. `panel` is one entry returned
// by useResizable(); all sizing logic lives there.
defineProps({
  panel: { type: Object, required: true },
  label: { type: String, default: 'Breite anpassen' }
})
</script>

<template>
  <div
    role="separator"
    aria-orientation="vertical"
    :aria-label="label"
    :aria-valuenow="panel.width"
    :aria-valuemin="panel.min"
    :aria-valuemax="panel.maxNow"
    tabindex="0"
    :class="[
      'group/handle absolute inset-y-0 z-30 w-2 cursor-col-resize touch-none select-none outline-none',
      panel.side === 'left' ? '-right-1' : '-left-1'
    ]"
    @pointerdown="panel.startDrag"
    @keydown="panel.onKeydown"
    @dblclick="panel.reset"
  >
    <span
      :class="[
        'pointer-events-none absolute inset-y-0 left-1/2 w-0.5 -translate-x-1/2 transition-colors duration-150',
        panel.dragging
          ? 'bg-mnema-accent'
          : 'bg-transparent group-hover/handle:bg-mnema-accent/60 group-focus-visible/handle:bg-mnema-accent'
      ]"
    ></span>
  </div>
</template>
