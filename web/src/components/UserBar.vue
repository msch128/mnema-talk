<script setup>
import { Mic, MicOff, Headphones, Monitor, PhoneOff, Settings, LogOut } from 'lucide-vue-next'
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
  <div class="bg-discord-darkest flex flex-col border-t border-discord-darkest/50">
    <!-- Active Voice Status Bar (Discord Style) -->
    <div 
      v-if="voiceStore.isConnected" 
      class="px-3 py-2 bg-discord-darker/60 border-b border-discord-darkest flex items-center justify-between text-xs"
    >
      <div class="flex flex-col">
        <div class="flex items-center gap-1.5 text-discord-green font-semibold">
          <span class="w-2 h-2 rounded-full bg-discord-green animate-pulse"></span>
          <span>Sprachchat verbunden</span>
        </div>
        <span class="text-discord-muted text-[11px]">Ping: {{ voiceStore.ping }} ms</span>
      </div>

      <div class="flex items-center gap-1">
        <!-- Screen Share Button -->
        <button
          @click="toggleScreenShare"
          :class="[
            'p-1.5 rounded transition',
            voiceStore.isScreenSharing ? 'bg-discord-accent text-white' : 'hover:bg-discord-hover text-discord-muted hover:text-white'
          ]"
          title="Bildschirm übertragen (bis zu 4K 60 FPS)"
        >
          <Monitor class="w-4 h-4" />
        </button>

        <!-- Disconnect Button -->
        <button
          @click="leaveVoiceChannel"
          class="p-1.5 rounded hover:bg-discord-red/20 text-discord-muted hover:text-discord-red transition"
          title="Verbindung trennen"
        >
          <PhoneOff class="w-4 h-4" />
        </button>
      </div>
    </div>

    <!-- User Control Bar -->
    <div class="h-14 px-3 flex items-center justify-between">
      <!-- User Info -->
      <div class="flex items-center gap-2 max-w-[120px] cursor-pointer group">
        <div 
          :class="[
            'w-8 h-8 rounded-full bg-discord-accent flex items-center justify-center text-white font-bold text-sm transition-all',
            voiceStore.speakingUsers[authStore.user?.id] ? 'ring-2 ring-discord-green ring-offset-1 ring-offset-discord-darkest' : ''
          ]"
        >
          {{ authStore.user?.display_name?.charAt(0).toUpperCase() || 'H' }}
        </div>
        <div class="flex flex-col min-w-0">
          <span class="text-xs font-semibold truncate text-white group-hover:underline">
            {{ authStore.user?.display_name || 'Herzog' }}
          </span>
          <span class="text-[10px] text-discord-muted truncate">
            {{ authStore.isAdmin ? '👑 Admin' : 'Mitglied' }}
          </span>
        </div>
      </div>

      <!-- Action Toggles -->
      <div class="flex items-center gap-0.5">
        <button
          @click="voiceStore.toggleMute"
          :class="[
            'p-1.5 rounded transition',
            voiceStore.isMuted ? 'text-discord-red hover:bg-discord-red/10' : 'text-discord-muted hover:bg-discord-hover hover:text-white'
          ]"
          :title="voiceStore.isMuted ? 'Mikrofon stummgeschaltet' : 'Stummschalten'"
        >
          <MicOff v-if="voiceStore.isMuted" class="w-4 h-4" />
          <Mic v-else class="w-4 h-4" />
        </button>

        <button
          @click="voiceStore.toggleDeafen"
          :class="[
            'p-1.5 rounded transition',
            voiceStore.isDeafened ? 'text-discord-red hover:bg-discord-red/10' : 'text-discord-muted hover:bg-discord-hover hover:text-white'
          ]"
          :title="voiceStore.isDeafened ? 'Taub geschaltet' : 'Taub stellen'"
        >
          <Headphones class="w-4 h-4" />
        </button>

        <button
          @click="authStore.logout"
          class="p-1.5 rounded text-discord-muted hover:bg-discord-hover hover:text-discord-red transition"
          title="Abmelden"
        >
          <LogOut class="w-4 h-4" />
        </button>
      </div>
    </div>
  </div>
</template>
