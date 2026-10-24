<script setup lang="ts">
// Admin dashboard shell: the tab bar and one component per tab. Tabs stay
// alive while the dialog is open, so an unsaved layout survives a tab switch.
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import { Users, FolderTree, Link, HardDrive, Server, Shield, ScrollText, Gavel, Bot, Sparkles, Smile, Flag, ShieldCheck, Activity, Download } from '@lucide/vue'
import { navigate } from '../lib/router'
import { t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import BaseDialog from './BaseDialog.vue'
import AdminUsersTab from './AdminUsersTab.vue'
import AdminRolesTab from './AdminRolesTab.vue'
import AdminLayoutTab from './AdminLayoutTab.vue'
import AdminInvitesTab from './AdminInvitesTab.vue'
import AdminMediaTab from './AdminMediaTab.vue'
import AdminSystemTab from './AdminSystemTab.vue'
import AdminStatusTab from './AdminStatusTab.vue'
import AdminAuditTab from './AdminAuditTab.vue'
import AdminBansTab from './AdminBansTab.vue'
import AdminBotsTab from './AdminBotsTab.vue'
import AdminOnboardingTab from './AdminOnboardingTab.vue'
import AdminEmojiTab from './AdminEmojiTab.vue'
import AdminReportsTab from './AdminReportsTab.vue'
import AdminAutomodTab from './AdminAutomodTab.vue'
import AdminImportTab from './AdminImportTab.vue'

const props = defineProps({
  initialTab: {
    type: String,
    default: 'users'
  }
})

const emit = defineEmits<{ close: [] }>()
const authStore = useAuthStore()
const permissions = usePermissionsStore()
const features = useServerFeaturesStore()

type AdminTab = 'users' | 'roles' | 'channels' | 'invites' | 'onboarding' | 'emoji' | 'reports' | 'automod' | 'bans' | 'bots' | 'import' | 'media' | 'audit' | 'system' | 'status'
const ALL_TABS: readonly AdminTab[] = ['users', 'roles', 'channels', 'invites', 'onboarding', 'emoji', 'reports', 'automod', 'bans', 'bots', 'import', 'media', 'audit', 'system', 'status']
// Each tab needs its own permission; storage and system are the Owner's.
const ALLOWED: Record<AdminTab, () => boolean> = {
  users: () => permissions.can('manage_members') || permissions.can('manage_roles'),
  roles: () => permissions.can('manage_roles'),
  channels: () => permissions.can('manage_channels'),
  invites: () => permissions.can('create_invites'),
  onboarding: () => permissions.can('administrator'),
  emoji: () => permissions.can('manage_emoji'),
  reports: () => features.reports && permissions.can('manage_reports'),
  automod: () => features.automod && permissions.can('administrator'),
  bans: () => permissions.can('ban_members'),
  bots: () => permissions.can('manage_bots'),
  import: () => features.importDiscord && permissions.can('administrator'),
  media: () => permissions.isOwner,
  audit: () => permissions.can('view_audit_log'),
  system: () => permissions.isOwner,
  status: () => features.opsStatus && (permissions.isOwner || permissions.can('administrator'))
}
const TABS = computed(() => ALL_TABS.filter(tab => ALLOWED[tab]()))
function isAdminTab(value: string): value is AdminTab { return TABS.value.some(tab => tab === value) }
const TAB_COMPONENTS = {
  users: AdminUsersTab,
  roles: AdminRolesTab,
  channels: AdminLayoutTab,
  invites: AdminInvitesTab,
  onboarding: AdminOnboardingTab,
  emoji: AdminEmojiTab,
  reports: AdminReportsTab,
  automod: AdminAutomodTab,
  bans: AdminBansTab,
  bots: AdminBotsTab,
  import: AdminImportTab,
  media: AdminMediaTab,
  audit: AdminAuditTab,
  system: AdminSystemTab,
  status: AdminStatusTab
}
const activeTab = ref<AdminTab>(isAdminTab(props.initialTab) ? props.initialTab : (TABS.value[0] ?? 'users'))

watch(() => props.initialTab, (newTab) => {
  if (newTab && isAdminTab(newTab)) {
    activeTab.value = newTab
  }
})

// Phone widths: the tab bar scrolls sideways, so fade the edge that has more
// tabs behind it and keep the active tab in view.
const tabBar = ref<HTMLElement | null>(null)
const fadeStart = ref(false)
const fadeEnd = ref(false)
function updateFades() {
  const bar = tabBar.value
  if (!bar) return
  fadeStart.value = bar.scrollLeft > 4
  fadeEnd.value = bar.scrollLeft + bar.clientWidth < bar.scrollWidth - 4
}
async function revealActiveTab() {
  await nextTick()
  tabBar.value?.querySelector('[aria-pressed="true"]')?.scrollIntoView?.({ block: 'nearest', inline: 'center' })
  updateFades()
}
onMounted(revealActiveTab)

// The title names the tab you are on; the subtitle names the console.
const dialogTitle = computed(() => t(`admin.tabs.${activeTab.value}`))
const dialogSubtitle = computed(() => `${t('menu.adminConsole')} · ${t('admin.signedInAs', { name: authStore.user?.display_name || authStore.user?.username || '' })}`)

watch(activeTab, (tab) => {
  void revealActiveTab()
  if (typeof window !== 'undefined' && window.location.pathname !== `/admin/${tab}`) {
    navigate(`/admin/${tab}`, { replace: true })
  }
})
</script>

<template>
  <BaseDialog
    :title="dialogTitle"
    :subtitle="dialogSubtitle"
    panel-class="max-w-5xl h-[85vh] max-h-[860px]"
    initial-focus="[data-testid=admin-tabs] [aria-pressed=true]"
    @close="emit('close')"
  >
    <!-- Navigation tabs -->
    <div
      ref="tabBar"
      data-testid="admin-tabs"
      :class="['admin-tabs flex items-center gap-1 px-6 max-md:px-3 py-2.5 border-b border-mnema-hairline bg-mnema-canvas/40 text-sm shrink-0 md:flex-wrap max-md:overflow-x-auto', fadeStart && 'fade-start', fadeEnd && 'fade-end']"
      @scroll.passive="updateFades"
    >
      <button
        v-for="tab in TABS"
        :key="tab"
        :data-testid="`tab-${tab}`"
        type="button"
        :aria-pressed="activeTab === tab ? 'true' : 'false'"
        class="chip chip-bare"
        @click="activeTab = tab"
      >
        <Users v-if="tab === 'users'" class="w-4 h-4" />
        <Shield v-else-if="tab === 'roles'" class="w-4 h-4" />
        <FolderTree v-else-if="tab === 'channels'" class="w-4 h-4" />
        <Link v-else-if="tab === 'invites'" class="w-4 h-4" />
        <Sparkles v-else-if="tab === 'onboarding'" class="w-4 h-4" />
        <Smile v-else-if="tab === 'emoji'" class="w-4 h-4" />
        <Flag v-else-if="tab === 'reports'" class="w-4 h-4" />
        <ShieldCheck v-else-if="tab === 'automod'" class="w-4 h-4" />
        <Gavel v-else-if="tab === 'bans'" class="w-4 h-4" />
        <Bot v-else-if="tab === 'bots'" class="w-4 h-4" />
        <Download v-else-if="tab === 'import'" class="w-4 h-4" />
        <HardDrive v-else-if="tab === 'media'" class="w-4 h-4" />
        <ScrollText v-else-if="tab === 'audit'" class="w-4 h-4" />
        <Server v-else-if="tab === 'system'" class="w-4 h-4" />
        <Activity v-else class="w-4 h-4" />
        <span>{{ $t(`admin.tabs.${tab}`) }}</span>
      </button>
    </div>

    <!-- Scrollable tab body -->
    <div class="flex-1 overflow-y-auto p-6 space-y-6">
      <KeepAlive>
        <component :is="TAB_COMPONENTS[activeTab]" :key="activeTab" />
      </KeepAlive>
    </div>
  </BaseDialog>
</template>
