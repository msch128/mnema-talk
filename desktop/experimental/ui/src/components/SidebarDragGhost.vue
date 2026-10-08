<script setup lang="ts">
import type { PropType } from 'vue'
// The channel or category being dragged, following the pointer (above the
// finger on touch, so it stays visible).
import { computed } from 'vue'
import { ChevronDown, Hash, Volume2 } from '@lucide/vue'

const props = defineProps({
  item: { type: Object as PropType<{ kind: 'channel' | 'category'; name: string; type?: 'text' | 'voice' }>, required: true }, // { kind, name, type }
  x: { type: Number, default: 0 },
  y: { type: Number, default: 0 },
  touch: { type: Boolean, default: false }
})

const style = computed(() => {
  const x = props.x + (props.touch ? -24 : 14)
  const y = props.y + (props.touch ? -56 : 10)
  return { transform: `translate3d(${x}px, ${y}px, 0)` }
})
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed left-0 top-0 z-[70] pointer-events-none max-w-[240px] h-8 px-2.5 flex items-center gap-1.5 rounded-md bg-mnema-elevated border border-mnema-border shadow-xl text-sm text-mnema-text"
      :style="style"
      data-testid="drag-ghost"
      aria-hidden="true"
    >
      <ChevronDown v-if="item.kind === 'category'" class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary" />
      <Volume2 v-else-if="item.type === 'voice'" class="w-4 h-4 flex-shrink-0 text-mnema-tertiary" />
      <Hash v-else class="w-4 h-4 flex-shrink-0 text-mnema-tertiary" />
      <span :class="['truncate', item.kind === 'category' && 'text-xs font-semibold uppercase tracking-wide']">{{ item.name }}</span>
    </div>
  </Teleport>
</template>
