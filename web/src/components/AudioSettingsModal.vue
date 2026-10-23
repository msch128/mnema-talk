<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useWebRTC } from '../composables/useWebRTC'
import BaseDialog from './BaseDialog.vue'
import AudioInputSection from './AudioInputSection.vue'
import AudioProcessingSection from './AudioProcessingSection.vue'
import AudioSoundsSection from './AudioSoundsSection.vue'
import AudioDevicesSection from './AudioDevicesSection.vue'

const emit = defineEmits<{ close: [] }>()
const { refreshAudioDevices, stopMicTest } = useWebRTC()

// Listing devices does not request capture. A local test needs its own action.
onMounted(() => refreshAudioDevices())

onUnmounted(() => {
  stopMicTest()
})
</script>

<template>
  <BaseDialog :title="$t('audio.settings')" :subtitle="$t('audio.subtitle')" panel-class="max-w-xl select-none" @close="emit('close')">
    <div class="contents">
      <!-- Settings Body -->
      <div class="p-6 space-y-6 overflow-y-auto max-h-[80vh]">
        <AudioInputSection />
        <AudioProcessingSection />
        <AudioSoundsSection />
        <AudioDevicesSection />
      </div>

      <!-- Footer -->
      <footer class="px-6 py-3.5 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-between">
        <span class="text-xs text-mnema-tertiary font-mono">
          {{ $t('audio.liveNote') }}
        </span>
        <button
          @click="emit('close')"
          class="px-5 py-2 rounded-lg bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold text-sm transition shadow-xs"
        >
          {{ $t('audio.done') }}
        </button>
      </footer>
    </div>
  </BaseDialog>
</template>
