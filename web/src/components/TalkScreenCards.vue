<script setup lang="ts">
import type { PropType } from 'vue'
import type { TalkScreenCard } from '../composables/useTalkStage'
import { Monitor, X } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import ScreenViewers from './ScreenViewers.vue'
import StreamQualityMenu from './StreamQualityMenu.vue'
import { focusRing } from './talkButtons'

// Screen shares not on the stage: mine, ones I watch, ones I can opt into.
defineProps({
  cards: { type: Array as PropType<TalkScreenCard[]>, required: true }
})
const emit = defineEmits<{ select: [card: TalkScreenCard]; 'open-change': [open: boolean] }>()

const voiceStore = useVoiceStore()
const authStore = useAuthStore()
</script>

<template>
  <div
    v-for="card in cards"
    :key="card.key"
    data-testid="screen-card"
    :data-screen-card="card.key"
    class="flex items-center gap-3 min-w-0 max-w-full rounded-lg border border-mnema-accent/30 bg-mnema-accent-subtle pl-3 pr-1.5 py-1.5"
  >
    <Monitor class="w-4 h-4 text-mnema-accent flex-shrink-0" />
    <span class="text-sm text-mnema-text truncate">
      {{ card.kind === 'own' ? $t('talk.ownScreen') : $t('talk.screenShareCard', { name: card.user.display_name || card.user.username }) }}
    </span>
    <ScreenViewers
      :user-id="card.kind === 'own' ? (authStore.user?.id || '') : card.user.id"
      :show-zero="card.kind === 'own'"
      variant="card"
    />
    <button
      type="button"
      data-testid="screen-card-action"
      :disabled="card.state === 'pending'"
      :class="['h-7 px-3 rounded-md text-sm font-semibold bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover transition-colors disabled:opacity-60 flex-shrink-0', focusRing, 'focus-visible:ring-offset-2 focus-visible:ring-offset-mnema-accent-subtle']"
      @click="emit('select', card)"
    >
      {{ card.state === 'idle' ? $t('talk.watchScreen') : card.state === 'queued' ? $t('talk.toStage') : $t('talk.screenConnecting') }}
    </button>
    <StreamQualityMenu v-if="card.kind === 'own'" variant="card" @open-change="emit('open-change', $event)" />
    <button
      v-if="card.kind === 'remote' && card.state !== 'idle'"
      type="button"
      :class="['w-7 h-7 flex items-center justify-center rounded-md text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition-colors flex-shrink-0', focusRing]"
      v-tooltip="$t('talk.unwatchScreen')"
      @click="voiceStore.unwatchScreen(card.user.id)"
    >
      <X class="w-4 h-4" />
    </button>
  </div>
</template>
