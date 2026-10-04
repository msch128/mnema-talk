<script setup>
import { ref, watch, onMounted } from 'vue'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import Sidebar from './components/Sidebar.vue'
import UserBar from './components/UserBar.vue'
import ChatArea from './components/ChatArea.vue'
import VoiceStage from './components/VoiceStage.vue'
import MemberList from './components/MemberList.vue'
import ThreadSidebar from './components/ThreadSidebar.vue'
import LoginModal from './components/LoginModal.vue'
import AdminDashboard from './components/AdminDashboard.vue'
import ConnectionStatsModal from './components/ConnectionStatsModal.vue'
import AudioSettingsModal from './components/AudioSettingsModal.vue'
import UserProfileModal from './components/UserProfileModal.vue'

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const showAdminModal = ref(false)

async function initializeApp() {
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers()
  ])
  chatStore.initWebSocket()
}

onMounted(async () => {
  const isAuthed = await authStore.checkAuth()
  if (isAuthed) {
    await initializeApp()
  }
})

// Watch for authentication changes
watch(() => authStore.isAuthenticated, (isAuthed, wasAuthed) => {
  if (isAuthed && !wasAuthed) {
    initializeApp()
  }
})
</script>

<template>
  <div class="h-full flex overflow-hidden bg-mnema-canvas text-mnema-text">
    <!-- Unauthenticated Modal -->
    <LoginModal v-if="!authStore.isAuthenticated" />

    <!-- Main Mnema Talk Layout -->
    <template v-else>
      <!-- Left Column: Navigation, Channels & User Controls -->
      <div class="flex flex-col h-full flex-shrink-0 border-r border-mnema-hairline">
        <Sidebar @open-admin="showAdminModal = true" class="flex-1" />
        <UserBar />
      </div>

      <!-- Center Space: Either Voice Talk Stage or Text Discussion Feed -->
      <VoiceStage 
        v-if="voiceStore.activeView === 'voice' && voiceStore.isConnected" 
      />
      <ChatArea v-else />

      <!-- Rocket.Chat-style Thread Sidebar -->
      <ThreadSidebar v-if="chatStore.activeThread" />

      <!-- Right Column: Discord-Style Member List -->
      <MemberList />

      <!-- Admin Storage & Retention Dashboard Modal -->
      <AdminDashboard v-if="showAdminModal" @close="showAdminModal = false" />

      <!-- Detailed RTC Connection Stats Modal (Discord-Style Debug & Metrics) -->
      <ConnectionStatsModal 
        v-if="voiceStore.showStatsModal" 
        @close="voiceStore.showStatsModal = false" 
      />

      <!-- Discord-Identical Audio & Sensitivity Settings Modal -->
      <AudioSettingsModal 
        v-if="voiceStore.showAudioSettings" 
        @close="voiceStore.showAudioSettings = false" 
      />

      <!-- Discord-Style User Profile Popover / Modal -->
      <UserProfileModal 
        v-if="chatStore.selectedUserProfile"
        :user="chatStore.selectedUserProfile"
        @close="chatStore.closeUserProfile()"
        @mention="(uname) => chatStore.insertMention(uname)"
      />
    </template>
  </div>
</template>
