<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { Mic, Radio } from '@lucide/vue'
import type { InputMode } from '../stores/voice'
import { useVoiceStore } from '../stores/voice'
import { pttKeyLabel } from '../lib/pttKey'
import { t } from '../i18n'

// How speaking is triggered: the microphone opens by voice, or while a key is
// held. The microphone itself and its check are in AudioMicCheck.
const voiceStore = useVoiceStore()

const isRecordingPttKey = ref(false)
const keyButton = ref<HTMLButtonElement | null>(null)

// The key only listens during a Talk or the microphone test, so that is where trying it makes sense.
const keyState = computed(() => {
  if (voiceStore.isPttPressed) return t('audio.pttKeyHeld')
  const key = pttKeyLabel(voiceStore.pttKey)
  return t(voiceStore.isMicTesting || voiceStore.isConnected ? 'audio.pttKeyIdle' : 'audio.pttKeyIdleTest', { key })
})

function handleInputModeChange(mode: InputMode) {
  voiceStore.inputMode = mode
  voiceStore.saveSettings()
  // Push to talk without a key does nothing: ask for the key right away.
  isRecordingPttKey.value = mode === 'ptt' && !voiceStore.pttKey
  // The key is read from the button, so it needs the focus.
  if (isRecordingPttKey.value) void nextTick(() => keyButton.value?.focus())
}

function handleKeyRecord(e: KeyboardEvent) {
  if (!isRecordingPttKey.value) return
  // Tab keeps moving focus (the blur ends recording) and a lone modifier is not a key to bind.
  if (e.key === 'Tab' || ['Shift', 'Control', 'Alt', 'Meta'].includes(e.key)) return
  e.preventDefault()
  if (e.key === 'Escape') {
    isRecordingPttKey.value = false
    return
  }
  voiceStore.pttKey = e.code || e.key
  isRecordingPttKey.value = false
  voiceStore.saveSettings()
}
</script>

<template>
  <!-- How speaking is triggered -->
  <section class="settings-group" aria-labelledby="audio-group-input">
    <h3 id="audio-group-input" class="settings-label">{{ $t('audio.groupInput') }}</h3>
    <p class="settings-help">{{ $t('audio.groupInputHelp') }}</p>

    <div class="settings-card">
      <div class="stack-3">
        <div class="settings-row-title" id="audio-input-mode">{{ $t('audio.inputMode') }}</div>
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
        <div v-if="voiceStore.inputMode === 'ptt'" class="stack-2 rounded-lg border border-mnema-hairline bg-mnema-canvas/50 px-4 py-3" data-testid="ptt-key-panel">
          <div class="flex flex-wrap items-center justify-between gap-3 text-sm">
            <span class="settings-row-title">{{ $t('audio.pttKey') }}</span>
            <button
              ref="keyButton"
              type="button"
              data-testid="ptt-key-button"
              :class="['btn', isRecordingPttKey ? 'btn-primary animate-pulse' : 'btn-secondary font-mono']"
              @click="isRecordingPttKey = true"
              @keydown="handleKeyRecord"
              @blur="isRecordingPttKey = false"
            >
              {{ isRecordingPttKey ? $t('audio.pressKey') : voiceStore.pttKey ? pttKeyLabel(voiceStore.pttKey) : $t('audio.pttKeyChoose') }}
            </button>
          </div>
          <!-- Live: lights up while the key is held, so a key that does nothing shows at once -->
          <p
            v-if="voiceStore.pttKey"
            role="status"
            data-testid="ptt-key-state"
            :class="['flex items-center gap-2 text-xs', voiceStore.isPttPressed ? 'font-medium text-mnema-accent' : 'text-mnema-tertiary']"
          >
            <span :class="['h-2 w-2 rounded-full', voiceStore.isPttPressed ? 'bg-mnema-accent' : 'bg-mnema-tertiary/50']" aria-hidden="true"></span>
            {{ keyState }}
          </p>
          <p v-else class="text-xs text-mnema-warning">{{ $t('audio.pttKeyNone') }}</p>
          <p class="settings-hint">{{ $t('audio.pttKeyHint') }}</p>
        </div>
      </div>
    </div>
  </section>
</template>
