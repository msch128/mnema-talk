<script setup lang="ts">
import { startDesktopGaming } from './talk/lib/desktopGaming'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useAuthStore } from './stores/auth'
import { usePermissionsStore } from './stores/permissions'
import { useChatStore } from './stores/chat'
import { useVoiceStore } from './stores/voice'
import { useAccountStore } from './stores/account'
import { useBlocksStore } from './stores/blocks'
import { useServerFeaturesStore } from './stores/serverFeatures'
import { recoveryFromLocation } from './app/lib/recoveryLink'
import { currentRoute, navigate, popRedirectRoute, savePendingRoute } from './shared/lib/router'
import Sidebar from './app/components/Sidebar.vue'
import UserBar from './app/components/UserBar.vue'
import ChatArea from './chat/components/ChatArea.vue'
import VoiceStage from './talk/components/VoiceStage.vue'
import VoiceChatPanel from './app/components/VoiceChatPanel.vue'
import ForumArea from './forum/components/ForumArea.vue'
import MemberList from './app/components/MemberList.vue'
import ThreadSidebar from './chat/components/ThreadSidebar.vue'
import InviteLanding from './app/components/InviteLanding.vue'
import RecoveryPage from './app/components/RecoveryPage.vue'
import { rulesOpen } from './app/lib/communityRules'
import SettingsDialog from './settings/components/SettingsDialog.vue'
import CommunityRulesDialog from './app/components/CommunityRulesDialog.vue'
import AdminDashboard from './admin/components/AdminDashboard.vue'
import ConnectionStatsModal from './talk/components/ConnectionStatsModal.vue'
import { openSettings } from './shared/lib/settingsDialog'
import { notifyDialogOpen } from './shared/lib/notifyDialog'
import EventsModal from './events/components/EventsModal.vue'
import { closeEvents, eventsRequest } from './events/lib/eventsDialog'
import NotificationSettingsDialog from './app/components/NotificationSettingsDialog.vue'
import ShortcutsDialog from './app/components/ShortcutsDialog.vue'
import { shortcutsOpen, useShortcutsKey } from './shared/lib/shortcutsDialog'
import ScreenShareModal from './talk/components/ScreenShareModal.vue'
import UserProfileModal from './app/components/UserProfileModal.vue'
import StatusDialog from './app/components/StatusDialog.vue'
import { useStatusEditor } from './app/composables/useStatusEditor'
import LegalModal from './shared/components/LegalModal.vue'
import ResizeHandle from './shared/components/ResizeHandle.vue'
import ToastHost from './shared/components/ToastHost.vue'
import CallHost from './dm/components/CallHost.vue'
import ConfirmDialog from './shared/components/ConfirmDialog.vue'
import MediaViewer from './shared/components/MediaViewer.vue'
import WelcomeFlow from './app/components/WelcomeFlow.vue'
import ConnectionBanner from './app/components/ConnectionBanner.vue'
import StorageBanner from './app/components/StorageBanner.vue'
import SkipLinks from './app/components/SkipLinks.vue'
import TimeoutBanner from './app/components/TimeoutBanner.vue'
import UpdateBanner from './app/components/UpdateBanner.vue'
import WikiDialog from './wiki/components/WikiDialog.vue'
import { ModerationDialog, MessageModerationDialog, EditHistoryDialog, ReportDialog, ForwardDialog, LaterDialog, TalkSettingsHost } from './app/components/overlayHosts'
import PipHost from './talk/components/PipHost.vue'
import { useResizable } from './shared/composables/useResizable'
import { useWebRTC } from './talk/composables/useWebRTC'
import { useAppRouting } from './app/composables/useAppRouting'
import { useAppBadge } from './app/composables/useAppBadge'
import { useCompactLayout } from './shared/composables/useCompactLayout'
import { usePushBridge } from './app/composables/usePushBridge'
import { usePttHint } from './talk/composables/usePttHint'
import { useTitleBadge } from './app/composables/useTitleBadge'
import { useSessionCheck } from './app/composables/useSessionCheck'
import ServerUnreachable from './app/components/ServerUnreachable.vue'
import { VOICE_CHAT_HEIGHT, STAGE_MIN_HEIGHT } from './talk/lib/voiceChatPanel'

