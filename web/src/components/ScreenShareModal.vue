<script setup>
import { ref, computed } from 'vue'
import { Monitor, Gamepad2, FileText, Sliders, Activity, Sparkles } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'
import {
  STREAM_RESOLUTIONS,
  STREAM_FRAME_RATES,
  STREAM_PRESETS,
  normalizeQuality,
  streamModeOf,
  streamBitrate
} from '../lib/streamQuality'
import { t } from '../i18n'

const SAVED_PREF_KEY = 'mnema_preferred_stream_quality'

const emit = defineEmits(['close'])

const voiceStore = useVoiceStore()
const { startScreenShare } = useWebRTC()

function loadPreferredQuality() {
  try {
    const raw = localStorage.getItem(SAVED_PREF_KEY)
    if (raw) return normalizeQuality(JSON.parse(raw))
  } catch {}
  return normalizeQuality(voiceStore.screenQuality)
}

const initialQuality = loadPreferredQuality()
const selectedResolution = ref(initialQuality.resolution)
const selectedFps = ref(initialQuality.fps)
const customPicked = ref(initialQuality.custom === true)

const currentMode = computed(() => {
  if (customPicked.value) return 'custom'
  return streamModeOf({ resolution: selectedResolution.value, fps: selectedFps.value })
})

function applyPreset(mode) {
  if (mode === 'gaming') {
    selectedResolution.value = STREAM_PRESETS.gaming.resolution
    selectedFps.value = STREAM_PRESETS.gaming.fps
    customPicked.value = false
  } else if (mode === 'screen') {
    selectedResolution.value = STREAM_PRESETS.screen.resolution
    selectedFps.value = STREAM_PRESETS.screen.fps
    customPicked.value = false
  } else {
    customPicked.value = true
  }
}

function selectResolution(r) {
  selectedResolution.value = r
  checkIfCustomMatchesPreset()
}

function selectFps(fps) {
  selectedFps.value = fps
  checkIfCustomMatchesPreset()
}

function checkIfCustomMatchesPreset() {
  const mode = streamModeOf({ resolution: selectedResolution.value, fps: selectedFps.value })
  if (mode === 'custom') {
    customPicked.value = true
  } else {
    customPicked.value = false
  }
}

const estimatedBitrateDisplay = computed(() => {
  const bps = streamBitrate({ resolution: selectedResolution.value, fps: selectedFps.value })
  if (bps >= 1_000_000) {
    const mbps = (bps / 1_000_000).toFixed(1)
    return `${mbps} Mbit/s`
  }
  const kbps = Math.round(bps / 1000)
  return `${kbps} kbit/s`
})

const isMotionPriority = computed(() => selectedFps.value >= 60)

async function handleStart() {
  const q = {
    resolution: selectedResolution.value,
    fps: selectedFps.value,
    custom: currentMode.value === 'custom'
  }
  try {
    localStorage.setItem(SAVED_PREF_KEY, JSON.stringify(q))
  } catch {}
  voiceStore.setScreenQuality(q)
  emit('close')
  if (!voiceStore.isScreenSharing) {
    await startScreenShare(q)
  }
}
</script>

