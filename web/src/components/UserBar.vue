<script setup>
import { Mic, MicOff, Headphones, Monitor, PhoneOff, LogOut } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC } from '../composables/useWebRTC'

const authStore = useAuthStore()
const voiceStore = useVoiceStore()
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
      class="px-3.5 py-2 bg-mnema-band/25 border-b border-mnema-hairline flex items-center justify-between text-xs"
    >
      <div class="flex flex-col">
        <div class="flex items-center gap-1.5 text-mnema-mint font-medium text-[11px]">
          <span class="w-1.5 h-1.5 rounded-full bg-mnema-accent animate-pulse"></span>
          <span>Sprachchat aktiv</span>
        </div>
        <span class="text-mnema-tertiary text-[10px] font-mono">{{ voiceStore.ping }}ms</span>
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
      <div class="flex items-center gap-2.5 min-w-0">
        <div 
          :class="[
            'w-7 h-7 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-mnema-accent font-semibold text-xs flex-shrink-0 transition-all',
            voiceStore.speakingUsers[authStore.user?.id] ? 'ring-2 ring-mnema-accent ring-offset-1 ring-offset-mnema-raised' : ''
          ]"
        >
          {{ authStore.user?.display_name?.charAt(0).toUpperCase() || 'H' }}
        </div>
        <div class="flex flex-col min-w-0">
          <span class="text-xs font-medium truncate text-mnema-text">
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
