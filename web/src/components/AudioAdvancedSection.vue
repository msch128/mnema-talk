<script setup lang="ts">
import { Play } from '@lucide/vue'
import type { SoundEvent } from '../stores/voice'
import { useVoiceStore, SOUND_EVENTS } from '../stores/voice'
import { useActiveAudioSettings } from '../composables/useActiveAudioSettings'
import { playSoundEffect } from '../lib/soundEffects'
import AudioFineTune from './AudioFineTune.vue'

// What most people never change: input volume and sensitivity, gain control, network priority, the two
// confirmation warnings and the individual sound effects.
const voiceStore = useVoiceStore()
const applyActiveAudioSettings = useActiveAudioSettings()

async function toggleAgc() {
  voiceStore.autoGainControl = !voiceStore.autoGainControl
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

async function toggleQos() {
  voiceStore.qosHighPriority = !voiceStore.qosHighPriority
  voiceStore.saveSettings()
  await applyActiveAudioSettings()
}

// Read when the next offer arrives, so it applies from the next call; nothing to apply to a running one.
function toggleSilenceSaving() {
  voiceStore.silenceSaving = !voiceStore.silenceSaving
  voiceStore.saveSettings()
}

function toggleWarnNoAudio() {
  voiceStore.warnNoAudioDetected = !voiceStore.warnNoAudioDetected
  voiceStore.saveSettings()
}

function toggleWarnSwitchChannel() {
  voiceStore.warnSwitchChannel = !voiceStore.warnSwitchChannel
  voiceStore.saveSettings()
}

function playPreviewSound(sound: SoundEvent = 'join') {
  playSoundEffect(sound, null, true)
}
</script>

<template>
  <details class="settings-disclosure" data-testid="audio-advanced">
    <summary>{{ $t('audio.advanced') }}</summary>
    <p class="settings-help mb-4">{{ $t('audio.advancedHelp') }}</p>

    <div class="stack-5">
      <AudioFineTune />

      <div class="settings-rows">
        <!-- Automatic gain control -->
        <div class="settings-row items-start">
          <div class="settings-row-text">
            <div class="settings-row-title flex flex-wrap items-center gap-2">
              <span>{{ $t('audio.agc') }}</span>
              <span class="rounded-sm bg-amber-500/15 px-1.5 py-0.5 font-mono text-xs font-medium text-amber-400">{{ $t('audio.agcBadge') }}</span>
            </div>
            <p class="settings-hint">{{ $t('audio.agcHint') }}</p>
          </div>
          <button type="button" role="switch" :aria-checked="voiceStore.autoGainControl ? 'true' : 'false'" :aria-label="$t('audio.agc')" class="switch mt-0.5" @click="toggleAgc"></button>
        </div>

        <!-- Opus DTX: almost nothing is sent while silent -->
        <div class="settings-row items-start">
          <div class="settings-row-text">
            <div class="settings-row-title">{{ $t('audio.silenceSavingTitle') }}</div>
            <p class="settings-hint">{{ $t('audio.silenceSavingHint') }}</p>
          </div>
          <button type="button" role="switch" data-testid="silence-saving" :aria-checked="voiceStore.silenceSaving ? 'true' : 'false'" :aria-label="$t('audio.silenceSavingTitle')" class="switch mt-0.5" @click="toggleSilenceSaving"></button>
        </div>

        <!-- Quality of service (high packet priority) -->
        <div class="settings-row items-start">
          <div class="settings-row-text">
            <div class="settings-row-title">{{ $t('audio.qosTitle') }}</div>
            <p class="settings-hint">{{ $t('audio.qosHint') }}</p>
          </div>
          <button type="button" role="switch" :aria-checked="voiceStore.qosHighPriority ? 'true' : 'false'" :aria-label="$t('audio.qosTitle')" class="switch mt-0.5" @click="toggleQos"></button>
        </div>
      </div>

      <div class="settings-group">
        <h4 class="settings-label">{{ $t('audio.warnings') }}</h4>
        <div class="settings-rows">
          <div class="settings-row items-start">
            <div class="settings-row-text">
              <div class="settings-row-title">{{ $t('audio.noAudioWarningTitle') }}</div>
              <p class="settings-hint">{{ $t('audio.noAudioWarningHint') }}</p>
            </div>
            <button type="button" role="switch" :aria-checked="voiceStore.warnNoAudioDetected ? 'true' : 'false'" :aria-label="$t('audio.noAudioWarningTitle')" class="switch mt-0.5" @click="toggleWarnNoAudio"></button>
          </div>
          <div class="settings-row items-start">
            <div class="settings-row-text">
              <div class="settings-row-title">{{ $t('audio.switchChannelWarningTitle') }}</div>
              <p class="settings-hint">{{ $t('audio.switchChannelWarningHint') }}</p>
            </div>
            <button type="button" role="switch" :aria-checked="voiceStore.warnSwitchChannel ? 'true' : 'false'" :aria-label="$t('audio.switchChannelWarningTitle')" class="switch mt-0.5" @click="toggleWarnSwitchChannel"></button>
          </div>
        </div>
      </div>

      <!-- Granular sound effects toggles -->
      <div v-if="voiceStore.soundEffectsEnabled" class="settings-group">
        <h4 class="settings-label">{{ $t('audio.soundsCustomTitle') }}</h4>
        <div class="settings-rows">
          <div v-for="sound in SOUND_EVENTS" :key="sound" class="settings-row py-3">
            <div class="flex items-center gap-3">
              <button
                type="button"
                :aria-label="$t('audio.soundsTest') + ': ' + $t(`audio.sound_${sound}`)"
                class="btn btn-icon btn-sm shrink-0"
                @click="playPreviewSound(sound)"
              >
                <Play class="ml-0.5 h-3 w-3 fill-current" />
              </button>
              <span class="text-sm text-mnema-text">{{ $t(`audio.sound_${sound}`) }}</span>
            </div>
            <button
              type="button"
              role="switch"
              :aria-checked="voiceStore.soundEvents[sound] !== false ? 'true' : 'false'"
              :aria-label="$t(`audio.sound_${sound}`)"
              class="switch"
              @click="voiceStore.toggleSoundEvent(sound)"
            ></button>
          </div>
        </div>
      </div>
    </div>
  </details>
</template>
