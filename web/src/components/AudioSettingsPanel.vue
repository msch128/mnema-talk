<script setup lang="ts">
// The audio section of the settings dialog: input, mic check, output, filter,
// then Advanced behind a disclosure. Every change applies at once.
import { onMounted, onUnmounted } from 'vue'
import { useWebRTC } from '../composables/useWebRTC'
import { useVoiceStore } from '../stores/voice'
import AudioInputSection from './AudioInputSection.vue'
import AudioOutputSection from './AudioOutputSection.vue'
import AudioProcessingSection from './AudioProcessingSection.vue'
import AudioAdvancedSection from './AudioAdvancedSection.vue'

const voice = useVoiceStore()
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
    <AudioInputSection />
    <AudioOutputSection />
    <AudioProcessingSection />
    <AudioAdvancedSection />
    <p class="font-mono text-xs text-mnema-tertiary">{{ $t('audio.liveNote') }}</p>
  </div>
</template>
