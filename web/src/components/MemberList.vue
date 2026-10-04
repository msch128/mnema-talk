<script setup>
import { computed } from 'vue'
import { Crown, Volume2, Shield, Circle } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import UserAvatar from './UserAvatar.vue'

const chatStore = useChatStore()
const voiceStore = useVoiceStore()

// Group members into Herzog/Admin, Online Members, and Offline Members
const admins = computed(() => {
  return chatStore.members.filter(m => m.role === 'admin')
})

const onlineNonAdmins = computed(() => {
  return chatStore.onlineMembers.filter(m => m.role !== 'admin')
})

const offlineNonAdmins = computed(() => {
  return chatStore.offlineMembers.filter(m => m.role !== 'admin')
})

function getUserVoiceChannel(userId) {
  for (const [chId, users] of Object.entries(voiceStore.channelUsers)) {
    if (users && users[userId]) {
      for (const cat of chatStore.categories) {
        const ch = cat.channels?.find(c => c.id === chId)
        if (ch) return ch.name
      }
      const uncat = chatStore.uncategorized?.find(c => c.id === chId)
      if (uncat) return uncat.name
      return 'Voice'
    }
  }
  return null
}
</script>

<template>
  <!-- Discord-Identical Right Edge Glued Member Sidebar -->
  <aside 
    v-if="chatStore.showMemberList" 
    class="w-60 bg-mnema-raised border-l border-mnema-hairline flex flex-col h-full select-none flex-shrink-0 ml-auto transition-all z-10"
  >
    <!-- Top Padding Header Line (matching channel bar height for visual continuity) -->
    <div class="h-14 px-4 border-b border-mnema-hairline flex items-center justify-between flex-shrink-0 bg-mnema-raised/60">
      <span class="text-[11px] font-semibold uppercase tracking-wider text-mnema-tertiary font-mono">
        Mitglieder
      </span>
      <span class="text-[10px] text-mnema-tertiary font-mono">
        {{ chatStore.members.length }}
      </span>
    </div>

    <!-- Scrollable Member Categories -->
    <div class="flex-1 overflow-y-auto px-2.5 py-3 space-y-4">
      <!-- 1. Administrators / Herzog Group -->
      <div v-if="admins.length" class="space-y-1">
        <div class="px-2 text-[10px] font-semibold uppercase tracking-wider text-mnema-amber font-mono flex items-center gap-1.5">
          <Crown class="w-3 h-3 text-mnema-amber" />
          <span>Herzog / Admin — {{ admins.length }}</span>
        </div>

        <div class="space-y-0.5">
          <div
            v-for="member in admins"
            :key="member.id"
            @click="chatStore.openUserProfile(member)"
            class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-mnema-surface/70 transition group cursor-pointer"
          >
            <!-- Avatar with Speaking and Status Indicators -->
            <UserAvatar 
              :user="member" 
              size="md" 
              :show-status="true" 
              :is-online="chatStore.onlineUserIds.has(member.id)" 
              :is-speaking="!!voiceStore.speakingUsers[member.id]" 
            />

            <!-- Name and Activity -->
            <div class="flex flex-col min-w-0 flex-1">
              <div class="flex items-center gap-1 min-w-0">
                <span class="text-xs font-semibold text-mnema-text group-hover:text-mnema-accent transition truncate">
                  {{ member.display_name }}
                </span>
                <Crown class="w-3 h-3 text-mnema-amber flex-shrink-0" />
              </div>

              <!-- Voice Activity Status Badge if in Hangout -->
              <div v-if="getUserVoiceChannel(member.id)" class="flex items-center gap-1 text-[10px] text-mnema-mint font-medium truncate">
                <Volume2 class="w-3 h-3 flex-shrink-0" />
                <span class="truncate">{{ getUserVoiceChannel(member.id) }}</span>
              </div>
              <span v-else class="text-[9px] text-mnema-tertiary font-mono">
                {{ chatStore.onlineUserIds.has(member.id) ? 'Online' : 'Offline' }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- 2. Online Members Group -->
      <div v-if="onlineNonAdmins.length" class="space-y-1">
        <div class="px-2 text-[10px] font-semibold uppercase tracking-wider text-mnema-tertiary font-mono">
          Online — {{ onlineNonAdmins.length }}
        </div>

        <div class="space-y-0.5">
          <div
            v-for="member in onlineNonAdmins"
            :key="member.id"
            @click="chatStore.openUserProfile(member)"
            class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-mnema-surface/70 transition group cursor-pointer"
          >
            <!-- Avatar with Speaking and Status Indicators -->
            <UserAvatar 
              :user="member" 
              size="md" 
              :show-status="true" 
              :is-online="true" 
              :is-speaking="!!voiceStore.speakingUsers[member.id]" 
            />

            <!-- Name and Activity -->
            <div class="flex flex-col min-w-0 flex-1">
              <span class="text-xs font-medium text-mnema-text group-hover:text-mnema-accent transition truncate">
                {{ member.display_name }}
              </span>

              <!-- Voice Activity Status Badge if in Hangout -->
              <div v-if="getUserVoiceChannel(member.id)" class="flex items-center gap-1 text-[10px] text-mnema-mint font-medium truncate">
                <Volume2 class="w-3 h-3 flex-shrink-0" />
                <span class="truncate">{{ getUserVoiceChannel(member.id) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 3. Offline Members Group -->
      <div v-if="offlineNonAdmins.length" class="space-y-1">
        <div class="px-2 text-[10px] font-semibold uppercase tracking-wider text-mnema-tertiary font-mono">
          Offline — {{ offlineNonAdmins.length }}
        </div>

        <div class="space-y-0.5 opacity-60 hover:opacity-100 transition-opacity">
          <div
            v-for="member in offlineNonAdmins"
            :key="member.id"
            @click="chatStore.openUserProfile(member)"
            class="flex items-center gap-2.5 px-2 py-1.5 rounded-lg hover:bg-mnema-surface/70 transition group cursor-pointer"
          >
            <UserAvatar 
              :user="member" 
              size="md" 
              :show-status="true" 
              :is-online="false" 
            />

            <div class="flex flex-col min-w-0 flex-1">
              <span class="text-xs text-mnema-muted group-hover:text-mnema-accent transition truncate">
                {{ member.display_name }}
              </span>
              <span class="text-[9px] text-mnema-tertiary font-mono">Offline</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </aside>
</template>
