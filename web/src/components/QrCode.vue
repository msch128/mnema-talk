<script setup lang="ts">
// A QR code drawn as an inline SVG from the text, in the browser: the secret
// of a two-factor setup never goes to any image service. Black on white also
// in the dark theme, because scanners need the contrast.
import { computed } from 'vue'
import qrcode from 'qrcode-generator'

const props = defineProps<{ text: string; label: string }>()

const QUIET = 4

const matrix = computed(() => {
  const qr = qrcode(0, 'M')
  qr.addData(props.text)
  qr.make()
  const n = qr.getModuleCount()
  let d = ''
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (qr.isDark(r, c)) d += `M${c + QUIET} ${r + QUIET}h1v1h-1z`
    }
  }
  return { size: n + 2 * QUIET, d }
})
</script>

<template>
  <svg
    role="img"
    :aria-label="label"
    :viewBox="`0 0 ${matrix.size} ${matrix.size}`"
    class="h-44 w-44 rounded-md"
    shape-rendering="crispEdges"
    data-testid="qr-code"
  >
    <rect width="100%" height="100%" fill="#fff" />
    <path :d="matrix.d" fill="#000" />
  </svg>
</template>
