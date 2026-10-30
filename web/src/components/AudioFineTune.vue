<script setup lang="ts">
import { HelpCircle } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import MicLevelMeter from './MicLevelMeter.vue'

// How loud the microphone is and how loud you must be before it opens: the
// part of the input that only matters when the basic check is not enough.
const voiceStore = useVoiceStore()

function handleSliderChange(e: Event) {
  voiceStore.sensitivityThreshold = parseInt((e.target as HTMLInputElement).value, 10)
  voiceStore.saveSettings()
}
</script>

<template>
  <div class="stack-5">
    <div class="stack-2">
      <div class="flex items-center justify-between gap-3">
        <label for="audio-input-volume" class="settings-row-title">{{ $t('audio.inputVolume') }}</label>
        <span class="font-mono text-xs tabular-nums text-mnema-text">{{ voiceStore.inputVolume }} %</span>
      </div>
      <input
        id="audio-input-volume"
        v-model.number="voiceStore.inputVolume"
        type="range"
        min="0"
        max="200"
        step="5"
        data-testid="input-volume"
      />
      <p class="settings-hint">{{ $t('audio.inputVolumeHint') }}</p>
    </div>

    <!-- Sensitivity (voice activity mode) -->
    <div v-if="voiceStore.inputMode === 'activity'" class="stack-2">
      <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span class="settings-row-title">{{ $t('audio.sensitivity') }}</span>
        <label class="flex cursor-pointer items-center gap-2 text-sm text-mnema-muted hover:text-mnema-text">
          <input
            v-model="voiceStore.autoSensitivity"
            type="checkbox"
            @change="voiceStore.saveSettings"
          />
          <span>{{ $t('audio.auto') }}</span>
        </label>
      </div>
      <p class="settings-hint">{{ $t('audio.sensitivityHint') }}</p>

      <MicLevelMeter numbers />

      <input
        v-if="!voiceStore.autoSensitivity"
        v-model="voiceStore.sensitivityThreshold"
        type="range"
        :aria-label="$t('audio.sensitivity')"
        min="0"
        max="100"
        @input="handleSliderChange"
      />

      <div class="flex gap-2 text-mnema-tertiary">
        <HelpCircle class="mt-0.5 h-4 w-4 shrink-0 text-mnema-mint" />
        <p class="settings-hint"><strong class="font-semibold text-mnema-text">{{ $t('audio.adviceTitle') }}.</strong> {{ $t('audio.advice') }}</p>
      </div>
    </div>
  </div>
</template>