const authStore = useAuthStore()
const permissions = usePermissionsStore()
const chatStore = useChatStore()
useTitleBadge()
const voiceStore = useVoiceStore()
const accountStore = useAccountStore()
const statusEditor = useStatusEditor()
const serverFeatures = useServerFeaturesStore()
// An account e-mail link (confirm address, reset password) takes over the page; its token is read once, then removed from the URL.
const recovery = recoveryFromLocation(window.location.pathname, window.location.hash)
const leaveRecovery = () => window.location.assign('/')
let alive = true
onBeforeUnmount(() => {
  alive = false
  routing.dispose()
  sessionCheck.dispose()
})
const { resumeVoiceSession, resumeRemoteAudio, leaveVoiceChannel, rejoinAfterReconnect } = useWebRTC()

// Column widths. Order = shrink priority on narrow windows
// (thread first, then member list, then the left sidebar).
const panels = useResizable([
  { name: 'left', side: 'left', defaultWidth: 240, min: 200, max: 360 },
  { name: 'members', side: 'right', defaultWidth: 240, min: 200, max: 360, visible: () => chatStore.showMemberList },
  { name: 'thread', side: 'right', defaultWidth: 400, min: 320, max: 640, visible: () => !!chatStore.activeThread }
], { centerMin: 400 })
// Height of the Talk's chat under the stage; the stage (and its 48px header)
// keeps the rest, at least STAGE_MIN_HEIGHT.
const voiceChatHeight = useResizable([VOICE_CHAT_HEIGHT], { axis: 'y', centerMin: STAGE_MIN_HEIGHT + 48 }).voiceChat
const showLegalModal = ref(false)
// A direct message conversation fills the chat area: no community member list beside it.
const inConversation = computed(() => chatStore.isDm(chatStore.activeChannel?.id))
// Phones: the channel list is a drawer, the thread and member list fill the screen.
const { isCompact, navOpen, closeNav } = useCompactLayout()
if (isCompact.value) chatStore.showMemberList = false
watch(() => [chatStore.activeChannel?.id, voiceStore.currentChannelId, voiceStore.activeView], closeNav)
// The Talk bar asks for the audio settings with this flag; they live in the settings dialog.
watch(() => voiceStore.showAudioSettings, open => { if (open && !accountStore.dialogOpen) openSettings('audio') })
watch(() => chatStore.activeThread, thread => { if (thread && isCompact.value) chatStore.showMemberList = false })
// A Talk needs the width: below 1100 px the member list starts closed (the header button opens it).
watch(() => voiceStore.activeView, view => { if (view === 'voice' && window.innerWidth < 1100) chatStore.showMemberList = false })
// Captured: runs before the panels' Escape handlers; preventDefault tells them the key is used.
function onWindowKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !navOpen.value) return
  event.preventDefault()
  closeNav()
}
onMounted(() => window.addEventListener('keydown', onWindowKeydown, true))
onBeforeUnmount(() => window.removeEventListener('keydown', onWindowKeydown, true))
const routing = useAppRouting(() => alive)
useAppBadge()
usePushBridge()
usePttHint()
useShortcutsKey(() => !!authStore.user)
const { showAdminModal, adminTab, voiceShowChat, voiceChannelId, applyRoute, syncCurrentStateToRoute, openAdmin, closeAdmin, onVoiceJoin } = routing

// Avoids flashing the login dialog while the session cookie is being checked.
const authChecked = ref(false)
// One voice-resume attempt per login (see the isConnected watcher below).
let resumeAttempted = false

async function initializeApp() {
  if (!alive) return
  routing.beginFirstRoute()
  // What the server can do decides what is fetched next and how the socket opens.
  await useServerFeaturesStore().load()
  if (!alive) return
  await Promise.all([
    chatStore.fetchChannels(),
    chatStore.fetchMembers(),
    chatStore.fetchReadState(),
    chatStore.fetchNotificationSettings(),
    useBlocksStore().load()
  ])
  if (!alive) return
  chatStore.dmsLoaded = false
  chatStore.initWebSocket()
  // The conversations are read as the socket opens; a first route into one needs them.
  await chatStore.waitForDms(3000)
}

async function onAuthSuccess() {
  if (!alive) return
  resumeAttempted = false
  await initializeApp()
  if (!alive) return
  const redirect = popRedirectRoute()
  if (redirect) {
    routing.endFirstRoute()
    navigate(redirect, { replace: true })
  } else {
    const rewritten = await applyRoute(currentRoute.value).finally(() => routing.endFirstRoute())
    if (!alive) return
    // A rewritten address is applied on its own; the screen shows a stand-in until then.
    if (!rewritten) syncCurrentStateToRoute()
  }
}

