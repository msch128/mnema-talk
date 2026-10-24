<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useWebRTC } from '../composables/useWebRTC'
import BaseDialog from './BaseDialog.vue'
import AudioInputSection from './AudioInputSection.vue'
import AudioOutputSection from './AudioOutputSection.vue'
import AudioProcessingSection from './AudioProcessingSection.vue'
import AudioAdvancedSection from './AudioAdvancedSection.vue'

const emit = defineEmits<{ close: [] }>()
const { refreshAudioDevices, stopMicTest } = useWebRTC()

// Listing devices does not request capture. A local test needs its own action.
onMounted(() => refreshAudioDevices())

onUnmounted(() => {
  stopMicTest()
})
</script>

<template>
  <BaseDialog :title="$t('audio.settings')" :subtitle="$t('audio.subtitle')" panel-class="max-w-2xl select-none" @close="emit('close')">
    <div class="contents">
      <!-- Settings body: input, mic check, output, filter, then Advanced behind a disclosure -->
      <div class="max-h-[80vh] space-y-8 overflow-y-auto p-6 max-md:p-4">
        <AudioInputSection />
        <AudioOutputSection />
        <AudioProcessingSection />
        <AudioAdvancedSection />
      </div>

      <!-- Footer -->
      <footer class="flex items-center justify-between gap-4 border-t border-mnema-hairline bg-mnema-raised px-6 py-3.5">
        <span class="font-mono text-xs text-mnema-tertiary">
          {{ $t('audio.liveNote') }}
        </span>
        <button
          type="button"
          class="btn btn-primary"
          @click="emit('close')"
        >
          {{ $t('audio.done') }}
        </button>
      </footer>
    </div>
  </BaseDialog>
</template>
