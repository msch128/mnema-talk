<script setup lang="ts">
import type { Component, PropType } from 'vue'
// The icon of a two-state control (muted or not, camera on or off). When the
// state flips, the new icon sharpens into place while the old one blurs and
// shrinks away in the same spot (styles/motion.css, .icon-swap); with reduced
// motion it is a plain swap. The control itself carries the accessible name.
defineProps({
  on: { type: Boolean, required: true },
  onIcon: { type: Object as PropType<Component>, required: true },
  offIcon: { type: Object as PropType<Component>, required: true },
  iconClass: { type: String, default: 'h-5 w-5' }
})
</script>

<template>
  <span class="icon-swap" aria-hidden="true">
    <Transition name="icon-swap">
      <component :is="on ? onIcon : offIcon" :key="on ? 'on' : 'off'" :class="iconClass" />
    </Transition>
  </span>
</template>
