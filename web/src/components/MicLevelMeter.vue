<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useVoiceStore } from '../stores/voice'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'

// The microphone level bar: amber below the threshold (not transmitted), green
// above it (transmitted), with the loudest recent level marked.
const props = defineProps<{ numbers?: boolean }>()
const voiceStore = useVoiceStore()

// The marker shows what the gate really uses (auto mode ignores the slider).
const threshold = computed(() => effectiveThreshold(voiceStore))
const isAboveThreshold = computed(() => voiceStore.currentInputLevel >= threshold.value)

const peakHold = createPeakHold()
const peakLevel = ref(0)
watch(() => voiceStore.currentInputLevel, level => {
  peakLevel.value = peakHold(level)
})
defineExpose({ peakLevel, threshold })
</script>

<template>
  <div class="stack-2">
    <div
      class="relative flex h-9 items-center overflow-hidden rounded-lg border border-mnema-border bg-mnema-canvas p-1"
      role="meter"
      :aria-label="$t('audio.levelLabel')"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-valuenow="voiceStore.currentInputLevel"
    >
      <div
        class="h-full rounded-md transition-all duration-75"
        :class="isAboveThreshold ? 'bg-mnema-accent' : 'bg-mnema-warning'"
        :style="{ width: `${voiceStore.currentInputLevel}%` }"
      ></div>

      <!-- Peak marker (loudest level of the last moments) -->
      <div
        v-if="peakLevel > 0"
        class="pointer-events-none absolute top-1.5 bottom-1.5 w-0.5 rounded-sm"
        :class="peakLevel >= threshold ? 'bg-mnema-accent-hover' : 'bg-mnema-amber'"
        :style="{ left: `${peakLevel}%` }"
      ></div>

      <!-- Threshold marker -->
      <div
        class="pointer-events-none absolute top-0 bottom-0 z-10 w-1 bg-white shadow-lg transition-all"
        :style="{ left: `${threshold}%` }"
      ></div>
    </div>

    <div v-if="props.numbers" class="flex items-center justify-between font-mono text-xs text-mnema-tertiary">
      <span :class="isAboveThreshold ? 'font-bold text-mnema-accent' : 'text-mnema-warning'">
        {{ $t('audio.level', { level: voiceStore.currentInputLevel, peak: peakLevel }) }}
      </span>
      <span class="font-bold text-mnema-text">
        {{ $t('audio.threshold', { value: threshold }) }}
      </span>
    </div>
  </div>
</template>
