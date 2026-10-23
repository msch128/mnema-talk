<script setup lang="ts">
import { useVoiceStore } from '../stores/voice'
import { canChooseOutputDevice } from '../lib/audioOutput'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'

// Input and output devices and their volumes.
const voiceStore = useVoiceStore()
const applyActiveAudioSettings = useActiveAudioSettings()
const outputSelectable = canChooseOutputDevice()
const sliderClass = 'w-full h-1.5 rounded-full appearance-none cursor-pointer bg-mnema-border accent-mnema-accent'

async function handleDeviceChange() {
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}
</script>

<template>
  <!-- 7. Devices -->
  <div class="stack-3">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.devices') }}
    </label>

    <div class="stack-2">
      <div>
        <label class="text-xs text-mnema-tertiary font-mono block mb-1">{{ $t('audio.inputDevice') }}</label>
        <select
          v-model="voiceStore.selectedInputDeviceId"
          @change="handleDeviceChange"
          class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
        >
          <option value="">{{ $t('audio.defaultDevice') }}</option>
          <option 
            v-for="dev in voiceStore.availableInputDevices" 
            :key="dev.deviceId" 
            :value="dev.deviceId"
          >
            {{ dev.label || $t('audio.deviceFallback', { id: dev.deviceId.slice(0, 5) }) }}
          </option>
        </select>
      </div>

      <div>
        <div class="mb-1 flex items-center justify-between text-xs font-mono">
          <label for="audio-input-volume" class="text-mnema-tertiary">{{ $t('audio.inputVolume') }}</label>
          <span class="tabular-nums text-mnema-text">{{ voiceStore.inputVolume }} %</span>
        </div>
        <input
          id="audio-input-volume"
          v-model.number="voiceStore.inputVolume"
          type="range"
          min="0"
          max="200"
          step="5"
          data-testid="input-volume"
          :class="sliderClass"
        />
      </div>

      <div>
        <label for="audio-output-device" class="text-xs text-mnema-tertiary font-mono block mb-1">{{ $t('audio.outputDevice') }}</label>
        <select
          v-if="outputSelectable"
          id="audio-output-device"
          v-model="voiceStore.selectedOutputDeviceId"
          data-testid="output-device"
          class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text focus:outline-hidden focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
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
        <p v-else class="text-xs text-mnema-tertiary">{{ $t('audio.outputDeviceUnsupported') }}</p>
      </div>

      <div>
        <div class="mb-1 flex items-center justify-between text-xs font-mono">
          <label for="audio-output-volume" class="text-mnema-tertiary">{{ $t('audio.outputVolume') }}</label>
          <span class="tabular-nums text-mnema-text">{{ voiceStore.outputVolume }} %</span>
        </div>
        <input
          id="audio-output-volume"
          v-model.number="voiceStore.outputVolume"
          type="range"
          min="0"
          max="200"
          step="5"
          data-testid="output-volume"
          :class="sliderClass"
        />
        <p class="mt-1 text-xs text-mnema-tertiary">{{ $t('audio.perUserVolumeHint') }}</p>
      </div>
    </div>
  </div>
</template>
