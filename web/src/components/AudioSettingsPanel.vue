<script setup lang="ts">
// The audio section of the settings dialog: the microphone check first, then
// output, filter and how you speak, then Fine-tune behind a disclosure. Every
// change applies at once and says "Saved" (a personal preference, doc/settings-ux.md).
import { onMounted, onUnmounted } from 'vue'
import { useWebRTC } from '../composables/useWebRTC'
import { usePreferenceSave } from '../composables/usePreferenceSave'
import { useVoiceStore } from '../stores/voice'
import SavedIndicator from './SavedIndicator.vue'
import AudioMicCheck from './AudioMicCheck.vue'
import AudioInputSection from './AudioInputSection.vue'
import AudioOutputSection from './AudioOutputSection.vue'
import AudioProcessingSection from './AudioProcessingSection.vue'
import AudioAdvancedSection from './AudioAdvancedSection.vue'
import ConnectionTest from './ConnectionTest.vue'

const voice = useVoiceStore()
const pref = usePreferenceSave()
// Every control stores through saveSettings; the indicator follows it.
voice.$onAction(({ name, after }) => { if (name === 'saveSettings') after(pref.flash) })
const { refreshAudioDevices, stopMicTest } = useWebRTC()

// Listing devices does not request capture. A local test needs its own action.
// The flag tells the call code to publish the live input level for the meter.
onMounted(() => {
  voice.showAudioSettings = true
  void refreshAudioDevices()
})

onUnmounted(() => {
  voice.showAudioSettings = false
  stopMicTest()
})
</script>

<template>
  <div class="space-y-8">
    <div class="-mb-4 flex justify-end"><SavedIndicator :visible="pref.saved.value" /></div>
    <AudioMicCheck />
    <AudioOutputSection />
    <AudioProcessingSection />
    <AudioInputSection />
    <AudioAdvancedSection />
    <section class="settings-group" aria-labelledby="audio-group-connection">
      <h3 id="audio-group-connection" class="settings-label">{{ $t('connectionTest.title') }}</h3>
      <div class="settings-card"><ConnectionTest :heading="false" /></div>
    </section>
    <p class="font-mono text-xs text-mnema-tertiary">{{ $t('audio.liveNote') }}</p>
  </div>
</template>
