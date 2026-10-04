<script setup>
import { ref, onMounted } from 'vue'
import { useAuthStore } from './stores/auth'
import { useChatStore } from './stores/chat'
import Sidebar from './components/Sidebar.vue'
import UserBar from './components/UserBar.vue'
import ChatArea from './components/ChatArea.vue'
import LoginModal from './components/LoginModal.vue'
import AdminDashboard from './components/AdminDashboard.vue'

const authStore = useAuthStore()
const chatStore = useChatStore()
const showAdminModal = ref(false)

onMounted(async () => {
  const isAuthed = await authStore.checkAuth()
  if (isAuthed) {
    chatStore.fetchChannels()
    chatStore.initWebSocket()
  }
})

// Watch for authentication changes
authStore.$subscribe((mutation, state) => {
  if (state.token && state.user) {
    chatStore.fetchChannels()
    chatStore.initWebSocket()
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

      <!-- Center Column: Conversation & Media Timeline -->
      <ChatArea />

      <!-- Admin Storage & Retention Dashboard Modal -->
      <AdminDashboard v-if="showAdminModal" @close="showAdminModal = false" />
    </template>
  </div>
</template>
