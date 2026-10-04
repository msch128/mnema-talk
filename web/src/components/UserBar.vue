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
      class="px-3 py-2 bg-mnema-band/25 border-b border-mnema-hairline flex items-center justify-between text-xs"
    >
      <div 
        @click="voiceStore.showStatsModal = true"
        class="flex flex-col cursor-pointer group/stat select-none"
        title="Detaillierte Verbindungsmetrik (RTC) anzeigen"
      >
        <div class="flex items-center gap-1.5 text-mnema-mint font-medium text-[11px] group-hover/stat:text-mnema-accent transition">
          <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent shadow-[0_0_4px_rgba(45,167,113,0.8)]"></span>
          <span>Sprachchat aktiv</span>
        </div>
        <div class="flex items-center gap-1 text-[10px] text-mnema-tertiary font-mono">
          <span class="text-mnema-accent font-semibold">{{ voiceStore.ping }}ms</span>
          <span class="group-hover/stat:underline">• RTC Metrik</span>
        </div>
      </div>

      <div class="flex items-center gap-1">
        <!-- 4K 60FPS Screen Share Button -->
        <button
          @click="toggleScreenShare"
          :class="[
            'p-1.5 rounded-md transition text-xs',
            voiceStore.isScreenSharing 
              ? 'bg-mnema-accent text-mnema-accent-ink font-semibold' 
              : 'hover:bg-mnema-hover text-mnema-muted hover:text-mnema-text'
          ]"
          title="Bildschirm übertragen (bis zu 4K 60 FPS)"
        >
          <Monitor class="w-3.5 h-3.5" />
        </button>

        <!-- Disconnect Button -->
        <button
          @click="leaveVoiceChannel"
          class="p-1.5 rounded-md hover:bg-mnema-danger/15 text-mnema-muted hover:text-mnema-danger transition text-xs"
          title="Verbindung trennen"
        >
          <PhoneOff class="w-3.5 h-3.5" />
        </button>
      </div>
    </div>

    <!-- User Identity & Audio Controls -->
    <div class="h-14 px-3.5 flex items-center justify-between">
      <!-- User Info -->
      <div 
        @click="chatStore.openUserProfile(authStore.user)"
        class="flex items-center gap-2.5 min-w-0 cursor-pointer hover:opacity-85 transition group p-1 -m-1 rounded-lg"
        title="Eigenes Profil öffnen / Avatar ändern"
      >
        <UserAvatar 
          :user="authStore.user" 
          size="sm" 
          :is-speaking="!!voiceStore.speakingUsers[authStore.user?.id]" 
        />
        <div class="flex flex-col min-w-0">
          <span class="text-xs font-medium truncate text-mnema-text group-hover:text-mnema-accent transition">
            {{ authStore.user?.display_name || 'Herzog' }}
          </span>
          <span class="text-[10px] text-mnema-tertiary truncate font-mono">
            {{ authStore.isAdmin ? 'Admin' : 'Mitglied' }}
          </span>
        </div>
      </div>

      <!-- Action Toggles -->
      <div class="flex items-center gap-0.5">
        <button
          @click="voiceStore.toggleMute"
          :class="[
            'p-1.5 rounded-md transition',
            voiceStore.isMuted 
              ? 'text-mnema-danger hover:bg-mnema-danger/15' 
              : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
          ]"
          :title="voiceStore.isMuted ? 'Mikrofon stumm' : 'Stummschalten'"
        >
          <MicOff v-if="voiceStore.isMuted" class="w-3.5 h-3.5" />
          <Mic v-else class="w-3.5 h-3.5" />
        </button>

        <button
          @click="voiceStore.toggleDeafen"
          :class="[
            'p-1.5 rounded-md transition',
            voiceStore.isDeafened 
              ? 'text-mnema-danger hover:bg-mnema-danger/15' 
              : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
          ]"
          :title="voiceStore.isDeafened ? 'Audio deaktiviert' : 'Taub stellen'"
        >
          <Headphones class="w-3.5 h-3.5" />
        </button>

        <!-- Audio & Sensitivity Settings -->
        <button
          @click="voiceStore.showAudioSettings = true"
          class="p-1.5 rounded-md text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text transition"
          title="Sprach- & Empfindlichkeitseinstellungen (Discord-Style Noise Gate)"
        >
          <Sliders class="w-3.5 h-3.5" />
        </button>

        <button
          @click="authStore.logout"
          class="p-1.5 rounded-md text-mnema-muted hover:bg-mnema-danger/15 hover:text-mnema-danger transition"
          title="Abmelden"
        >
          <LogOut class="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  </div>
</template>
