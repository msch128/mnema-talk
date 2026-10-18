<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Mic, Radio, Square, HelpCircle, Volume2 } from '@lucide/vue'
import type { InputMode } from '../stores/voice'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'

// Input mode (voice activity or push-to-talk), the push-to-talk key, the mic
// test and the sensitivity meter.
const voiceStore = useVoiceStore()
const { stopMicTest, toggleMicTest } = useWebRTC()

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
</script>

<template>
  <!-- 1. Input mode (voice activity or push-to-talk) -->
  <div class="space-y-2">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.inputMode') }}
    </label>
    <div class="grid grid-cols-2 gap-3">
      <button
        type="button"
        @click="handleInputModeChange('activity')"
        :class="[
          'p-3 rounded-lg border text-left transition flex items-center gap-3',
          voiceStore.inputMode === 'activity'
            ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
            : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
        ]"
      >
        <div :class="['w-8 h-8 rounded-md flex items-center justify-center flex-shrink-0', voiceStore.inputMode === 'activity' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
          <Mic class="w-4 h-4" />
        </div>
        <div>
          <div class="text-sm font-semibold">{{ $t('audio.activity') }}</div>
          <div class="text-xs text-mnema-tertiary">{{ $t('audio.activityHint') }}</div>
        </div>
      </button>

      <button
        type="button"
        @click="handleInputModeChange('ptt')"
        :class="[
          'p-3 rounded-lg border text-left transition flex items-center gap-3',
          voiceStore.inputMode === 'ptt'
            ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
            : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
        ]"
      >
        <div :class="['w-8 h-8 rounded-md flex items-center justify-center flex-shrink-0', voiceStore.inputMode === 'ptt' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
          <Radio class="w-4 h-4" />
        </div>
        <div>
          <div class="text-sm font-semibold">{{ $t('audio.ptt') }}</div>
          <div class="text-xs text-mnema-tertiary">{{ $t('audio.pttHint') }}</div>
        </div>
      </button>
    </div>
  </div>

  <!-- Push-to-talk key (if active) -->
  <div v-if="voiceStore.inputMode === 'ptt'" class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2">
    <div class="flex items-center justify-between text-sm">
      <span class="text-mnema-tertiary font-medium">{{ $t('audio.pttKey') }}</span>
      <button
        @click="isRecordingPttKey = true"
        @keydown="handleKeyRecord"
        :class="[
          'px-3 py-1 rounded border font-mono text-sm font-semibold transition',
          isRecordingPttKey 
            ? 'bg-mnema-accent text-mnema-accent-ink border-mnema-accent animate-pulse' 
            : 'bg-mnema-canvas border-mnema-border text-mnema-text hover:border-mnema-accent'
        ]"
      >
        {{ isRecordingPttKey ? $t('audio.pressKey') : voiceStore.pttKey }}
      </button>
    </div>
    <p class="text-xs text-mnema-tertiary">
      {{ $t('audio.pttKeyHint') }}
    </p>
  </div>

  <!-- 2. Mic Test (Discord-like level check and channel mute) -->
  <div class="space-y-2">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.micTest') }}
    </label>
    <div class="p-3.5 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2.5">
      <div class="flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text flex items-center gap-2">
            <Volume2 class="w-4 h-4 text-mnema-accent" />
            <span>{{ $t('audio.micTest') }}</span>
            <span
              v-if="voiceStore.isMicTesting"
              class="text-xs px-2 py-0.5 rounded bg-amber-500/15 text-amber-400 font-mono font-medium animate-pulse"
            >
              {{ $t('audio.micTestActiveBadge') }}
            </span>
          </div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.micTestHint') }}
          </p>
        </div>
        <button
          type="button"
          @click="handleMicTest"
          data-testid="mic-test-toggle"
          :aria-pressed="voiceStore.isMicTesting ? 'true' : 'false'"
          :class="[
            'px-3.5 py-1.5 rounded-lg font-semibold text-xs transition flex items-center gap-1.5 flex-shrink-0 shadow-sm mt-0.5',
            voiceStore.isMicTesting
              ? 'bg-amber-600 hover:bg-amber-500 text-white'
              : 'bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink'
          ]"
        >
          <Square v-if="voiceStore.isMicTesting" class="w-3.5 h-3.5 fill-current" />
          <Volume2 v-else class="w-3.5 h-3.5" />
          <span>{{ voiceStore.isMicTesting ? $t('audio.micTestStop') : $t('audio.micTestStart') }}</span>
        </button>
      </div>
    </div>
  </div>

  <!-- 3. Input sensitivity and live meter (voice activity mode) -->
  <div v-if="voiceStore.inputMode === 'activity'" class="space-y-3">
    <div class="flex items-center justify-between">
      <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
        {{ $t('audio.sensitivity') }}
      </label>
      <label class="flex items-center gap-2 cursor-pointer text-sm text-mnema-tertiary hover:text-mnema-text">
        <input
          type="checkbox"
          v-model="voiceStore.autoSensitivity"
          @change="voiceStore.saveSettings"
          class="rounded border-mnema-border bg-mnema-canvas text-mnema-accent focus:ring-0"
        />
        <span>{{ $t('audio.auto') }}</span>
      </label>
    </div>

    <!-- Live level meter and threshold slider -->
    <div class="space-y-2">
      <!-- Level bar: amber below the threshold (not transmitted), green above it (transmitted) -->
      <div class="relative h-6 bg-mnema-canvas rounded-lg border border-mnema-border overflow-hidden p-0.5 flex items-center">
        <!-- Live level fill -->
        <div
          class="h-full rounded transition-all duration-75"
          :class="isAboveThreshold ? 'bg-mnema-accent' : 'bg-mnema-warning'"
          :style="{ width: `${voiceStore.currentInputLevel}%` }"
        ></div>

        <!-- Peak Marker (loudest level of the last moments) -->
        <div
          v-if="peakLevel > 0"
          class="absolute top-1 bottom-1 w-0.5 rounded pointer-events-none"
          :class="peakLevel >= threshold ? 'bg-mnema-accent-hover' : 'bg-mnema-amber'"
          :style="{ left: `${peakLevel}%` }"
        ></div>

        <!-- Threshold marker -->
        <div
          class="absolute top-0 bottom-0 w-1 bg-white shadow-lg pointer-events-none z-10 transition-all"
          :style="{ left: `${threshold}%` }"
        ></div>
      </div>

      <div class="flex items-center justify-between text-xs font-mono text-mnema-tertiary">
        <span :class="isAboveThreshold ? 'text-mnema-accent font-bold' : 'text-mnema-warning'">
          {{ $t('audio.level', { level: voiceStore.currentInputLevel, peak: peakLevel }) }}
        </span>
        <span class="text-mnema-text font-bold">
          {{ $t('audio.threshold', { value: threshold }) }}
        </span>
      </div>

      <!-- Manual slider -->
      <div v-if="!voiceStore.autoSensitivity" class="space-y-1">
        <input
          type="range"
          :aria-label="$t('audio.sensitivity')"
          min="0"
          max="100"
          v-model="voiceStore.sensitivityThreshold"
          @input="handleSliderChange"
          class="w-full accent-mnema-accent cursor-pointer"
        />
      </div>

      <!-- Advice for shared rooms -->
      <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline text-sm text-mnema-tertiary space-y-1">
        <div class="flex items-center gap-1.5 text-mnema-text font-semibold text-xs">
          <HelpCircle class="w-4 h-4 text-mnema-mint" />
          <span>{{ $t('audio.adviceTitle') }}</span>
        </div>
        <p class="text-xs leading-relaxed">
          {{ $t('audio.advice') }}
        </p>
      </div>
    </div>
  </div>
</template>
