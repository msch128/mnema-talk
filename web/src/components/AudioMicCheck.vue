<script setup lang="ts">
import { ref, watch } from 'vue'
import { Square, Volume2, CheckCircle2 } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'
import { effectiveThreshold } from '../lib/levelMeter'
import MicLevelMeter from './MicLevelMeter.vue'

// The first check, up front: pick the microphone, press the button, say a few
// words and see the bar move. Nothing else is needed to be heard.
const voiceStore = useVoiceStore()
const { stopMicTest, toggleMicTest } = useWebRTC()
const applyActiveAudioSettings = useActiveAudioSettings()

// Heard: the level passed the gate while testing; the check stays done until the test ends.
const heard = ref(false)
watch(() => [voiceStore.isMicTesting, voiceStore.currentInputLevel] as const, ([testing, level]) => {
  if (!testing) heard.value = false
  else if (level >= effectiveThreshold(voiceStore)) heard.value = true
})

function handleMicTest() {
  if (voiceStore.isMicTesting) {
    stopMicTest()
    return
  }
  // Capture owns generation-bound cancellation. A late result from a closed
  // dialog must not stop a newer dialog's test through global cleanup.
  return toggleMicTest()
}

async function handleDeviceChange() {
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}
</script>

<template>
  <section class="settings-group" aria-labelledby="audio-group-meter">
    <h3 id="audio-group-meter" class="settings-label">{{ $t('audio.groupMeter') }}</h3>
    <p class="settings-help">{{ $t('audio.groupMeterHelp') }}</p>

    <div class="settings-card">
      <div class="stack-2">
        <label for="audio-input-device" class="settings-row-title block">{{ $t('audio.inputDevice') }}</label>
        <select
          id="audio-input-device"
          v-model="voiceStore.selectedInputDeviceId"
          class="w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text"
          @change="handleDeviceChange"
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

      <MicLevelMeter />

      <!-- The test: prominent, with what it does in one line -->
      <div class="flex flex-col gap-3 rounded-lg border border-mnema-hairline bg-mnema-canvas/50 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-5">
        <div class="min-w-0 space-y-0.5">
          <div class="flex items-center gap-2 text-sm font-semibold text-mnema-text">
            <Volume2 class="h-4 w-4 text-mnema-accent" />
            <span>{{ $t('audio.micTest') }}</span>
            <span
              v-if="voiceStore.isMicTesting"
              class="animate-pulse rounded-sm bg-amber-500/15 px-2 py-0.5 font-mono text-xs font-medium text-amber-400"
            >
              {{ $t('audio.micTestActiveBadge') }}
            </span>
          </div>
          <p class="settings-hint">{{ $t('audio.micTestHint') }}</p>
        </div>
        <button
          type="button"
          data-testid="mic-test-toggle"
          :aria-pressed="voiceStore.isMicTesting ? 'true' : 'false'"
          :class="['btn btn-lg shrink-0 max-sm:w-full', voiceStore.isMicTesting ? 'btn-danger-solid' : 'btn-primary']"
          @click="handleMicTest"
        >
          <Square v-if="voiceStore.isMicTesting" class="h-3.5 w-3.5 fill-current" />
          <Volume2 v-else class="h-4 w-4" />
          <span>{{ voiceStore.isMicTesting ? $t('audio.micTestStop') : $t('audio.micTestStart') }}</span>
        </button>
      </div>

      <p v-if="heard" role="status" data-testid="mic-heard" class="flex items-center gap-2 text-sm font-medium text-mnema-accent">
        <CheckCircle2 class="h-4 w-4 shrink-0" aria-hidden="true" />{{ $t('audio.micHeard') }}
      </p>
    </div>
  </section>
</template>
