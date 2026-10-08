<script setup lang="ts">
import type { User } from '../types/domain'
import { ref, computed } from 'vue'
import { Mic, MicOff, Headphones, HeadphoneOff, MoreHorizontal } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useAppVersionStore } from '../stores/appVersion'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import VoiceStatusPanel from './VoiceStatusPanel.vue'
import AccountMenu from './AccountMenu.vue'
import PresenceMenu from './PresenceMenu.vue'

const emit = defineEmits<{ 'open-admin': []; 'open-legal': [] }>()

const authStore = useAuthStore()
const voiceStore = useVoiceStore()
const chatStore = useChatStore()
// Admins: a dot on the ⋯ button while an update is available.
const versionStore = useAppVersionStore()

const menuOpen = ref(false)
const menuButton = ref<HTMLButtonElement | null>(null)
const presenceOpen = ref(false)
const presenceButton = ref<HTMLButtonElement | null>(null)

const me = computed<Partial<User>>(() => authStore.user || {})
// What others see; before the first snapshot arrives, the own choice.
const myStatus = computed(() => {
  const live = chatStore.presenceOf(me.value.id ?? null)
  return live === 'offline' ? me.value.presence || 'online' : live
})
// Second line: the status text, else the presence.
const subline = computed(() => me.value.status_text || t(`presence.${myStatus.value}`))

function togglePresence() {
  menuOpen.value = false
  presenceOpen.value = !presenceOpen.value
}
function toggleMenu() {
  presenceOpen.value = false
  menuOpen.value = !menuOpen.value
}

function openOwnProfile() {
  if (authStore.user) chatStore.openUserProfile(authStore.user)
}

const ib = 'flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md transition focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent'
function ibTone(active: boolean) {
  return active
    ? 'bg-mnema-danger/[0.12] text-mnema-danger'
    : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
}
</script>

<template>
  <div class="relative flex flex-col bg-mnema-raised">
    <!-- Only while connected: connection status, share, leave -->
    <VoiceStatusPanel v-if="voiceStore.isConnected" />

    <div class="flex h-[56px] items-center gap-1 border-t border-mnema-hairline pl-2 pr-1.5">
      <!-- Avatar: choose the presence -->
      <button
        ref="presenceButton"
        type="button"
        data-testid="presence-button"
        v-tooltip="$t('presence.choose')"
        aria-haspopup="menu"
        :aria-expanded="presenceOpen ? 'true' : 'false'"
        :aria-label="$t('presence.current', { status: $t(`presence.${myStatus}`) })"
        class="flex-shrink-0 rounded-full p-0.5 transition-colors hover:bg-mnema-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
        @click="togglePresence"
      >
        <UserAvatar
          :user="me"
          size="sm"
          show-status
          :status="myStatus"
          ring-class="bg-mnema-raised"
          :is-speaking="voiceStore.isSpeaking(me.id ?? null)"
        />
      </button>

      <!-- Name: own profile -->
      <button
        type="button"
        data-testid="own-profile-button"
        v-tooltip.visual="$t('user.openProfile')"
        class="flex min-w-0 flex-1 flex-col rounded-md px-1.5 py-1 text-left leading-[18px] transition-colors hover:bg-mnema-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-mnema-accent"
        @click="openOwnProfile"
      >
        <span class="truncate text-sm font-semibold text-mnema-text">{{ me.display_name || me.username }}</span>
        <span data-testid="own-subline" class="truncate text-xs text-mnema-tertiary">{{ subline }}</span>
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
        ref="menuButton"
        type="button"
        data-testid="account-menu-button"
        v-tooltip="$t('account.menu')"
        aria-haspopup="menu"
        :aria-expanded="menuOpen ? 'true' : 'false'"
        :class="[ib, 'relative', menuOpen ? 'bg-mnema-hover text-mnema-text' : ibTone(false)]"
        @click="toggleMenu"
      >
        <MoreHorizontal class="h-[18px] w-[18px]" />
        <span
          v-if="authStore.isAdmin && versionStore.adminUpdateAvailable"
          data-testid="admin-update-dot"
          class="absolute right-1 top-1 h-2 w-2 rounded-full bg-mnema-accent ring-2 ring-mnema-raised"
          aria-hidden="true"
        ></span>
      </button>
    </div>

    <PresenceMenu v-if="presenceOpen" :trigger="presenceButton" @close="presenceOpen = false" />
    <AccountMenu
      v-if="menuOpen"
      :trigger="menuButton"
      @close="menuOpen = false"
      @open-admin="emit('open-admin')"
      @open-legal="emit('open-legal')"
    />
  </div>
</template>
