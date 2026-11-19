<script setup lang="ts">
// Wraps a menu or popover that comes and goes with v-if. It arrives with
// .pop-in (it grows out of its trigger) and leaves with .pop-out: back
// towards the trigger, about 20 % faster (styles/motion.css). While it leaves
// it is hidden from assistive technology and takes no input, so only the menu
// that is open counts; opening it again cuts the leaving one short.
function onBeforeLeave(el: Element) {
  el.setAttribute('aria-hidden', 'true')
  if (el instanceof HTMLElement) el.inert = true
}
</script>

<template>
  <Transition leave-active-class="pop-out" @before-leave="onBeforeLeave">
    <slot></slot>
  </Transition>
</template>
