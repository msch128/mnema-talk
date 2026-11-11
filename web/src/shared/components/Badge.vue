<script setup lang="ts">
import type { PropType } from 'vue'
// The one count pill: red for what addresses you (mentions, direct messages),
// a neutral raised pill for plain unread, accent for the small count on a
// header icon. Size and shape are the same everywhere; placement is the
// caller's (class and data-* attributes fall through to the pill).
type BadgeVariant = 'mention' | 'unread' | 'info'

defineProps({
  variant: { type: String as PropType<BadgeVariant>, default: 'unread' }
})

const VARIANTS: Record<BadgeVariant, string> = {
  mention: 'bg-mnema-danger text-white font-bold',
  unread: 'bg-mnema-hover text-mnema-muted font-semibold',
  info: 'bg-mnema-accent text-mnema-accent-ink font-mono font-semibold'
}
</script>

<template>
  <span :class="['inline-flex min-w-[18px] shrink-0 items-center justify-center rounded-full px-1.5 py-0.5 text-center text-xs leading-none tabular-nums', VARIANTS[variant]]"><slot /></span>
</template>
