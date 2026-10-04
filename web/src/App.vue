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
import LegalModal from './components/LegalModal.vue'
import ResizeHandle from './components/ResizeHandle.vue'
import { useResizable } from './composables/useResizable'
import { useWebRTC } from './composables/useWebRTC'

const authStore = useAuthStore()
const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const { resumeVoiceSession, resumeRemoteAudio } = useWebRTC()

// Discord-like column widths. Order = shrink priority on narrow windows
// (thread first, then member list, then the left sidebar).
const panels = useResizable([
  { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
  { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360, visible: () => chatStore.showMemberList },
  { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => !!chatStore.activeThread }
], { centerMin: 400 })
const showAdminModal = ref(false)
const showLegalModal = ref(false)
// Avoids flashing the login dialog while the session cookie is being checked.
const authChecked = ref(false)
// One voice-resume attempt per login (see the isConnected watcher below).
let resumeAttempted = false

async function initializeApp() {
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers()
  ])
  chatStore.initWebSocket()
}

// Single place that reacts to login, logout and expired sessions.
watch(() => authStore.isAuthenticated, isAuthed => {
  if (isAuthed) {
    resumeAttempted = false
    initializeApp()
  } else {
    chatStore.closeWebSocket()
  }
})

// Rejoin the voice channel after a page reload (within the 30 s window), once
// the WebSocket is up and the channel list is known. Only once per login, so a
// later reconnect doesn't pull the user back into a call they left.
watch(() => chatStore.isConnected, connected => {
  if (!connected || resumeAttempted) return
  resumeAttempted = true
  resumeVoiceSession().catch(() => {})
})

onMounted(async () => {
  await authStore.checkAuth()
  authChecked.value = true
})
</script>

<template>
  <div class="h-full flex overflow-hidden bg-mnema-canvas text-mnema-text">
    <!-- Session check still running: minimal loading state, no data fetching -->
    <div
      v-if="!authChecked"
      class="flex-1 flex flex-col items-center justify-center gap-3"
      role="status"
      aria-live="polite"
    >
      <div class="w-10 h-10 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-base animate-pulse">
        M
      </div>
      <span class="text-sm text-mnema-tertiary">Verbindung wird geprüft…</span>
    </div>

    <!-- Unauthenticated: login / invite registration -->
    <LoginModal v-else-if="!authStore.isAuthenticated" />

    <!-- Main Mnema Talk Layout -->
    <template v-else>
      <!-- Left Column: Navigation, Channels & User Controls -->
      <div
        class="relative flex flex-col h-full flex-shrink-0 border-r border-mnema-hairline bg-mnema-raised"
        :style="{ width: `${panels.left.width}px` }"
      >
        <Sidebar @open-admin="showAdminModal = true" @open-legal="showLegalModal = true" class="flex-1 min-h-0" />
        <UserBar />
        <ResizeHandle :panel="panels.left" label="Breite der Kanalliste anpassen" />
      </div>

      <!-- Center Space: Either Voice Talk Stage or Text Discussion Feed -->
      <VoiceStage
        v-if="voiceStore.activeView === 'voice' && voiceStore.isConnected"
      />
      <ChatArea v-else />

      <!-- Rocket.Chat-style Thread Sidebar -->
      <div
        v-if="chatStore.activeThread"
        class="relative h-full flex-shrink-0"
        :style="{ width: `${panels.thread.width}px` }"
      >
        <ThreadSidebar />
        <ResizeHandle :panel="panels.thread" label="Breite des Threads anpassen" />
      </div>

      <!-- Right Column: Discord-Style Member List -->
      <div
        v-if="chatStore.showMemberList"
        class="relative h-full flex-shrink-0"
        :style="{ width: `${panels.members.width}px` }"
      >
        <MemberList />
        <ResizeHandle :panel="panels.members" label="Breite der Mitgliederliste anpassen" />
      </div>

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

      <!-- Legal & Privacy Policy Modal (DSGVO) -->
      <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />

      <!-- Browser blocked call audio after a reload: one click unblocks it -->
      <button
        v-if="voiceStore.audioBlocked && voiceStore.isConnected"
        type="button"
        class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2 rounded-md bg-mnema-mint text-mnema-canvas text-sm font-semibold shadow-lg hover:brightness-110"
        @click="resumeRemoteAudio"
      >
        Du bist wieder im Sprachkanal. Klicken, um Ton zu aktivieren
      </button>
    </template>
  </div>
</template>