/** Ends the call and drops everything the previous account had loaded, so nothing leaks into the next sign-in. */
function endAccountSession() {
  savePendingRoute()
  if (voiceStore.currentChannelId) leaveVoiceChannel()
  chatStore.resetCommunityState()
}

// Single place that reacts to login, logout and expired sessions.
watch(() => authStore.isAuthenticated, async (isAuthed) => {
  if (!alive) return
  if (!isAuthed) endAccountSession() // logout or expired session
  else if (authChecked.value) await onAuthSuccess()
})

// Another tab may sign a different account in under the same cookie: the user swaps without a "signed out" step.
watch(() => authStore.user?.id, async (id, previous) => {
  if (!alive || !id || !previous || !authChecked.value) return
  endAccountSession()
  await onAuthSuccess()
})

// The server may have dropped our media connection while the socket was down.
watch(() => chatStore.reconnectCount, () => rejoinAfterReconnect())

// Rejoin the voice channel after a page reload (within the 30 s window), once
// the WebSocket is up and the channel list is known. Only once per login, so a
// later reconnect doesn't pull the user back into a call they left.
watch(() => chatStore.isConnected, connected => {
  if (!connected || resumeAttempted) return
  resumeAttempted = true
  resumeVoiceSession().catch(() => {})
})

// Session check at start-up (see useSessionCheck): an unanswered check is not
// "signed out", so the unreachable screen stays and keeps trying.
const sessionCheck = useSessionCheck(() => authStore.checkAuth())

onBeforeUnmount(startDesktopGaming())
onMounted(async () => {
  const isAuthed = await sessionCheck.run()
  if (!alive) return
  authChecked.value = true
  if (isAuthed) {
    await onAuthSuccess()
  } else {
    savePendingRoute()
  }
})
</script>

