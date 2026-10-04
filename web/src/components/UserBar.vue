<script setup>
import { Mic, MicOff, Headphones, Monitor, PhoneOff, LogOut, Sliders } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useWebRTC } from '../composables/useWebRTC'
import UserAvatar from './UserAvatar.vue'

const authStore = useAuthStore()
const voiceStore = useVoiceStore()
const chatStore = useChatStore()
const { leaveVoiceChannel, startScreenShare, stopScreenShare } = useWebRTC()

// Bottom-row toggles: 32x32 hit areas that overlap by 10px (-mx-[5px]) so the
// four buttons take 88px of row width, leaving room for ~12-char names at the
// default 240px. The visible hover pill is 24px wide so neighbouring icons stay clear.
const toggleBtn = 'group/btn relative w-8 h-8 -mx-[5px] flex items-center justify-center rounded-md focus:outline-none'
function togglePill(tone = 'default', active = false) {
  const base = 'w-6 h-8 flex items-center justify-center rounded-md transition group-focus-visible/btn:ring-2 group-focus-visible/btn:ring-mnema-accent'
  if (active) return [base, 'text-mnema-danger group-hover/btn:bg-mnema-danger/15']
  return [
    base,
    tone === 'danger'
      ? 'text-mnema-muted group-hover/btn:bg-mnema-danger/15 group-hover/btn:text-mnema-danger'
      : 'text-mnema-muted group-hover/btn:bg-mnema-hover group-hover/btn:text-mnema-text'
  ]
}

function toggleScreenShare() {
  if (voiceStore.isScreenSharing) {
    stopScreenShare()
  } else {
    startScreenShare()
  }
}
</script>

<template>
  <div class="bg-mnema-raised flex flex-col border-t border-mnema-hairline">
    <!-- Active Voice Hangout Status Banner -->
    <div 
      v-if="voiceStore.isConnected" 
      class="px-2 py-2 bg-mnema-band/25 border-b border-mnema-hairline flex items-center justify-between gap-2 text-sm"
    >
      <div 
        @click="voiceStore.showStatsModal = true"
        class="flex flex-col min-w-0 pl-1 cursor-pointer group/stat select-none"
        title="Detaillierte Verbindungsmetrik (RTC) anzeigen"
      >
        <div class="flex items-center gap-1.5 text-mnema-mint font-medium text-xs group-hover/stat:text-mnema-accent transition">
          <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent shadow-[0_0_4px_rgba(45,167,113,0.8)]"></span>
          <span class="truncate">Sprachchat aktiv</span>
        </div>
        <div class="flex items-center gap-1 text-xs text-mnema-tertiary font-mono min-w-0 whitespace-nowrap">
          <span class="text-mnema-accent font-semibold">{{ voiceStore.ping ?? '–' }} ms</span>
          <span class="group-hover/stat:underline">• RTC Metrik</span>
        </div>
      </div>

      <div class="flex items-center flex-shrink-0">
        <!-- 4K 60FPS Screen Share Button -->
        <button
          @click="toggleScreenShare"
          :class="[
            'w-8 h-8 flex items-center justify-center rounded-md transition',
            voiceStore.isScreenSharing 
              ? 'bg-mnema-accent text-mnema-accent-ink font-semibold' 
              : 'hover:bg-mnema-hover text-mnema-muted hover:text-mnema-text'
          ]"
          title="Bildschirm übertragen (bis zu 4K 60 FPS)"
        >
          <Monitor class="w-5 h-5" />
        </button>

        <!-- Disconnect Button -->
        <button
          @click="leaveVoiceChannel"
          class="w-8 h-8 flex items-center justify-center rounded-md hover:bg-mnema-danger/15 text-mnema-muted hover:text-mnema-danger transition"
          title="Verbindung trennen"
        >
          <PhoneOff class="w-5 h-5" />
        </button>
      </div>
    </div>

    <!-- User Identity & Audio Controls -->
    <div class="h-[52px] pl-1 pr-2 flex items-center justify-between gap-1.5">
      <!-- User Info -->
      <div
        @click="chatStore.openUserProfile(authStore.user)"
        class="flex items-center gap-1.5 min-w-0 flex-1 cursor-pointer hover:bg-mnema-hover transition-colors group py-1 pl-0.5 pr-0.5 rounded-md"
        title="Eigenes Profil öffnen / Avatar ändern"
      >
        <UserAvatar
          :user="authStore.user"
          size="sm"
          :is-speaking="!!voiceStore.speakingUsers[authStore.user?.id]"
        />
        <div class="flex flex-col min-w-0 leading-tight">
          <span class="text-sm font-semibold truncate text-mnema-text group-hover:text-mnema-accent transition-colors">
            {{ authStore.user?.display_name || 'Herzog' }}
          </span>
          <span class="text-xs text-mnema-tertiary truncate">
            {{ authStore.isAdmin ? 'Admin' : 'Mitglied' }}
          </span>
        </div>
      </div>

      <!-- Action Toggles (32px hit areas, overlapping by 10px) -->
      <div class="flex items-center flex-shrink-0">
        <button
          type="button"
          @click="voiceStore.toggleMute"
          :class="toggleBtn"
          :title="voiceStore.isMuted ? 'Mikrofon stumm' : 'Stummschalten'"
          :aria-pressed="voiceStore.isMuted ? 'true' : 'false'"
        >
          <span :class="togglePill('default', voiceStore.isMuted)">
            <MicOff v-if="voiceStore.isMuted" class="w-[18px] h-[18px]" />
            <Mic v-else class="w-[18px] h-[18px]" />
          </span>
        </button>

        <button
          type="button"
          @click="voiceStore.toggleDeafen"
          :class="toggleBtn"
          :title="voiceStore.isDeafened ? 'Audio deaktiviert' : 'Taub stellen'"
          :aria-pressed="voiceStore.isDeafened ? 'true' : 'false'"
        >
          <span :class="togglePill('default', voiceStore.isDeafened)">
            <Headphones class="w-[18px] h-[18px]" />
          </span>
        </button>

        <!-- Audio & Sensitivity Settings -->
        <button
          type="button"
          @click="voiceStore.showAudioSettings = true"
          :class="toggleBtn"
          title="Sprach- & Empfindlichkeitseinstellungen (Discord-Style Noise Gate)"
        >
          <span :class="togglePill()">
            <Sliders class="w-[18px] h-[18px]" />
          </span>
        </button>

        <button
          type="button"
          @click="authStore.logout"
          :class="toggleBtn"
          title="Abmelden"
        >
          <span :class="togglePill('danger')">
            <LogOut class="w-[18px] h-[18px]" />
          </span>
        </button>
      </div>
    </div>
  </div>
</template>
