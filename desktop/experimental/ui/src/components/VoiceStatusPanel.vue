<script setup lang="ts">
// Shown above the user bar only while connected to a Talk.
import { computed } from 'vue'
import { Monitor, MonitorOff, PhoneOff, Volume2, VolumeX } from '@lucide/vue'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useWebRTC } from '../composables/useWebRTC'
import { pingTone, PING_CLASS } from '../lib/ping'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare } = useWebRTC()

const channelName = computed(
  () => chatStore.allChannels.find(c => c.id === voiceStore.currentChannelId)?.name || ''
)
const ping = computed(() => voiceStore.ping ?? voiceStore.avgPing)
const pingClass = computed(() => PING_CLASS[pingTone(ping.value)])

function toggleScreenShare() {
  if (voiceStore.isScreenSharing) stopScreenShare()
  else startScreenShare()
}

const ib = 'flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent'
</script>

<template>
  <section
    :aria-label="$t('voice.panel.label')"
    class="vpanel-wrap flex items-center gap-1 border-t border-mnema-hairline py-2.5 pl-3 pr-2"
  >
    <div class="flex min-w-0 flex-1 flex-col gap-0.5">
      <button
        type="button"
        data-testid="voice-panel-open"
        v-tooltip.visual="$t('voice.panel.open')"
        class="flex min-w-0 items-center gap-2 text-left text-sm font-semibold leading-[18px] text-mnema-accent hover:text-mnema-accent-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
        @click="voiceStore.activeView = 'voice'"
      >
        <span class="h-2 w-2 flex-shrink-0 rounded-full bg-mnema-accent"></span>
        <span class="truncate">{{ $t('voice.panel.connected', { channel: channelName }) }}</span>
      </button>
      <!-- The ping opens the connection details. -->
      <button
        type="button"
        data-testid="voice-panel-ping"
        :data-tone="pingTone(ping)"
        v-tooltip="$t('voice.panel.details')"
        class="flex items-center gap-1.5 self-start pl-4 text-xs leading-4 rounded-sm hover:underline underline-offset-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
        @click="voiceStore.showStatsModal = true"
      >
        <span :class="['font-mono font-medium tabular-nums', pingClass]">{{ ping == null ? '–' : ping }} ms</span>
        <span class="vp-narrow-hide text-mnema-border-strong">·</span>
        <span class="vp-narrow-hide truncate text-mnema-tertiary">{{ $t('voice.panel.details') }}</span>
      </button>
    </div>

    <button
      type="button"
      data-testid="voice-panel-share"
      v-tooltip="voiceStore.isScreenSharing ? $t('voice.stopShare') : $t('voice.share')"
      :aria-pressed="voiceStore.isScreenSharing ? 'true' : 'false'"
      :class="[ib, 'vp-narrow-hide', voiceStore.isScreenSharing ? 'bg-mnema-accent text-mnema-accent-ink' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
      @click="toggleScreenShare"
    >
      <MonitorOff v-if="voiceStore.isScreenSharing" class="h-[18px] w-[18px]" />
      <Monitor v-else class="h-[18px] w-[18px]" />
    </button>
    <button
      v-if="voiceStore.isScreenSharing"
      type="button"
      data-testid="voice-panel-stream-audio"
      v-tooltip="voiceStore.isScreenAudioMuted ? $t('talk.unmuteStreamAudio') : $t('talk.muteStreamAudio')"
      :aria-pressed="voiceStore.isScreenAudioMuted ? 'true' : 'false'"
      :class="[ib, 'vp-narrow-hide', voiceStore.isScreenAudioMuted ? 'text-mnema-danger hover:bg-mnema-danger/[0.12]' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
      @click="voiceStore.toggleScreenAudioMute"
    >
      <VolumeX v-if="voiceStore.isScreenAudioMuted" class="h-[18px] w-[18px]" />
      <Volume2 v-else class="h-[18px] w-[18px]" />
    </button>
    <button
      type="button"
      data-testid="voice-panel-leave"
      v-tooltip="$t('voice.leave')"
      :class="[ib, 'text-mnema-danger hover:bg-mnema-danger/[0.12]']"
      @click="leaveVoiceChannel"
    >
      <PhoneOff class="h-[18px] w-[18px]" />
    </button>
  </section>
</template>
