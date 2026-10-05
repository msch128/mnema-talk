<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { Mic, Sparkles, HelpCircle, Radio } from '@lucide/vue'
import { useVoiceStore, NOISE_MODES } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'
import BaseDialog from './BaseDialog.vue'

const emit = defineEmits(['close'])
const voiceStore = useVoiceStore()
const { refreshAudioDevices, startMicTest, stopMicTest, applyAudioSettings } = useWebRTC()

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

onMounted(async () => {
  await refreshAudioDevices()
  await startMicTest()
})

onUnmounted(() => {
  stopMicTest()
})

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
  await applyAudioSettings()
}

async function toggleAgc() {
  voiceStore.autoGainControl = !voiceStore.autoGainControl
  voiceStore.saveSettings()
  await applyAudioSettings()
}

async function setNoiseMode(mode) {
  if (voiceStore.noiseMode === mode) return
  voiceStore.setNoiseMode(mode)
  await applyAudioSettings()
}

async function toggleEcho() {
  voiceStore.echoCancellation = !voiceStore.echoCancellation
  voiceStore.saveSettings()
  await applyAudioSettings()
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

        <!-- 2. Input sensitivity and live meter (voice activity mode) -->
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
          </div>
        </div>

        <!-- 4. Devices -->
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
