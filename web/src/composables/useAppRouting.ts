import type { Route } from '../lib/router'
import type { RoutingContext } from './appRoutingApply'
import { ref, computed, watch } from 'vue'
import { applyRoute } from './appRoutingApply'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { currentRoute, navigate, canGoBackInApp } from '../lib/router'
import { loadVoiceChatOpen, saveVoiceChatOpen } from '../lib/voiceChatPanel'

/** Writes what the UI shows back to the address whenever any of it changes. */
function watchStateForRoute(ctx: RoutingContext, sync: () => void) {
  const { chatStore, voiceStore } = ctx
  watch(currentRoute, newRoute => { applyRoute(ctx, newRoute) })
  watch(() => chatStore.activeChannel, () => sync())
  watch(() => chatStore.activeThread, () => sync())
  watch(() => voiceStore.activeView, () => sync())
  // Joining a call (from anywhere) makes the joined channel the one on screen.
  watch(() => voiceStore.currentChannelId, id => {
    if (id) ctx.previewVoiceChannelId.value = null
    sync()
  })
  watch(ctx.previewVoiceChannelId, () => sync())
  watch(ctx.voiceShowChat, () => sync())
  // "Mention" (profile, member menu) in a Talk opens its chat to write it there.
  watch(() => chatStore.pendingMention, name => {
    if (name && voiceStore.activeView === 'voice' && !ctx.voiceShowChat.value) ctx.voiceShowChat.value = true
  })
  watch(ctx.showAdminModal, () => sync())
  watch(ctx.adminTab, () => sync())
}

/** Path of whatever the UI currently shows (to leave the admin console, or to give "/" an address). */
function statePath(ctx: RoutingContext, voiceChannelId: string | null) {
  const { chatStore, voiceStore } = ctx
  if (voiceStore.activeView === 'voice' && voiceChannelId) {
    return ctx.voiceShowChat.value ? `/v/${voiceChannelId}/chat` : `/v/${voiceChannelId}`
  }
  const channel = chatStore.activeChannel
  if (!channel) return '/'
  if (chatStore.activeThread) return `/c/${channel.id}/t/${chatStore.activeThread.id}`
  return `/c/${channel.id}`
}

function syncStateToRoute(ctx: RoutingContext) {
  if (ctx.isSyncingFromRoute || ctx.loadingFirstRoute) return
  if (!ctx.authStore.isAuthenticated) return
  const targetPath = ctx.showAdminModal.value ? `/admin/${ctx.adminTab.value || 'users'}` : ctx.currentStatePath()
  if (targetPath !== '/' && window.location.pathname !== targetPath) {
    navigate(targetPath, { replace: false })
  }
}

function createAdminNavigation(ctx: RoutingContext) {
  function openAdmin(tab = 'users') {
    if (!ctx.permissions.canOpenAdmin) return
    ctx.adminTab.value = tab
    ctx.showAdminModal.value = true
    navigate(`/admin/${tab}`, { replace: false })
  }

  function closeAdmin() {
    if (canGoBackInApp()) {
      window.history.back()
      return
    }
    ctx.showAdminModal.value = false
    navigate(ctx.currentStatePath(), { replace: true })
  }

  return { openAdmin, closeAdmin }
}

/**
 * The app's address and what it shows, kept in step both ways: the route
 * decides the channel, thread, Talk or admin console on screen, and what the
 * user does on screen is written back to the address.
 */
export function useAppRouting(alive: () => boolean) {
  const voiceStore = useVoiceStore()
  const showAdminModal = ref(false)
  const adminTab = ref('users')
  const previewVoiceChannelId = ref<string | null>(null)
  const voiceShowChat = ref((currentRoute.value.view === 'voice' && !!currentRoute.value.showChat) || loadVoiceChatOpen())
  watch(voiceShowChat, open => saveVoiceChatOpen(open))
  const voiceChannelId = computed(() => previewVoiceChannelId.value || voiceStore.currentChannelId)
  const currentStatePath = () => statePath(ctx, voiceChannelId.value)
  const ctx: RoutingContext = {
    alive,
    authStore: useAuthStore(),
    permissions: usePermissionsStore(),
    chatStore: useChatStore(),
    voiceStore,
    toasts: useToastStore(),
    showAdminModal,
    adminTab,
    previewVoiceChannelId,
    voiceShowChat,
    currentStatePath,
    lastVoiceRouteChannel: null,
    isSyncingFromRoute: false,
    routesApplying: 0,
    loadingFirstRoute: false,
    routeGen: 0
  }

  const syncCurrentStateToRoute = () => syncStateToRoute(ctx)
  watchStateForRoute(ctx, syncCurrentStateToRoute)

  const { openAdmin, closeAdmin } = createAdminNavigation(ctx)
  const onVoiceJoin = (id: string) => { previewVoiceChannelId.value = id === voiceStore.currentChannelId ? null : id }

  return {
    showAdminModal,
    adminTab,
    voiceShowChat,
    voiceChannelId,
    applyRoute: (route: Route) => applyRoute(ctx, route),
    syncCurrentStateToRoute,
    currentStatePath,
    openAdmin,
    closeAdmin,
    onVoiceJoin,
    beginFirstRoute() { ctx.loadingFirstRoute = true },
    endFirstRoute() { ctx.loadingFirstRoute = false },
    dispose() { ++ctx.routeGen }
  }
}
