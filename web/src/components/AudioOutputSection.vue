<script setup lang="ts">
import { useVoiceStore } from '../stores/voice'
import { canChooseOutputDevice } from '../lib/audioOutput'
import { playSoundEffect } from '../lib/soundEffects'

// Output: where you hear the others, how loud, and the sound effects.
const voiceStore = useVoiceStore()
const outputSelectable = canChooseOutputDevice()

function toggleSoundEffects() {
  voiceStore.soundEffectsEnabled = !voiceStore.soundEffectsEnabled
  voiceStore.saveSettings()
  if (voiceStore.soundEffectsEnabled) {
    playSoundEffect('unmute')
  }
}

function handleSoundsVolumeChange(e: Event) {
  voiceStore.soundEffectsVolume = parseInt((e.target as HTMLInputElement).value, 10)
  voiceStore.saveSettings()
}
</script>

<template>
  <section class="settings-group" aria-labelledby="audio-group-output">
    <h3 id="audio-group-output" class="settings-label">{{ $t('audio.groupOutput') }}</h3>
    <p class="settings-help">{{ $t('audio.groupOutputHelp') }}</p>

    <div class="settings-card">
      <div class="stack-2">
        <label for="audio-output-device" class="settings-row-title block">{{ $t('audio.outputDevice') }}</label>
        <select
          v-if="outputSelectable"
          id="audio-output-device"
          v-model="voiceStore.selectedOutputDeviceId"
          data-testid="output-device"
          class="w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
        >
          <option value="">{{ $t('audio.defaultDevice') }}</option>
          <option
            v-for="dev in voiceStore.availableOutputDevices.filter(d => d.deviceId && d.deviceId !== 'default')"
            :key="dev.deviceId"
            :value="dev.deviceId"
          >
            {{ dev.label || $t('audio.deviceFallback', { id: dev.deviceId.slice(0, 5) }) }}
          </option>
        </select>
        <p v-else class="settings-hint">{{ $t('audio.outputDeviceUnsupported') }}</p>
      </div>

      <div class="stack-2">
        <div class="flex items-center justify-between gap-3">
          <label for="audio-output-volume" class="settings-row-title">{{ $t('audio.outputVolume') }}</label>
          <span class="font-mono text-xs tabular-nums text-mnema-text">{{ voiceStore.outputVolume }} %</span>
        </div>
        <input
          id="audio-output-volume"
          v-model.number="voiceStore.outputVolume"
          type="range"
          min="0"
          max="200"
          step="5"
          data-testid="output-volume"
        />
        <p class="settings-hint">{{ $t('audio.perUserVolumeHint') }}</p>
      </div>

      <!-- Sound effects: on or off, and how loud -->
      <div class="settings-sub">
        <div class="flex items-start justify-between gap-6">
          <div class="settings-row-text">
            <div class="settings-row-title">{{ $t('audio.soundsTitle') }}</div>
            <p class="settings-hint">{{ $t('audio.soundsHint') }}</p>
          </div>
          <button
            type="button"
            role="switch"
            :aria-checked="voiceStore.soundEffectsEnabled ? 'true' : 'false'"
            :aria-label="$t('audio.soundsTitle')"
            class="switch mt-0.5"
            @click="toggleSoundEffects"
          ></button>
        </div>

        <div v-if="voiceStore.soundEffectsEnabled" class="stack-2">
          <div class="flex items-center justify-between gap-3">
            <span class="settings-row-title">{{ $t('audio.soundsVolume') }}</span>
            <span class="font-mono text-xs font-semibold tabular-nums text-mnema-text">{{ voiceStore.soundEffectsVolume }} %</span>
          </div>
          <input
            v-model.number="voiceStore.soundEffectsVolume"
            type="range"
            :aria-label="$t('audio.soundsVolume')"
            min="0"
            max="100"
            @input="handleSoundsVolumeChange"
          />
        </div>
      </div>
    </div>
  </section>
</template>
