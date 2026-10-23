<script setup lang="ts">
import { Play } from '@lucide/vue'
import type { SoundEvent } from '../stores/voice'
import { useVoiceStore, SOUND_EVENTS } from '../stores/voice'
import { playSoundEffect } from '../lib/soundEffects'

// Sound effects: on or off, their volume, and which events make a sound.
const voiceStore = useVoiceStore()

function toggleSoundEffects() {
  voiceStore.soundEffectsEnabled = !voiceStore.soundEffectsEnabled
  voiceStore.saveSettings()
  if (voiceStore.soundEffectsEnabled) {
    playSoundEffect('unmute')
  }
}

function handleSoundsVolumeChange(e: Event) {
  voiceStore.soundEffectsVolume = parseInt((e.target as HTMLInputElement).value, 10)
  voiceStore.saveSettings()
}

function playPreviewSound(sound: SoundEvent = 'join') {
  playSoundEffect(sound, null, true)
}
</script>

<template>
  <!-- 6. Sound Effects -->
  <div class="stack-2">
    <label class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">
      {{ $t('audio.soundsTitle') }}
    </label>
    <div class="rounded-lg bg-mnema-surface border border-mnema-hairline p-3.5 stack-3">
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
          class="switch mt-1"
        >
        </button>
      </div>

      <!-- Volume slider when enabled -->
      <div v-if="voiceStore.soundEffectsEnabled" class="space-y-1 pt-2 border-t border-mnema-hairline">
        <div class="flex items-center justify-between text-xs">
          <span class="text-mnema-tertiary font-mono">{{ $t('audio.soundsVolume') }}</span>
          <div class="flex items-center gap-2">
            <button
              type="button"
              @click="playPreviewSound()"
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
          class="w-full cursor-pointer"
        />
      </div>

      <!-- Granular sound effects toggles -->
      <div v-if="voiceStore.soundEffectsEnabled" class="stack-1.5 pt-3 border-t border-mnema-hairline">
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
                class="w-6 h-6 rounded-sm flex items-center justify-center bg-mnema-raised text-mnema-tertiary hover:text-mnema-accent hover:bg-mnema-surface transition shrink-0"
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
              class="switch"
            >
              <div :class="['w-3 h-3 rounded-full bg-white transition-transform shadow-xs', voiceStore.soundEvents[sound] !== false ? 'translate-x-4' : 'translate-x-0']"></div>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
