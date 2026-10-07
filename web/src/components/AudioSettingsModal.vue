<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { Mic, Sparkles, HelpCircle, Radio, Volume2, Square, Play } from '@lucide/vue'
import { useVoiceStore, NOISE_MODES, SOUND_EVENTS } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { canChooseOutputDevice } from '../lib/audioOutput'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'
import { playSoundEffect } from '../lib/soundEffects'
import BaseDialog from './BaseDialog.vue'

const emit = defineEmits(['close'])
const voiceStore = useVoiceStore()
const { refreshAudioDevices, stopMicTest, toggleMicTest, applyAudioSettings } = useWebRTC()

const isRecordingPttKey = ref(false)
const outputSelectable = canChooseOutputDevice()
const sliderClass = 'w-full h-1.5 rounded-full appearance-none cursor-pointer bg-mnema-border accent-mnema-accent'

// The marker shows what the gate really uses (auto mode ignores the slider).
const threshold = computed(() => effectiveThreshold(voiceStore))
const isAboveThreshold = computed(() => voiceStore.currentInputLevel >= threshold.value)

// Peak marker so the loudest recent level stays visible while calibrating.
const peakHold = createPeakHold()
const peakLevel = ref(0)
watch(() => voiceStore.currentInputLevel, level => {
  peakLevel.value = peakHold(level)
})

// Listing devices does not request capture. A local test needs its own action.
onMounted(() => refreshAudioDevices())

onUnmounted(() => {
  stopMicTest()
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

function applyActiveAudioSettings() {
  // Saving preferences must not acquire a microphone outside a call/test.
  if (voiceStore.localAudioStream || voiceStore.isMicTesting) return applyAudioSettings()
}

function handleSliderChange(e) {
  voiceStore.sensitivityThreshold = parseInt(e.target.value, 10)
  voiceStore.saveSettings()
}

function handleInputModeChange(mode) {
  voiceStore.inputMode = mode
  voiceStore.saveSettings()
}

function handleKeyRecord(e) {
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

async function toggleAgc() {
  voiceStore.autoGainControl = !voiceStore.autoGainControl
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

async function setNoiseMode(mode) {
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

function toggleSoundEffects() {
  voiceStore.soundEffectsEnabled = !voiceStore.soundEffectsEnabled
  voiceStore.saveSettings()
  if (voiceStore.soundEffectsEnabled) {
    playSoundEffect('unmute')
  }
}

function handleSoundsVolumeChange(e) {
  voiceStore.soundEffectsVolume = parseInt(e.target.value, 10)
  voiceStore.saveSettings()
}

function playPreviewSound(sound = 'join') {
  playSoundEffect(sound, null, true)
}
</script>

<template>
  <BaseDialog :title="$t('audio.settings')" :subtitle="$t('audio.subtitle')" panel-class="max-w-xl select-none" @close="emit('close')">
    <div class="contents">
      <!-- Settings Body -->
      <div class="p-6 space-y-6 overflow-y-auto max-h-[80vh]">
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

        <!-- 6. Sound Effects -->
        <div class="space-y-2">
          <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
            {{ $t('audio.soundsTitle') }}
          </label>
          <div class="rounded-lg bg-mnema-surface border border-mnema-hairline p-3.5 space-y-3">
            <div class="flex items-start justify-between gap-4">
              <div class="space-y-0.5">
                <div class="text-sm font-semibold text-mnema-text">{{ $t('audio.soundsTitle') }}</div>
                <p class="text-xs text-mnema-tertiary leading-relaxed">
                  {{ $t('audio.soundsHint') }}
                </p>
              </div>
              <button
                @click="toggleSoundEffects"
                role="switch"
                :aria-checked="voiceStore.soundEffectsEnabled ? 'true' : 'false'"
                :aria-label="$t('audio.soundsTitle')"
                :class="[
                  'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
                  voiceStore.soundEffectsEnabled ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
                ]"
              >
                <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.soundEffectsEnabled ? 'translate-x-5' : 'translate-x-0']"></div>
              </button>
            </div>

            <!-- Volume slider when enabled -->
            <div v-if="voiceStore.soundEffectsEnabled" class="space-y-1 pt-2 border-t border-mnema-hairline">
              <div class="flex items-center justify-between text-xs">
                <span class="text-mnema-tertiary font-mono">{{ $t('audio.soundsVolume') }}</span>
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    @click="playPreviewSound"
                    class="text-xs text-mnema-accent hover:underline font-mono"
                  >
                    {{ $t('audio.soundsTest') }}
                  </button>
                  <span class="text-mnema-text font-mono font-semibold">{{ voiceStore.soundEffectsVolume }} %</span>
                </div>
              </div>
              <input
                type="range"
                :aria-label="$t('audio.soundsVolume')"
                min="0"
                max="100"
                v-model.number="voiceStore.soundEffectsVolume"
                @input="handleSoundsVolumeChange"
                class="w-full accent-mnema-accent cursor-pointer"
              />
            </div>

            <!-- Granular sound effects toggles -->
            <div v-if="voiceStore.soundEffectsEnabled" class="space-y-1.5 pt-3 border-t border-mnema-hairline">
              <div class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono mb-1">
                {{ $t('audio.soundsCustomTitle') }}
              </div>
              <div class="divide-y divide-mnema-hairline/60 rounded-md bg-mnema-canvas border border-mnema-hairline">
                <div
                  v-for="sound in SOUND_EVENTS"
                  :key="sound"
                  class="flex items-center justify-between p-2.5 text-xs"
                >
                  <div class="flex items-center gap-2">
                    <button
                      type="button"
                      @click="playPreviewSound(sound)"
                      :aria-label="$t('audio.soundsTest') + ': ' + $t(`audio.sound_${sound}`)"
                      class="w-6 h-6 rounded flex items-center justify-center bg-mnema-raised text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition flex-shrink-0"
                    >
                      <Play class="w-3 h-3 fill-current ml-0.5" />
                    </button>
                    <span class="text-mnema-text font-medium">{{ $t(`audio.sound_${sound}`) }}</span>
                  </div>
                  <button
                    type="button"
                    @click="voiceStore.toggleSoundEvent(sound)"
                    role="switch"
                    :aria-checked="voiceStore.soundEvents[sound] !== false ? 'true' : 'false'"
                    :aria-label="$t(`audio.sound_${sound}`)"
                    :class="[
                      'w-8 h-4 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0',
                      voiceStore.soundEvents[sound] !== false ? 'bg-mnema-accent' : 'bg-mnema-surface border border-mnema-border'
                    ]"
                  >
                    <div :class="['w-3 h-3 rounded-full bg-white transition-transform shadow-sm', voiceStore.soundEvents[sound] !== false ? 'translate-x-4' : 'translate-x-0']"></div>
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 7. Devices -->
        <div class="space-y-3">
          <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
            {{ $t('audio.devices') }}
          </label>

          <div class="space-y-2">
            <div>
              <label class="text-xs text-mnema-tertiary font-mono block mb-1">{{ $t('audio.inputDevice') }}</label>
              <select
                v-model="voiceStore.selectedInputDeviceId"
                @change="handleDeviceChange"
                class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
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
                class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
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
      </div>

      <!-- Footer -->
      <footer class="px-6 py-3.5 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-between">
        <span class="text-xs text-mnema-tertiary font-mono">
          {{ $t('audio.liveNote') }}
        </span>
        <button
          @click="emit('close')"
          class="px-5 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition shadow-sm"
        >
          {{ $t('audio.done') }}
        </button>
      </footer>
    </div>
  </BaseDialog>
</template>
