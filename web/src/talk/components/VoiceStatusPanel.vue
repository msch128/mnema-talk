<script setup lang="ts">
// Shown above the user bar only while connected to a Talk: where you are, how
// the connection is, and the call's controls. Three rows, each with the whole
// width, so nothing is squeezed beside the buttons: the status and the Talk,
// the ping with the connection details, then the controls (microphone and
// sound, which leave the user bar room for the name and status line, screen
// sharing, and Leave at the end).
import { barIconButton } from './talkButtons'
import { computed, ref } from 'vue'
import { Headphones, HeadphoneOff, Mic, MicOff, Monitor, MonitorOff, PhoneOff, Volume2, VolumeX } from '@lucide/vue'
import { useVoiceStore } from '../../stores/voice'
import { useChatStore } from '../../stores/chat'
import { useWebRTC } from '../composables/useWebRTC'
import { pingTone, PING_CLASS } from '../lib/ping'
import TriggerMenu from '../../shared/components/TriggerMenu.vue'
import { useVoiceMenus } from '../composables/useVoiceMenus'
import { isTextCut } from '../../directives/fullText'
import { t } from '../../i18n'

const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare } = useWebRTC()
// Right click or long press on a control: its devices or the stream quality.
const { mic, output, share } = useVoiceMenus()

const channelName = computed(
  () => chatStore.allChannels.find(c => c.id === voiceStore.currentChannelId)?.name || ''
)
const ping = computed(() => voiceStore.ping ?? voiceStore.avgPing)
const pingClass = computed(() => PING_CLASS[pingTone(ping.value)])

// The tooltip names the Talk only while its name is cut, so a name that fits is not read twice.
const nameEl = ref<HTMLElement | null>(null)
const nameCut = ref(false)
const measureName = () => { nameCut.value = isTextCut(nameEl.value) }
const openTip = computed(() => nameCut.value ? t('voice.panel.openNamed', { channel: channelName.value }) : t('voice.panel.open'))

function toggleScreenShare() {
  if (voiceStore.isScreenSharing) stopScreenShare()
  else startScreenShare()
}

const ib = barIconButton
const tone = (active: boolean) => active
  ? 'bg-mnema-danger/12 text-mnema-danger'
  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
</script>

<template>
  <section
    :aria-label="$t('voice.panel.label')"
    class="vpanel-wrap flex flex-col gap-0.5 border-t border-mnema-hairline pb-2 pl-3 pr-2 pt-2.5"
  >
    <!-- "Connected" always stays whole; a long Talk name truncates. -->
    <button
      type="button"
      data-testid="voice-panel-open"
      v-tooltip.visual="openTip"
      :aria-label="$t('voice.panel.connected', { channel: channelName })"
      class="flex min-w-0 items-center gap-1 text-left text-sm font-semibold leading-5 text-mnema-accent hover:text-mnema-accent-hover focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-mnema-accent"
      @pointerenter="measureName"
      @focus="measureName"
      @click="voiceStore.activeView = 'voice'"
    >
      <span class="mr-1 h-2 w-2 shrink-0 rounded-full bg-mnema-accent"></span>
      <span class="shrink-0">{{ $t('voice.panel.status') }}</span>
      <span class="vp-name shrink-0" aria-hidden="true">·</span>
      <span ref="nameEl" data-testid="voice-panel-channel" class="vp-name min-w-0 truncate">{{ channelName }}</span>
    </button>

    <!-- The ping opens the connection details; the value is part of its name. -->
    <button
      type="button"
      data-testid="voice-panel-ping"
      :data-tone="pingTone(ping)"
      v-tooltip.visual="$t('voice.panel.details')"
      class="flex min-w-0 max-w-full items-center gap-1 self-start whitespace-nowrap pl-4 text-xs leading-4 rounded-xs hover:underline underline-offset-2 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-mnema-accent"
      @click="voiceStore.showStatsModal = true"
    >
      <span :class="['shrink-0 font-mono font-medium tabular-nums', pingClass]">{{ ping == null ? '–' : ping }} ms</span>
      <span class="shrink-0 text-mnema-border-strong" aria-hidden="true">·</span>
      <span class="min-w-0 truncate text-mnema-tertiary">{{ $t('voice.panel.details') }}</span>
    </button>

    <div class="mt-1.5 flex items-center gap-1" role="group" :aria-label="$t('talk.controls')">
      <button
        type="button"
        data-testid="toggle-mute"
        v-tooltip="voiceStore.isMuted ? $t('voice.unmute') : $t('voice.mute')"
        :aria-pressed="voiceStore.isMuted ? 'true' : 'false'"
        :class="[ib, tone(voiceStore.isMuted)]"
        v-bind="mic.on"
        @click="voiceStore.toggleMute"
      >
        <MicOff v-if="voiceStore.isMuted" class="h-[18px] w-[18px]" />
        <Mic v-else class="h-[18px] w-[18px]" />
      </button>
      <button
        type="button"
        data-testid="toggle-deafen"
        v-tooltip="voiceStore.isDeafened ? $t('voice.undeafen') : $t('voice.deafen')"
        :aria-pressed="voiceStore.isDeafened ? 'true' : 'false'"
        :class="[ib, tone(voiceStore.isDeafened)]"
        v-bind="output.on"
        @click="voiceStore.toggleDeafen"
      >
        <HeadphoneOff v-if="voiceStore.isDeafened" class="h-[18px] w-[18px]" />
        <Headphones v-else class="h-[18px] w-[18px]" />
      </button>
      <button
        type="button"
        data-testid="voice-panel-share"
        v-tooltip="voiceStore.isScreenSharing ? $t('voice.stopShare') : $t('voice.share')"
        :aria-pressed="voiceStore.isScreenSharing ? 'true' : 'false'"
        :class="[ib, 'vp-narrow-hide', voiceStore.isScreenSharing ? 'bg-mnema-accent text-mnema-accent-ink' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
        v-bind="share.on"
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
        :class="[ib, 'vp-narrow-hide', voiceStore.isScreenAudioMuted ? 'text-mnema-danger hover:bg-mnema-danger/12' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
        @click="voiceStore.toggleScreenAudioMute"
      >
        <VolumeX v-if="voiceStore.isScreenAudioMuted" class="h-[18px] w-[18px]" />
        <Volume2 v-else class="h-[18px] w-[18px]" />
      </button>
      <button
        type="button"
        data-testid="voice-panel-leave"
        v-tooltip="$t('voice.leave')"
        :class="[ib, 'ml-auto text-mnema-danger hover:bg-mnema-danger/12']"
        @click="leaveVoiceChannel"
      >
        <PhoneOff class="h-[18px] w-[18px]" />
      </button>
    </div>
    <TriggerMenu :trigger="mic" :aria-label="$t('audio.inputDevice')" :min-width="260" />
    <TriggerMenu :trigger="output" :aria-label="$t('audio.outputDevice')" :min-width="260" />
    <TriggerMenu :trigger="share" :aria-label="$t('talk.quality.title')" :min-width="260" />
  </section>
</template>