<template>
  <BaseDialog
    :title="t('talk.quality.streamModalTitle')"
    :subtitle="t('talk.quality.streamModalSubtitle')"
    panel-class="max-w-lg"
    @close="emit('close')"
  >
    <div class="space-y-5 p-5">
      <!-- 1. Presets -->
      <div class="space-y-2">
        <label class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ t('talk.quality.preset') }}
        </label>
        <div class="grid grid-cols-3 gap-2">
          <!-- Gaming Preset -->
          <button
            type="button"
            data-testid="preset-gaming"
            @click="applyPreset('gaming')"
            :class="[
              'flex flex-col items-center gap-1.5 p-3 rounded-lg border text-center transition cursor-pointer',
              currentMode === 'gaming'
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <div :class="['w-8 h-8 rounded-md flex items-center justify-center flex-shrink-0', currentMode === 'gaming' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Gamepad2 class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-semibold truncate">{{ t('talk.quality.gaming') }}</div>
              <div class="text-[11px] text-mnema-tertiary font-mono">{{ `${STREAM_PRESETS.gaming.resolution}p · ${t('talk.quality.fps', { fps: STREAM_PRESETS.gaming.fps })}` }}</div>
            </div>
          </button>

          <!-- Screen Preset -->
          <button
            type="button"
            data-testid="preset-screen"
            @click="applyPreset('screen')"
            :class="[
              'flex flex-col items-center gap-1.5 p-3 rounded-lg border text-center transition cursor-pointer',
              currentMode === 'screen'
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <div :class="['w-8 h-8 rounded-md flex items-center justify-center flex-shrink-0', currentMode === 'screen' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <FileText class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-semibold truncate">{{ t('talk.quality.screen') }}</div>
              <div class="text-[11px] text-mnema-tertiary font-mono">{{ `${t('talk.quality.source')} · ${t('talk.quality.fps', { fps: STREAM_PRESETS.screen.fps })}` }}</div>
            </div>
          </button>

          <!-- Custom Preset -->
          <button
            type="button"
            data-testid="preset-custom"
            @click="applyPreset('custom')"
            :class="[
              'flex flex-col items-center gap-1.5 p-3 rounded-lg border text-center transition cursor-pointer',
              currentMode === 'custom'
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <div :class="['w-8 h-8 rounded-md flex items-center justify-center flex-shrink-0', currentMode === 'custom' ? 'bg-mnema-accent/20 text-mnema-accent' : 'bg-mnema-raised text-mnema-tertiary']">
              <Sliders class="w-4 h-4" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-semibold truncate">{{ t('talk.quality.custom') }}</div>
              <div class="text-[11px] text-mnema-tertiary font-mono">{{ `${selectedResolution === 'source' ? t('talk.quality.source') : `${selectedResolution}p`} · ${t('talk.quality.fps', { fps: selectedFps })}` }}</div>
            </div>
          </button>
        </div>
      </div>

      <!-- 2. Resolution (Auflösung) -->
      <div class="space-y-2">
        <label class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ t('talk.quality.resolution') }}
        </label>
        <div class="grid grid-cols-4 gap-2">
          <button
            v-for="r in STREAM_RESOLUTIONS"
            :key="r"
            type="button"
            :data-testid="`resolution-btn-${r}`"
            @click="selectResolution(r)"
            :class="[
              'py-2 px-3 rounded-lg border text-center font-medium transition cursor-pointer flex flex-col items-center justify-center gap-0.5',
              selectedResolution === r
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <span class="text-sm font-semibold">
              {{ r === 'source' ? t('talk.quality.source') : `${r}p` }}
            </span>
            <span v-if="r === 'source'" class="text-[10px] text-mnema-accent font-mono font-medium">
              {{ '4K' }}
            </span>
          </button>
        </div>
      </div>

      <!-- 3. Frame Rate (Bildrate) -->
      <div class="space-y-2">
        <label class="text-xs font-medium text-mnema-tertiary uppercase tracking-wider font-mono">
          {{ t('talk.quality.frameRate') }}
        </label>
        <div class="grid grid-cols-3 gap-2">
          <button
            v-for="fps in STREAM_FRAME_RATES"
            :key="fps"
            type="button"
            :data-testid="`fps-btn-${fps}`"
            @click="selectFps(fps)"
            :class="[
              'py-2 px-3 rounded-lg border text-center font-medium transition cursor-pointer flex items-center justify-center gap-1.5',
              selectedFps === fps
                ? 'bg-mnema-surface border-mnema-accent text-mnema-text ring-1 ring-mnema-accent/40'
                : 'bg-mnema-canvas border-mnema-hairline text-mnema-muted hover:border-mnema-border'
            ]"
          >
            <span class="text-sm font-semibold">{{ t('talk.quality.fps', { fps }) }}</span>
          </button>
        </div>
      </div>

      <!-- 4. Estimated Bitrate & Tuning Hint -->
      <div class="rounded-lg border border-mnema-hairline bg-mnema-canvas/60 p-3 space-y-2">
        <div class="flex items-center justify-between text-xs">
          <div class="flex items-center gap-1.5 text-mnema-muted">
            <Activity class="w-3.5 h-3.5 text-mnema-accent" />
            <span>{{ t('talk.quality.estimatedBitrate', { bitrate: estimatedBitrateDisplay }) }}</span>
          </div>
          <span class="text-[11px] font-mono px-1.5 py-0.5 rounded bg-mnema-raised text-mnema-tertiary">
            {{ 'H.264' }}
          </span>
        </div>
        <p class="text-xs text-mnema-tertiary flex items-start gap-1.5">
          <Sparkles class="w-3.5 h-3.5 text-mnema-mint flex-shrink-0 mt-0.5" />
          <span>{{ isMotionPriority ? t('talk.quality.motionPriority') : t('talk.quality.detailPriority') }}</span>
        </p>
      </div>

      <!-- 5. Actions Footer -->
      <div class="pt-3 flex items-center justify-end gap-2 border-t border-mnema-hairline">
        <button
          type="button"
          data-testid="stream-modal-cancel"
          @click="emit('close')"
          class="h-9 px-4 rounded-md border border-mnema-border hover:bg-mnema-hover text-mnema-text text-sm font-semibold transition cursor-pointer"
        >
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          data-testid="stream-modal-submit"
          @click="handleStart"
          class="h-9 px-4 rounded-md bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition flex items-center gap-1.5 cursor-pointer"
        >
          <Monitor class="w-4 h-4" />
          <span>{{ voiceStore.isScreenSharing ? t('talk.quality.applyQuality') : t('talk.quality.startStream') }}</span>
        </button>
      </div>
    </div>
  </BaseDialog>
</template>
