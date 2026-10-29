<script setup lang="ts">
import { Sparkles } from '@lucide/vue'
import type { NoiseMode } from '../stores/voice'
import { useVoiceStore, NOISE_MODES } from '../stores/voice'
import { rovingKeydown } from '../lib/rovingFocus'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'

// The noise filter and echo cancellation. The rarely used processing options
// (gain control, packet priority) are in the Advanced section.
const voiceStore = useVoiceStore()
const applyActiveAudioSettings = useActiveAudioSettings()

async function setNoiseMode(mode: NoiseMode) {
  if (voiceStore.noiseMode === mode) return
  voiceStore.setNoiseMode(mode)
  await applyActiveAudioSettings()
}

function onModeKey(e: KeyboardEvent, index: number) {
  rovingKeydown(e, index, NOISE_MODES.length, n => {
    const mode = NOISE_MODES[n]
    if (mode) void setNoiseMode(mode)
  })
}

async function toggleEcho() {
  voiceStore.echoCancellation = !voiceStore.echoCancellation
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}
</script>

<template>
  <section class="settings-group" aria-labelledby="audio-group-processing">
    <h3 id="audio-group-processing" class="settings-label">{{ $t('audio.groupProcessing') }}</h3>
    <p class="settings-help">{{ $t('audio.groupProcessingHelp') }}</p>

    <div class="settings-card">
      <!-- Noise suppression -->
      <div class="stack-3">
        <div class="settings-row-text">
          <div class="settings-row-title flex items-center gap-2">
            <Sparkles class="h-4 w-4 text-mnema-mint" />
            <span id="noise-mode-label">{{ $t('audio.noise') }}</span>
          </div>
          <p class="settings-hint mt-0.5">{{ $t(`audio.noiseHint_${voiceStore.noiseMode}`) }}</p>
        </div>
        <div role="radiogroup" aria-labelledby="noise-mode-label" class="grid grid-cols-2 gap-1 rounded-xl border border-mnema-hairline bg-mnema-canvas p-1 sm:grid-cols-4">
          <button
            v-for="(mode, i) in NOISE_MODES"
            :key="mode"
            type="button"
            role="radio"
            :aria-checked="voiceStore.noiseMode === mode ? 'true' : 'false'"
            :tabindex="voiceStore.noiseMode === mode ? 0 : -1"
            :class="[
              'rounded-lg px-3 py-2 text-sm font-medium transition',
              voiceStore.noiseMode === mode
                ? 'bg-mnema-surface text-mnema-text shadow-sm ring-1 ring-mnema-accent/40'
                : 'text-mnema-muted hover:bg-mnema-hover/60 hover:text-mnema-text'
            ]"
            @click="setNoiseMode(mode)"
            @keydown="onModeKey($event, i)"
          >
            {{ $t(`audio.noiseMode_${mode}`) }}
          </button>
        </div>
      </div>

      <!-- Echo cancellation -->
      <div class="settings-sub">
        <div class="flex items-start justify-between gap-6">
          <div class="settings-row-text">
            <div class="settings-row-title">{{ $t('audio.echo') }}</div>
            <p class="settings-hint">{{ $t('audio.echoHint') }}</p>
          </div>
          <button
            type="button"
            role="switch"
            :aria-checked="voiceStore.echoCancellation ? 'true' : 'false'"
            :aria-label="$t('audio.echo')"
            class="switch mt-0.5"
            @click="toggleEcho"
          ></button>
        </div>
      </div>
    </div>
  </section>
</template>
