<script setup>
import { Mic, MicOff, Headphones, HeadphoneOff, Sliders, MoreHorizontal } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import UserAvatar from './UserAvatar.vue'
import VoiceStatusPanel from './VoiceStatusPanel.vue'
import AccountMenu from './AccountMenu.vue'
import { ref } from 'vue'

const emit = defineEmits(['open-admin', 'open-legal'])

const authStore = useAuthStore()
const voiceStore = useVoiceStore()
const chatStore = useChatStore()

const menuOpen = ref(false)
const menuButton = ref(null)

const ib = 'flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent'
function ibTone(active) {
  return active
    ? 'bg-mnema-danger/[0.12] text-mnema-danger'
    : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
}
</script>

<template>
  <div class="relative flex flex-col bg-mnema-raised">
    <!-- Only while connected: connection status, share, leave -->
    <VoiceStatusPanel v-if="voiceStore.isConnected" />

    <div class="flex h-[60px] items-center gap-0.5 border-t border-mnema-hairline pl-3 pr-1">
      <button
        type="button"
        v-tooltip.visual="$t('user.openProfile')"
        class="mr-1.5 flex min-w-0 flex-1 items-center gap-2.5 rounded-md py-1 text-left transition-colors hover:bg-mnema-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
        @click="chatStore.openUserProfile(authStore.user)"
      >
        <UserAvatar
          :user="authStore.user"
          size="md"
          show-status
          is-online
          :is-speaking="!!voiceStore.speakingUsers[authStore.user?.id]"
        />
        <span class="flex min-w-0 flex-col leading-[18px]">
          <span class="truncate text-sm font-semibold text-mnema-text">{{ authStore.user?.display_name || authStore.user?.username }}</span>
          <span class="truncate text-xs text-mnema-tertiary">{{ $t('user.online') }}</span>
        </span>
      </button>

      <template v-if="voiceStore.isConnected">
        <button
          type="button"
          data-testid="toggle-mute"
          v-tooltip="voiceStore.isMuted ? $t('voice.unmute') : $t('voice.mute')"
          :aria-pressed="voiceStore.isMuted ? 'true' : 'false'"
          :class="[ib, ibTone(voiceStore.isMuted)]"
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
          :class="[ib, ibTone(voiceStore.isDeafened)]"
          @click="voiceStore.toggleDeafen"
        >
          <HeadphoneOff v-if="voiceStore.isDeafened" class="h-[18px] w-[18px]" />
          <Headphones v-else class="h-[18px] w-[18px]" />
        </button>
      </template>

      <button
        type="button"
        v-tooltip="$t('audio.settings')"
        :class="[ib, ibTone(false)]"
        @click="voiceStore.showAudioSettings = true"
      >
        <Sliders class="h-[18px] w-[18px]" />
      </button>

      <button
        ref="menuButton"
        type="button"
        data-testid="account-menu-button"
        v-tooltip="$t('account.menu')"
        aria-haspopup="menu"
        :aria-expanded="menuOpen ? 'true' : 'false'"
        :class="[ib, menuOpen ? 'bg-mnema-hover text-mnema-text' : ibTone(false)]"
        @click="menuOpen = !menuOpen"
      >
        <MoreHorizontal class="h-[18px] w-[18px]" />
      </button>
    </div>

    <AccountMenu
      v-if="menuOpen"
      :trigger="menuButton"
      @close="menuOpen = false"
      @open-admin="emit('open-admin')"
      @open-legal="emit('open-legal')"
    />
  </div>
</template>