<template>
  <div class="h-full flex overflow-hidden bg-mnema-canvas text-mnema-text">
    <RecoveryPage v-if="recovery" :link="recovery" @done="leaveRecovery" />

    <!-- Session check still running: minimal loading state, no data fetching -->
    <ServerUnreachable
      v-else-if="!authChecked && sessionCheck.retrying.value"
      :trying="sessionCheck.trying.value"
      :last-tried-at="sessionCheck.lastTriedAt.value"
      @retry="sessionCheck.retryNow"
    />
    <div
      v-else-if="!authChecked"
      class="flex-1 flex flex-col items-center justify-center gap-3"
      role="status"
      aria-live="polite"
    >
      <div class="w-10 h-10 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint font-semibold text-base animate-pulse">
        M
      </div>
      <span class="text-sm text-mnema-tertiary">{{ $t('app.checking') }}</span>
    </div>

    <!-- Unauthenticated: login / invite registration -->
    <InviteLanding v-else-if="!authStore.isAuthenticated" />

    <!-- Main Mnema Talk Layout -->
    <template v-else>
      <SkipLinks />
      <!-- Left Column: Navigation, Channels & User Controls -->
      <div
        v-if="isCompact && navOpen"
        data-testid="nav-scrim"
        class="fixed inset-0 z-30 bg-black/60"
        aria-hidden="true"
        @click="closeNav"
      ></div>
      <div
        data-testid="channel-drawer"
        :class="[
          'relative flex flex-col h-full shrink-0 border-r border-mnema-hairline bg-mnema-raised',
          'max-md:safe-pad max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-40 max-md:w-[min(86vw,22rem)] max-md:transition-transform max-md:duration-200 motion-reduce:transition-none',
          navOpen ? '' : 'max-md:-translate-x-full'
        ]"
        :inert="isCompact && !navOpen"
        :style="isCompact ? undefined : { width: `${panels.left.width}px` }"
      >
        <Sidebar @open-admin="openAdmin('users')" @open-legal="showLegalModal = true" class="flex-1 min-h-0" />
        <UserBar @open-legal="showLegalModal = true" />
        <ResizeHandle v-if="!isCompact" :panel="panels.left" :label="$t('resize.sidebar')" />
      </div>

      <!-- Center: connection banner, then the Talk or a text channel -->
      <div class="flex-1 min-w-0 h-full flex flex-col">
        <ConnectionBanner />
        <StorageBanner />
        <TimeoutBanner />
        <UpdateBanner />
        <template v-if="voiceStore.activeView === 'voice'">
          <VoiceStage
            :channel-id="voiceChannelId"
            v-model:show-chat="voiceShowChat"
            @join="onVoiceJoin"
            class="min-h-0"
          />
          <!-- The Talk's own text chat, under the stage -->
          <VoiceChatPanel
            v-if="voiceShowChat && voiceChannelId"
            :channel-id="voiceChannelId"
            :panel="voiceChatHeight"
            @close="voiceShowChat = false"
          />
        </template>
        <div v-else class="min-h-0 flex flex-1 flex-col">
          <ForumArea v-if="chatStore.activeChannel?.type === 'forum'" class="min-h-0 flex-1" />
          <ChatArea v-else class="min-h-0 flex-1" />
        </div>
      </div>

      <!-- Thread panel -->
      <div
        v-if="chatStore.activeThread"
        class="relative h-full shrink-0 max-md:safe-pad max-md:fixed max-md:inset-0 max-md:z-30 max-md:bg-mnema-canvas"
        :style="isCompact ? undefined : { width: `${panels.thread.width}px` }"
      >
        <ThreadSidebar />
        <ResizeHandle v-if="!isCompact" :panel="panels.thread" :label="$t('resize.thread')" />
      </div>

      <!-- Right column: member list -->
      <div
        v-if="chatStore.showMemberList && !inConversation"
        class="relative h-full shrink-0 max-md:safe-pad max-md:fixed max-md:inset-0 max-md:z-30 max-md:bg-mnema-canvas"
        :style="isCompact ? undefined : { width: `${panels.members.width}px` }"
      >
        <MemberList />
        <ResizeHandle v-if="!isCompact" :panel="panels.members" :label="$t('resize.members')" />
      </div>

      <!-- Admin console dialog -->
      <AdminDashboard v-if="showAdminModal && permissions.canOpenAdmin" :initial-tab="adminTab" @close="closeAdmin" />

      <!-- Connection statistics dialog -->
      <ConnectionStatsModal 
        v-if="voiceStore.showStatsModal" 
        @close="voiceStore.showStatsModal = false" 
      />

      <!-- Screen share quality dialog -->
      <ScreenShareModal
        v-if="voiceStore.showScreenShareModal"
        @close="voiceStore.closeScreenShareModal()"
      />

      <!-- User profile dialog -->
      <UserProfileModal 
        v-if="chatStore.selectedUserProfile"
        :user="chatStore.selectedUserProfile"
        @close="chatStore.closeUserProfile()"
        @mention="(uname) => chatStore.insertMention(uname)"
      />

      <StatusDialog v-if="statusEditor.open.value" @close="statusEditor.hide()" />
      <!-- Settings: account, privacy, push and app, audio -->
      <SettingsDialog v-if="accountStore.dialogOpen" />
      <EventsModal v-if="eventsRequest && authStore.isAuthenticated && serverFeatures.events" :focus-event-id="eventsRequest.eventId" :focus-seq="eventsRequest.seq" @close="closeEvents" />
      <WikiDialog v-if="currentRoute.view === 'wiki' && serverFeatures.wiki" />
      <NotificationSettingsDialog v-if="notifyDialogOpen" @close="notifyDialogOpen = false" />
      <ShortcutsDialog v-if="shortcutsOpen" @close="shortcutsOpen = false" />

      <!-- First run and re-acceptance of changed rules -->
      <WelcomeFlow />

      <!-- The community rules, read again from the server menu -->
      <CommunityRulesDialog v-if="rulesOpen" />

      <!-- Legal & privacy dialog -->
      <LegalModal v-if="showLegalModal" @close="showLegalModal = false" />

      <!-- The video of the Picture-in-Picture window (outlives the Talk view) -->
      <PipHost />

      <!-- Browser blocked call audio after a reload: one click unblocks it -->
      <button
        v-if="voiceStore.audioBlocked && voiceStore.isConnected"
        type="button"
        class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 px-4 py-2 rounded-md bg-mnema-mint text-mnema-canvas text-sm font-semibold shadow-lg hover:brightness-110"
        @click="resumeRemoteAudio"
      >
        {{ $t('voice.audioBlocked') }}
      </button>
    </template>

    <ToastHost />
    <!-- Before the confirm host: its "Discard changes?" must open above the Talk settings. -->
    <TalkSettingsHost />
    <CallHost />
    <ConfirmDialog />
    <MediaViewer />
    <ModerationDialog />
    <MessageModerationDialog />
    <EditHistoryDialog />
    <ReportDialog />
    <ForwardDialog />
    <LaterDialog />
  </div>
</template>
