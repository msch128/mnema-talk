<script setup lang="ts">
// A tiny line chart without axes: the shape of the last hour at a glance.
// The exact numbers live next to it, so the drawing is decorative for
// assistive technology except for its text summary (label).
import { computed } from 'vue'

const props = defineProps<{
  values: number[]
  /** Text summary for screen readers. */
  label: string
}>()

const W = 120
const H = 28
const PAD = 2

const points = computed(() => {
  const v = props.values
  if (v.length < 2) return ''
  let lo = Math.min(...v)
  let hi = Math.max(...v)
  // A flat series sits on the baseline instead of dividing by zero.
  if (hi === lo) { lo = Math.min(0, lo); hi = lo === hi ? lo + 1 : hi }
  const step = (W - PAD * 2) / (v.length - 1)
  return v
    .map((n, i) => `${(PAD + i * step).toFixed(1)},${(H - PAD - ((n - lo) / (hi - lo)) * (H - PAD * 2)).toFixed(1)}`)
    .join(' ')
})
</script>

<template>
  <svg
    :viewBox="`0 0 ${W} ${H}`"
    class="block h-7 w-full text-mnema-accent"
    preserveAspectRatio="none"
    role="img"
    :aria-label="label"
    data-testid="sparkline"
  >
    <line :x1="PAD" :y1="H - PAD" :x2="W - PAD" :y2="H - PAD" class="stroke-mnema-hairline" stroke-width="1" />
    <polyline
      v-if="points"
      :points="points"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
</template>
