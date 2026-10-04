<script setup>
import { computed } from 'vue'
import { Hash, Volume2, ShieldCheck, Crown } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'

const emit = defineEmits(['open-admin'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const { joinVoiceChannel } = useWebRTC()

function handleChannelClick(channel) {
  if (channel.type === 'text') {
    chatStore.selectChannel(channel)
  } else if (channel.type === 'voice') {
    // Instant voice connect
    joinVoiceChannel(channel.id)
  }
}
</script>

<template>
  <aside class="w-64 bg-mnema-raised flex flex-col h-full select-none">
    <!-- Server / Workspace Header -->
    <header class="h-14 px-4 border-b border-mnema-hairline flex items-center justify-between">
      <div class="flex items-center gap-2.5 min-w-0">
        <!-- Mnema Forest Mark -->
        <div class="w-7 h-7 rounded-md bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-xs shadow-sm flex-shrink-0">
          M
        </div>
        <div class="flex flex-col min-w-0">
          <div class="flex items-center gap-1.5">
            <span class="font-semibold text-xs tracking-tight text-mnema-text truncate">Mnema Talk</span>
            <Crown v-if="authStore.isAdmin" class="w-3.5 h-3.5 text-mnema-amber flex-shrink-0" />
          </div>
          <span class="text-[10px] text-mnema-tertiary font-mono">Private Server</span>
        </div>
      </div>

      <!-- Admin Dashboard Button -->
      <button 
        v-if="authStore.isAdmin" 
        @click.stop="emit('open-admin')"
        title="Admin Konsole & S3 Speicher"
        class="text-[11px] font-medium px-2 py-0.5 rounded border border-mnema-accent/40 bg-mnema-accent-subtle text-mnema-accent hover:bg-mnema-accent hover:text-mnema-accent-ink transition"
      >
        Admin
      </button>
    </header>

    <!-- Channels Navigation List -->
    <div class="flex-1 overflow-y-auto px-2.5 py-3 space-y-4">
      <!-- Categorized Channels -->
      <div v-for="category in chatStore.categories" :key="category.id" class="space-y-1">
        <!-- Category Title (Nano Upper Rule) -->
        <div class="px-2 pt-1 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span>{{ category.name }}</span>
        </div>

        <div class="space-y-0.5 mt-1">
          <div v-for="channel in category.channels" :key="channel.id">
            <!-- Channel Row -->
            <button
              @click="handleChannelClick(channel)"
              :class="[
                'w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs transition-colors group text-left',
                chatStore.activeChannel?.id === channel.id
                  ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
                  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
            >
              <Hash 
                v-if="channel.type === 'text'" 
                :class="[
                  'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                  chatStore.activeChannel?.id === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                ]" 
              />
              <Volume2 
                v-else 
                :class="[
                  'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                  voiceStore.connectedChannelId === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                ]" 
              />
              <span class="truncate">{{ channel.name }}</span>
            </button>

            <!-- Nested Connected Voice Users -->
            <div 
              v-if="channel.type === 'voice' && voiceStore.channelUsers[channel.id]"
              class="pl-6 py-1 space-y-1"
            >
              <div
                v-for="user in Object.values(voiceStore.channelUsers[channel.id])"
                :key="user.id"
                class="flex items-center gap-2 text-xs py-1 px-2 rounded-md hover:bg-mnema-hover/60 transition"
              >
                <!-- Avatar with Real-time Mnema Emerald Speaking Ring -->
                <div 
                  :class="[
                    'w-5 h-5 rounded-full bg-mnema-accent-subtle border border-mnema-border flex items-center justify-center text-[10px] text-mnema-accent font-bold transition-all',
                    voiceStore.speakingUsers[user.id] ? 'ring-2 ring-mnema-accent ring-offset-1 ring-offset-mnema-raised' : ''
                  ]"
                >
                  {{ user.display_name?.charAt(0).toUpperCase() }}
                </div>
                <span class="truncate text-mnema-text text-xs">{{ user.display_name }}</span>
                <span v-if="user.role === 'admin'" class="text-[9px] px-1 py-0.2 rounded bg-amber-500/10 text-amber-400 font-mono ml-auto">
                  Admin
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Uncategorized Channels -->
      <div v-if="chatStore.uncategorized.length" class="space-y-0.5">
        <button
          v-for="channel in chatStore.uncategorized"
          :key="channel.id"
          @click="handleChannelClick(channel)"
          :class="[
            'w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs transition-colors group text-left',
            chatStore.activeChannel?.id === channel.id
              ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
              : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
          ]"
        >
          <Hash v-if="channel.type === 'text'" class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
          <Volume2 v-else class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
          <span class="truncate">{{ channel.name }}</span>
        </button>
      </div>
    </div>
  </aside>
</template>
