<script setup lang="ts">
import { Sparkles } from '@lucide/vue'
import type { NoiseMode } from '../stores/voice'
import { useVoiceStore, NOISE_MODES } from '../stores/voice'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'

// Audio processing (gain, noise suppression, echo, packet priority) and the warnings.
const voiceStore = useVoiceStore()
const applyActiveAudioSettings = useActiveAudioSettings()

async function toggleAgc() {
  voiceStore.autoGainControl = !voiceStore.autoGainControl
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

async function setNoiseMode(mode: NoiseMode) {
  if (voiceStore.noiseMode === mode) return
  voiceStore.setNoiseMode(mode)
  await applyActiveAudioSettings()
}

async function toggleEcho() {
  voiceStore.echoCancellation = !voiceStore.echoCancellation
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

async function toggleQos() {
  voiceStore.qosHighPriority = !voiceStore.qosHighPriority
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

function toggleWarnNoAudio() {
  voiceStore.warnNoAudioDetected = !voiceStore.warnNoAudioDetected
  voiceStore.saveSettings()
}

function toggleWarnSwitchChannel() {
  voiceStore.warnSwitchChannel = !voiceStore.warnSwitchChannel
  voiceStore.saveSettings()
}
</script>

<template>
  <!-- 3. Audio processing -->
  <div class="space-y-2">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.processing') }}
    </label>

    <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline">
      <!-- Automatic gain control -->
      <div class="p-3.5 flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text flex items-center gap-1.5">
            <span>{{ $t('audio.agc') }}</span>
            <span class="text-xs px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-400 font-mono">{{ $t('audio.agcBadge') }}</span>
          </div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.agcHint') }}
          </p>
        </div>
        <button
          @click="toggleAgc"
          role="switch"
          :aria-checked="voiceStore.autoGainControl ? 'true' : 'false'"
          :aria-label="$t('audio.agc')"
          :class="[
            'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
            voiceStore.autoGainControl ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
          ]"
        >
          <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.autoGainControl ? 'translate-x-5' : 'translate-x-0']"></div>
        </button>
      </div>

      <!-- Noise suppression -->
      <div class="p-3.5 space-y-2.5">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text flex items-center gap-1.5">
            <Sparkles class="w-4 h-4 text-mnema-mint" />
            <span id="noise-mode-label">{{ $t('audio.noise') }}</span>
          </div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t(`audio.noiseHint_${voiceStore.noiseMode}`) }}
          </p>
        </div>
        <div role="radiogroup" aria-labelledby="noise-mode-label" class="grid grid-cols-2 sm:grid-cols-4 gap-1 p-1 rounded-lg bg-mnema-canvas border border-mnema-hairline">
          <button
            v-for="mode in NOISE_MODES"
            :key="mode"
            type="button"
            role="radio"
            :aria-checked="voiceStore.noiseMode === mode ? 'true' : 'false'"
            @click="setNoiseMode(mode)"
            :class="[
              'px-2 py-1.5 rounded-md text-xs font-semibold transition',
              voiceStore.noiseMode === mode
                ? 'bg-mnema-surface text-mnema-text ring-1 ring-mnema-accent/40'
                : 'text-mnema-muted hover:text-mnema-text'
            ]"
          >
            {{ $t(`audio.noiseMode_${mode}`) }}
          </button>
        </div>
      </div>

      <!-- Echo cancellation -->
      <div class="p-3.5 flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text">{{ $t('audio.echo') }}</div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.echoHint') }}
          </p>
        </div>
        <button
          @click="toggleEcho"
          role="switch"
          :aria-checked="voiceStore.echoCancellation ? 'true' : 'false'"
          :aria-label="$t('audio.echo')"
          :class="[
            'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
            voiceStore.echoCancellation ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
          ]"
        >
          <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.echoCancellation ? 'translate-x-5' : 'translate-x-0']"></div>
        </button>
      </div>

      <!-- Quality of Service (High Packet Priority) -->
      <div class="p-3.5 flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text">{{ $t('audio.qosTitle') }}</div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.qosHint') }}
          </p>
        </div>
        <button
          @click="toggleQos"
          role="switch"
          :aria-checked="voiceStore.qosHighPriority ? 'true' : 'false'"
          :aria-label="$t('audio.qosTitle')"
          :class="[
            'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
            voiceStore.qosHighPriority ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
          ]"
        >
          <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.qosHighPriority ? 'translate-x-5' : 'translate-x-0']"></div>
        </button>
      </div>
    </div>
  </div>

  <!-- 5. Warnings & Prompts -->
  <div class="space-y-2">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.warnings') }}
    </label>
    <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline">
      <!-- No audio detected warning -->
      <div class="p-3.5 flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text">{{ $t('audio.noAudioWarningTitle') }}</div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.noAudioWarningHint') }}
          </p>
        </div>
        <button
          @click="toggleWarnNoAudio"
          role="switch"
          :aria-checked="voiceStore.warnNoAudioDetected ? 'true' : 'false'"
          :aria-label="$t('audio.noAudioWarningTitle')"
          :class="[
            'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
            voiceStore.warnNoAudioDetected ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
          ]"
        >
          <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.warnNoAudioDetected ? 'translate-x-5' : 'translate-x-0']"></div>
        </button>
      </div>

      <!-- Switch voice channel warning -->
      <div class="p-3.5 flex items-start justify-between gap-4">
        <div class="space-y-0.5">
          <div class="text-sm font-semibold text-mnema-text">{{ $t('audio.switchChannelWarningTitle') }}</div>
          <p class="text-xs text-mnema-tertiary leading-relaxed">
            {{ $t('audio.switchChannelWarningHint') }}
          </p>
        </div>
        <button
          @click="toggleWarnSwitchChannel"
          role="switch"
          :aria-checked="voiceStore.warnSwitchChannel ? 'true' : 'false'"
          :aria-label="$t('audio.switchChannelWarningTitle')"
          :class="[
            'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
            voiceStore.warnSwitchChannel ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
          ]"
        >
          <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.warnSwitchChannel ? 'translate-x-5' : 'translate-x-0']"></div>
        </button>
      </div>
    </div>
  </div>
</template>
