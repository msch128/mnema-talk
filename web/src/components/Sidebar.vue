<script setup>
import { computed } from 'vue'
import { Hash, Volume2, Plus, ChevronDown, ChevronRight, Crown } from 'lucide-vue-next'
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
    // Discord behavior: Clicking voice connects instantly!
    joinVoiceChannel(channel.id)
  }
}
</script>

<template>
  <aside class="w-60 bg-discord-darker flex flex-col h-full select-none border-r border-discord-darkest/50">
    <!-- Server Header -->
    <header class="h-12 px-4 border-b border-discord-darkest flex items-center justify-between font-semibold shadow-sm hover:bg-discord-hover/50 cursor-pointer transition">
      <div class="flex items-center gap-2">
        <span class="truncate text-white">Mnema Talk</span>
        <Crown v-if="authStore.isAdmin" class="w-4 h-4 text-yellow-500 flex-shrink-0" />
      </div>
      <button 
        v-if="authStore.isAdmin" 
        @click.stop="emit('open-admin')"
        title="Admin Dashboard"
        class="text-xs bg-discord-accent hover:bg-discord-accent/80 text-white px-2 py-0.5 rounded font-medium transition"
      >
        Admin
      </button>
    </header>

    <!-- Channels Scroll Area -->
    <div class="flex-1 overflow-y-auto px-2 py-3 space-y-4">
      <!-- Categories with Channels -->
      <div v-for="category in chatStore.categories" :key="category.id" class="space-y-0.5">
        <div class="flex items-center justify-between px-1 text-xs font-bold text-discord-muted uppercase tracking-wider">
          <span>{{ category.name }}</span>
        </div>

        <div class="space-y-0.5 mt-1">
          <div v-for="channel in category.channels" :key="channel.id">
            <!-- Channel Item -->
            <button
              @click="handleChannelClick(channel)"
              :class="[
                'w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition group',
                chatStore.activeChannel?.id === channel.id
                  ? 'bg-discord-light text-white font-medium'
                  : 'text-discord-muted hover:bg-discord-hover hover:text-discord-text'
              ]"
            >
              <Hash v-if="channel.type === 'text'" class="w-4 h-4 flex-shrink-0 text-discord-muted group-hover:text-white" />
              <Volume2 v-else class="w-4 h-4 flex-shrink-0 text-discord-muted group-hover:text-white" />
              <span class="truncate">{{ channel.name }}</span>
            </button>

            <!-- Nested Users inside Voice Channel (Discord Hangout) -->
            <div 
              v-if="channel.type === 'voice' && voiceStore.channelUsers[channel.id]"
              class="pl-6 py-1 space-y-1"
            >
              <div
                v-for="user in Object.values(voiceStore.channelUsers[channel.id])"
                :key="user.id"
                class="flex items-center gap-2 text-xs py-0.5 px-1.5 rounded hover:bg-discord-hover/40"
              >
                <!-- Avatar with Speaking Indicator (Green Ring) -->
                <div 
                  :class="[
                    'w-5 h-5 rounded-full bg-discord-accent flex items-center justify-center text-[10px] text-white font-bold transition-all',
                    voiceStore.speakingUsers[user.id] ? 'ring-2 ring-discord-green ring-offset-1 ring-offset-discord-darker' : ''
                  ]"
                >
                  {{ user.display_name?.charAt(0).toUpperCase() }}
                </div>
                <span class="truncate text-discord-text">{{ user.display_name }}</span>
                <span v-if="user.role === 'admin'" class="text-[10px] text-yellow-500 font-bold ml-auto">👑</span>
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
            'w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition group',
            chatStore.activeChannel?.id === channel.id
              ? 'bg-discord-light text-white font-medium'
              : 'text-discord-muted hover:bg-discord-hover hover:text-discord-text'
          ]"
        >
          <Hash v-if="channel.type === 'text'" class="w-4 h-4 flex-shrink-0" />
          <Volume2 v-else class="w-4 h-4 flex-shrink-0" />
          <span class="truncate">{{ channel.name }}</span>
        </button>
      </div>
    </div>
  </aside>
</template>
