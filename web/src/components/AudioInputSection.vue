<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Mic, Radio, Square, HelpCircle, Volume2 } from '@lucide/vue'
import type { InputMode } from '../stores/voice'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'

// Input: the microphone, its volume, voice activity or push-to-talk, and the
// mic test with the sensitivity meter.
const voiceStore = useVoiceStore()
const { stopMicTest, toggleMicTest } = useWebRTC()
const applyActiveAudioSettings = useActiveAudioSettings()

const isRecordingPttKey = ref(false)

// The marker shows what the gate really uses (auto mode ignores the slider).
const threshold = computed(() => effectiveThreshold(voiceStore))
const isAboveThreshold = computed(() => voiceStore.currentInputLevel >= threshold.value)

// Peak marker so the loudest recent level stays visible while calibrating.
const peakHold = createPeakHold()
const peakLevel = ref(0)
watch(() => voiceStore.currentInputLevel, level => {
  peakLevel.value = peakHold(level)
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

function handleSliderChange(e: Event) {
  voiceStore.sensitivityThreshold = parseInt((e.target as HTMLInputElement).value, 10)
  voiceStore.saveSettings()
}

function handleInputModeChange(mode: InputMode) {
  voiceStore.inputMode = mode
  voiceStore.saveSettings()
}

function handleKeyRecord(e: KeyboardEvent) {
  if (!isRecordingPttKey.value) return
  e.preventDefault()
  voiceStore.pttKey = e.code || e.key
  isRecordingPttKey.value = false
  voiceStore.saveSettings()
}

async function handleDeviceChange() {
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}
</script>

<template>
  <!-- 1. Input: device, volume, how speaking is triggered -->
  <section class="settings-group" aria-labelledby="audio-group-input">
    <h3 id="audio-group-input" class="settings-label">{{ $t('audio.groupInput') }}</h3>
    <p class="settings-help">{{ $t('audio.groupInputHelp') }}</p>

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
      </div>

      <div class="settings-sub">
        <div class="settings-row-title">{{ $t('audio.inputMode') }}</div>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <button
            type="button"
            :aria-pressed="voiceStore.inputMode === 'activity' ? 'true' : 'false'"
            class="choice-card flex items-center gap-3"
            @click="handleInputModeChange('activity')"
          >
            <div :class="['flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', voiceStore.inputMode === 'activity' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Mic class="h-4 w-4" />
            </div>
            <div>
              <div class="text-sm font-semibold">{{ $t('audio.activity') }}</div>
              <div class="text-xs text-mnema-tertiary">{{ $t('audio.activityHint') }}</div>
            </div>
          </button>

          <button
            type="button"
            :aria-pressed="voiceStore.inputMode === 'ptt' ? 'true' : 'false'"
            class="choice-card flex items-center gap-3"
            @click="handleInputModeChange('ptt')"
          >
            <div :class="['flex h-9 w-9 shrink-0 items-center justify-center rounded-lg', voiceStore.inputMode === 'ptt' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Radio class="h-4 w-4" />
            </div>
            <div>
              <div class="text-sm font-semibold">{{ $t('audio.ptt') }}</div>
              <div class="text-xs text-mnema-tertiary">{{ $t('audio.pttHint') }}</div>
            </div>
          </button>
        </div>

        <!-- Push-to-talk key (if active) -->
        <div v-if="voiceStore.inputMode === 'ptt'" class="stack-2 rounded-lg border border-mnema-hairline bg-mnema-canvas/50 px-4 py-3">
          <div class="flex flex-wrap items-center justify-between gap-3 text-sm">
            <span class="settings-row-title">{{ $t('audio.pttKey') }}</span>
            <button
              type="button"
              :class="['btn', isRecordingPttKey ? 'btn-primary animate-pulse' : 'btn-secondary font-mono']"
              @click="isRecordingPttKey = true"
              @keydown="handleKeyRecord"
            >
              {{ isRecordingPttKey ? $t('audio.pressKey') : voiceStore.pttKey }}
            </button>
          </div>
          <p class="settings-hint">{{ $t('audio.pttKeyHint') }}</p>
        </div>
      </div>
    </div>
  </section>

  <!-- 2. Check the microphone: the test button and the level meter, up front -->
  <section class="settings-group" aria-labelledby="audio-group-meter">
    <h3 id="audio-group-meter" class="settings-label">{{ $t('audio.groupMeter') }}</h3>
    <p class="settings-help">{{ $t('audio.groupMeterHelp') }}</p>

    <div class="settings-card">
      <!-- Level bar: amber below the threshold (not transmitted), green above it (transmitted) -->
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

      <div class="flex items-center justify-between font-mono text-xs text-mnema-tertiary">
        <span :class="isAboveThreshold ? 'font-bold text-mnema-accent' : 'text-mnema-warning'">
          {{ $t('audio.level', { level: voiceStore.currentInputLevel, peak: peakLevel }) }}
        </span>
        <span class="font-bold text-mnema-text">
          {{ $t('audio.threshold', { value: threshold }) }}
        </span>
      </div>

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

      <!-- Sensitivity (voice activity mode) -->
      <div v-if="voiceStore.inputMode === 'activity'" class="settings-sub">
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
  </section>
</template>
