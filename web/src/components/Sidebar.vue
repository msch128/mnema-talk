<script setup>
import { computed } from 'vue'
import { Hash, Volume2, ShieldCheck, Crown, Radio, RadioTower } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useAuthStore } from '../stores/auth'
import { useWebRTC } from '../composables/useWebRTC'

const emit = defineEmits(['open-admin'])

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const authStore = useAuthStore()
const { joinVoiceChannel } = useWebRTC()

// Separate channels into Voice Hangouts and Text Channels
const voiceChannels = computed(() => {
  const list = []
  for (const cat of chatStore.categories) {
    for (const ch of cat.channels || []) {
      if (ch.type === 'voice') list.push({ ...ch, categoryName: cat.name })
    }
  }
  for (const ch of chatStore.uncategorized || []) {
    if (ch.type === 'voice') list.push({ ...ch, categoryName: 'Unkategorisiert' })
  }
  return list
})

const textCategories = computed(() => {
  return chatStore.categories.map(cat => ({
    ...cat,
    channels: (cat.channels || []).filter(c => c.type === 'text')
  })).filter(cat => cat.channels.length > 0)
})

const uncategorizedText = computed(() => {
  return (chatStore.uncategorized || []).filter(c => c.type === 'text')
})

function handleVoiceClick(channel) {
  // Select channel messages for side-chat
  chatStore.selectChannel(channel)
  // Join voice and switch to voice stage
  joinVoiceChannel(channel.id)
  voiceStore.activeView = 'voice'
}

