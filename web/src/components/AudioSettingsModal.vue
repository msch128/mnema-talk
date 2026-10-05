<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { X, Mic, Sparkles, Sliders, HelpCircle, Radio } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import { effectiveThreshold, createPeakHold } from '../lib/levelMeter'

const emit = defineEmits(['close'])
const voiceStore = useVoiceStore()
const { refreshAudioDevices, startMicTest, stopMicTest } = useWebRTC()

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
  await startMicTest()
}

async function toggleAgc() {
  voiceStore.autoGainControl = !voiceStore.autoGainControl
  voiceStore.saveSettings()
  await startMicTest()
}

async function toggleNoise() {
  voiceStore.toggleNoiseCancelling()
  await startMicTest()
}

async function toggleEcho() {
  voiceStore.echoCancellation = !voiceStore.echoCancellation
  voiceStore.saveSettings()
  await startMicTest()
}
</script>

<template>
  <div class="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4 select-none">
    <div class="bg-mnema-elevated w-full max-w-xl rounded-xl flex flex-col shadow-2xl border border-mnema-border overflow-hidden">
      <!-- Header -->
      <header class="px-6 py-4 border-b border-mnema-hairline flex items-center justify-between bg-mnema-raised">
        <div class="flex items-center gap-2.5">
          <div class="w-7 h-7 rounded-md bg-mnema-band text-mnema-mint flex items-center justify-center text-sm font-semibold">
            <Sliders class="w-4 h-4" />
          </div>
          <div>
            <h2 class="text-lg font-semibold text-mnema-text">Sprach- & Audio-Einstellungen</h2>
            <p class="text-xs text-mnema-tertiary font-mono">Eingabeempfindlichkeit & Raumakustik</p>
          </div>
        </div>

        <button 
          @click="emit('close')"
          class="p-1 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
        >
          <X class="w-4 h-4" />
        </button>
      </header>

      <!-- Settings Body -->
      <div class="p-6 space-y-6 overflow-y-auto max-h-[80vh]">
        <!-- 1. Input Mode Selection (Voice Activity vs PTT) -->
        <div class="space-y-2">
          <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
            Eingabemodus
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
                <div class="text-sm font-semibold">Sprachaktivierung</div>
                <div class="text-xs text-mnema-tertiary">Mikrofon öffnet per Pegel</div>
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
                <div class="text-sm font-semibold">Push-to-Talk</div>
                <div class="text-xs text-mnema-tertiary">Taste gedrückt halten</div>
              </div>
            </button>
          </div>
        </div>

        <!-- Push-to-Talk Keybind Selector (if PTT active) -->
        <div v-if="voiceStore.inputMode === 'ptt'" class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline space-y-2">
          <div class="flex items-center justify-between text-sm">
            <span class="text-mnema-tertiary font-medium">Push-to-Talk Taste</span>
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
              {{ isRecordingPttKey ? 'Taste drücken...' : voiceStore.pttKey }}
            </button>
          </div>
          <p class="text-xs text-mnema-tertiary">
            Klicke auf die Taste und drücke deine gewünschte Taste (z.B. Leertaste, Strg, Mausbutton).
          </p>
        </div>

        <!-- 2. Discord-Identical Input Sensitivity & Live Meter (Voice Activity Mode) -->
        <div v-if="voiceStore.inputMode === 'activity'" class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
              Eingabeempfindlichkeit (Noise Gate)
            </label>
            <label class="flex items-center gap-2 cursor-pointer text-sm text-mnema-tertiary hover:text-mnema-text">
              <input
                type="checkbox"
                v-model="voiceStore.autoSensitivity"
                @change="voiceStore.saveSettings"
                class="rounded border-mnema-border bg-mnema-canvas text-mnema-accent focus:ring-0"
              />
              <span>Automatisch ermitteln</span>
            </label>
          </div>

          <!-- Live Visual Volume Meter & Threshold Slider -->
          <div class="space-y-2">
            <!-- Level bar: amber below the threshold (not transmitted), green above it (transmitted) -->
            <div class="relative h-6 bg-mnema-canvas rounded-lg border border-mnema-border overflow-hidden p-0.5 flex items-center">
              <!-- Live Level Fill -->
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

              <!-- Threshold Marker Line -->
              <div
                class="absolute top-0 bottom-0 w-1 bg-white shadow-lg pointer-events-none z-10 transition-all"
                :style="{ left: `${threshold}%` }"
              ></div>
            </div>

            <div class="flex items-center justify-between text-xs font-mono text-mnema-tertiary">
              <span :class="isAboveThreshold ? 'text-mnema-accent font-bold' : 'text-mnema-warning'">
                Aktueller Pegel: {{ voiceStore.currentInputLevel }}% · Spitze: {{ peakLevel }}%
              </span>
              <span class="text-mnema-text font-bold">
                Schwellenwert: {{ threshold }}%
              </span>
            </div>

            <!-- Manual Slider Control -->
            <div v-if="!voiceStore.autoSensitivity" class="space-y-1">
              <input
                type="range"
                min="0"
                max="100"
                v-model="voiceStore.sensitivityThreshold"
                @input="handleSliderChange"
                class="w-full accent-mnema-accent cursor-pointer"
              />
            </div>

            <!-- Helpful Room Advice Note -->
            <div class="p-3 rounded-lg bg-mnema-surface border border-mnema-hairline text-sm text-mnema-tertiary space-y-1">
              <div class="flex items-center gap-1.5 text-mnema-text font-semibold text-xs">
                <HelpCircle class="w-4 h-4 text-mnema-mint" />
                <span>Empfehlung für gemeinsame Räume / zwei Personen:</span>
              </div>
              <p class="text-xs leading-relaxed">
                Lasse deine Freundin hinter dir normal sprechen und beobachte den Ausschlag oben: Stelle den Schieberegler so ein, dass ihre Hintergrundstimme <strong>unterhalb</strong> des weißen Strichs im gelben Bereich bleibt (ca. <strong>30% – 45%</strong>). Sobald du selbst sprichst, schlägt die Leiste grün über den Strich aus und überträgt nur deine Stimme.
              </p>
            </div>
          </div>
        </div>

        <!-- 3. Audio Processing Settings (Crucial AGC Toggle for Shared Rooms) -->
        <div class="space-y-2">
          <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
            Erweiterte Sprachverarbeitung
          </label>

          <div class="rounded-lg bg-mnema-surface border border-mnema-hairline divide-y divide-mnema-hairline">
            <!-- Automatic Gain Control (AGC) Toggle -->
            <div class="p-3.5 flex items-start justify-between gap-4">
              <div class="space-y-0.5">
                <div class="text-sm font-semibold text-mnema-text flex items-center gap-1.5">
                  <span>Automatische Lautstärkeregelung (AGC)</span>
                  <span class="text-xs px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-400 font-mono">Wichtig bei 2 Personen</span>
                </div>
                <p class="text-xs text-mnema-tertiary leading-relaxed">
                  <strong>Empfohlen: DEAKTIVIERT.</strong> Wenn AGC aktiv ist, pegelt das Mikrofon in Sprechpausen leise Geräusche künstlich hoch – wodurch die Stimme im Hintergrund laut übertragen wird.
                </p>
              </div>
              <button
                @click="toggleAgc"
                :class="[
                  'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
                  voiceStore.autoGainControl ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
                ]"
              >
                <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.autoGainControl ? 'translate-x-5' : 'translate-x-0']"></div>
              </button>
            </div>

            <!-- Browser noise suppression (getUserMedia noiseSuppression) -->
            <div class="p-3.5 flex items-start justify-between gap-4">
              <div class="space-y-0.5">
                <div class="text-sm font-semibold text-mnema-text flex items-center gap-1.5">
                  <Sparkles class="w-4 h-4 text-mnema-mint" />
                  <span>Rauschunterdrückung</span>
                  <span class="text-xs px-1.5 py-0.2 rounded bg-mnema-accent/15 text-mnema-accent font-mono">Browser</span>
                </div>
                <p class="text-xs text-mnema-tertiary leading-relaxed">
                  Die eingebaute Rauschunterdrückung deines Browsers dämpft gleichmäßige Hintergrundgeräusche wie Lüfter oder Brummen.
                </p>
              </div>
              <button
                @click="toggleNoise"
                :class="[
                  'w-10 h-5 rounded-full transition-colors relative flex items-center px-0.5 flex-shrink-0 mt-1',
                  voiceStore.noiseCancelling ? 'bg-mnema-accent' : 'bg-mnema-canvas border border-mnema-border'
                ]"
              >
                <div :class="['w-4 h-4 rounded-full bg-white transition-transform shadow-sm', voiceStore.noiseCancelling ? 'translate-x-5' : 'translate-x-0']"></div>
              </button>
            </div>

            <!-- Echo Cancellation -->
            <div class="p-3.5 flex items-start justify-between gap-4">
              <div class="space-y-0.5">
                <div class="text-sm font-semibold text-mnema-text">Echounterdrückung (AEC)</div>
                <p class="text-xs text-mnema-tertiary leading-relaxed">
                  Verhindert Rückkopplungen und Echos, falls Ton aus Lautsprechern wieder ins Mikrofon gelangt.
                </p>
              </div>
              <button
                @click="toggleEcho"
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

        <!-- 4. Device Selection -->
        <div class="space-y-3">
          <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
            Geräteauswahl
          </label>

          <div class="space-y-2">
            <div>
              <label class="text-xs text-mnema-tertiary font-mono block mb-1">Eingabegerät (Mikrofon)</label>
              <select
                v-model="voiceStore.selectedInputDeviceId"
                @change="handleDeviceChange"
                class="w-full bg-mnema-canvas border border-mnema-border rounded-lg px-3 py-2 text-sm text-mnema-text focus:outline-none focus:border-mnema-accent focus:ring-1 focus:ring-mnema-accent"
              >
                <option value="">(Standard Mikrofon des Systems)</option>
                <option 
                  v-for="dev in voiceStore.availableInputDevices" 
                  :key="dev.deviceId" 
                  :value="dev.deviceId"
                >
                  {{ dev.label || `Mikrofon ${dev.deviceId.slice(0, 5)}...` }}
                </option>
              </select>
            </div>
          </div>
        </div>
      </div>

      <!-- Footer -->
      <footer class="px-6 py-3.5 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-between">
        <span class="text-xs text-mnema-tertiary font-mono">
          Änderungen werden sofort aktiv
        </span>
        <button
          @click="emit('close')"
          class="px-5 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition shadow-sm"
        >
          Fertig
        </button>
      </footer>
    </div>
  </div>
</template>
