<script setup lang="ts">
import { Volume2, VideoOff } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { focusRing } from './talkButtons'

// What the Talk area says when there is no stage and no grid to show:
// everyone's video is hidden, or nobody is there yet (preview).
defineProps({
  variant: { type: String as () => 'no-video' | 'empty', required: true },
  connected: { type: Boolean, default: false },
  canJoin: { type: Boolean, default: false }
})
const emit = defineEmits<{ join: [] }>()

const voiceStore = useVoiceStore()
</script>

<template>
  <!-- Everyone is hidden: nobody has video on -->
  <div
    v-if="variant === 'no-video'"
    data-testid="no-video-hint"
    class="flex-1 flex flex-col items-center justify-center text-center max-w-sm gap-1"
  >
    <div class="w-12 h-12 mb-2 rounded-full bg-mnema-surface border border-mnema-hairline flex items-center justify-center" aria-hidden="true">
      <VideoOff class="w-5 h-5 text-mnema-tertiary" />
    </div>
    <p class="font-semibold text-base text-mnema-text">{{ $t('talk.noVideoTitle') }}</p>
    <p class="text-sm text-mnema-tertiary">{{ $t('talk.noVideoHint') }}</p>
    <button
      type="button"
      :class="['mt-3 h-8 px-3 rounded-md text-sm font-medium border border-mnema-hairline bg-mnema-surface text-mnema-text hover:bg-mnema-hover transition-colors', focusRing]"
      @click="voiceStore.setHideNoVideo(false)"
    >
      {{ $t('talk.showAllParticipants') }}
    </button>
  </div>

  <!-- Nobody there yet (preview): what to do, right where one looks -->
  <div
    v-else
    data-testid="talk-empty"
    class="flex-1 flex flex-col items-center justify-center text-center max-w-sm gap-1"
  >
    <div class="w-14 h-14 mb-3 rounded-full bg-mnema-band border border-mnema-mint/30 flex items-center justify-center" aria-hidden="true">
      <Volume2 class="w-6 h-6 text-mnema-mint" />
    </div>
    <p class="font-semibold text-base text-mnema-text">{{ $t('voice.noOneInVoice') }}</p>
    <p class="text-sm text-mnema-tertiary">{{ $t('voice.joinToTalk') }}</p>
    <button
      v-if="!connected"
      type="button"
      @click="emit('join')"
      :disabled="!canJoin"
      :class="['mt-4 flex items-center gap-2 h-10 px-6 rounded-full text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition-colors shadow-lg disabled:opacity-40', focusRing, 'focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-canvas']"
    >
      <Volume2 class="w-4 h-4" />
      <span>{{ $t('voice.join') }}</span>
    </button>
  </div>
</template>