function handleTextClick(channel) {
  chatStore.selectChannel(channel)
  voiceStore.activeView = 'chat'
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
          <span class="text-[10px] text-mnema-tertiary font-mono">Private Community</span>
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

    <!-- Navigation Scroll Area -->
    <div class="flex-1 overflow-y-auto px-2.5 py-3 space-y-5">
      <!-- 1. Active Voice Room Jump Button (if connected) -->
      <div v-if="voiceStore.isConnected" class="px-1">
        <button
          @click="voiceStore.activeView = 'voice'"
          :class="[
            'w-full flex items-center justify-between p-2 rounded-lg border transition shadow-sm text-left',
            voiceStore.activeView === 'voice'
              ? 'bg-mnema-accent-subtle border-mnema-accent text-mnema-accent'
              : 'bg-mnema-surface border-mnema-hairline text-mnema-text hover:border-mnema-border-strong'
          ]"
        >
          <div class="flex items-center gap-2 min-w-0">
            <span class="w-2 h-2 rounded-full bg-mnema-accent animate-pulse flex-shrink-0"></span>
            <div class="flex flex-col min-w-0">
              <span class="text-[11px] font-semibold truncate">Talk-Bühne öffnen</span>
              <span class="text-[9px] text-mnema-tertiary font-mono">Aktiver Hangout</span>
            </div>
          </div>
          <Volume2 class="w-4 h-4 text-mnema-accent flex-shrink-0" />
        </button>
      </div>

      <!-- 2. Voice Hangouts Section -->
      <div class="space-y-1.5">
        <div class="px-2 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span class="flex items-center gap-1.5">
            <Radio class="w-3 h-3 text-mnema-accent" />
            <span>Voice Hangouts</span>
          </span>
          <span class="text-[9px] text-mnema-tertiary font-mono">{{ voiceChannels.length }}</span>
        </div>

        <div class="space-y-0.5">
          <div v-for="channel in voiceChannels" :key="channel.id">
            <!-- Voice Channel Row -->
            <button
              @click="handleVoiceClick(channel)"
              :class="[
                'w-full flex items-center justify-between px-2.5 py-2 rounded-md text-xs transition-colors group text-left',
                voiceStore.currentChannelId === channel.id && voiceStore.activeView === 'voice'
                  ? 'bg-mnema-surface text-mnema-accent font-semibold border-l-2 border-mnema-accent pl-2'
                  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
            >
              <div class="flex items-center gap-2 min-w-0">
                <Volume2 
                  :class="[
                    'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                    voiceStore.currentChannelId === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                  ]" 
                />
                <span class="truncate">{{ channel.name }}</span>
              </div>

              <!-- Participant Count Indicator -->
              <span 
                v-if="voiceStore.channelUsers[channel.id] && Object.keys(voiceStore.channelUsers[channel.id]).length"
                class="text-[9px] px-1.5 py-0.2 rounded-full bg-mnema-accent-subtle text-mnema-accent font-mono font-bold"
              >
                {{ Object.keys(voiceStore.channelUsers[channel.id]).length }}
              </span>
            </button>

            <!-- Nested Connected Voice Users -->
            <div 
              v-if="voiceStore.channelUsers[channel.id] && Object.keys(voiceStore.channelUsers[channel.id]).length"
              class="pl-6 py-1 space-y-1"
            >
              <div
                v-for="user in Object.values(voiceStore.channelUsers[channel.id])"
                :key="user.id"
                class="flex items-center gap-2 text-xs py-0.5 px-1.5 rounded-md hover:bg-mnema-hover/60 transition"
              >
                <div 
                  :class="[
                    'w-4 h-4 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center text-[9px] text-mnema-accent font-bold transition-all',
                    voiceStore.speakingUsers[user.id] ? 'ring-2 ring-mnema-accent ring-offset-1 ring-offset-mnema-raised' : ''
                  ]"
                >
                  {{ user.display_name?.charAt(0).toUpperCase() }}
                </div>
                <span class="truncate text-mnema-text text-[11px]">{{ user.display_name }}</span>
                <span v-if="user.role === 'admin'" class="text-[8px] px-1 rounded bg-amber-500/10 text-amber-400 font-mono ml-auto">
                  Admin
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 3. Text Discussions Section -->
      <div class="space-y-3">
        <div class="px-2 flex items-center justify-between text-[10px] font-semibold tracking-wider uppercase text-mnema-tertiary font-mono">
          <span class="flex items-center gap-1.5">
            <Hash class="w-3 h-3 text-mnema-tertiary" />
            <span>Text Kanäle</span>
          </span>
        </div>

        <div v-for="category in textCategories" :key="category.id" class="space-y-1">
          <div class="px-2 text-[9px] font-medium uppercase tracking-wider text-mnema-tertiary font-mono">
            {{ category.name }}
          </div>

          <div class="space-y-0.5">
            <button
              v-for="channel in category.channels"
              :key="channel.id"
              @click="handleTextClick(channel)"
              :class="[
                'w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs transition-colors group text-left',
                chatStore.activeChannel?.id === channel.id && voiceStore.activeView === 'chat'
                  ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
                  : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
              ]"
            >
              <Hash 
                :class="[
                  'w-3.5 h-3.5 flex-shrink-0 transition-colors',
                  chatStore.activeChannel?.id === channel.id ? 'text-mnema-accent' : 'text-mnema-tertiary group-hover:text-mnema-text'
                ]" 
              />
              <span class="truncate">{{ channel.name }}</span>
            </button>
          </div>
        </div>

        <!-- Uncategorized Text -->
        <div v-if="uncategorizedText.length" class="space-y-0.5">
          <button
            v-for="channel in uncategorizedText"
            :key="channel.id"
            @click="handleTextClick(channel)"
            :class="[
              'w-full flex items-center gap-2 px-2.5 py-1.5 rounded-md text-xs transition-colors group text-left',
              chatStore.activeChannel?.id === channel.id && voiceStore.activeView === 'chat'
                ? 'bg-mnema-surface text-mnema-text font-semibold border-l-2 border-mnema-accent pl-2'
                : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
            ]"
          >
            <Hash class="w-3.5 h-3.5 flex-shrink-0 text-mnema-tertiary group-hover:text-mnema-text" />
            <span class="truncate">{{ channel.name }}</span>
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>
