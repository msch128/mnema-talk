<script setup lang="ts">
// Admin dashboard shell: the grouped navigation and one component per tab. On a
// desktop the navigation is a sidebar; on a phone the console is a full-screen
// sheet that opens on the list of sections and shows one section at a time.
// Tabs stay alive while the dialog is open, so an unsaved draft survives a tab
// switch; closing the console with an unsaved draft asks first (doc/settings-ux.md).
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { ArrowLeft, X } from '@lucide/vue'
import { navigate } from '../lib/router'
import { t } from '../i18n'
import { api } from '../lib/api'
import { decodeChatReportsPage } from '../types/rest'
import { ALL_TABS } from '../lib/adminNav'
import type { AdminTab } from '../lib/adminNav'
import { useCompactLayout } from '../composables/useCompactLayout'
import { provideDraftScope } from '../composables/useSettingsDraft'
import { useAuthStore } from '../stores/auth'
import { useFirstRunStore } from '../stores/firstRun'
import { usePermissionsStore } from '../stores/permissions'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import BaseDialog from './BaseDialog.vue'
import AdminNav from './AdminNav.vue'
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
import AdminAccessSettings from './AdminAccessSettings.vue'
import AdminEmojiTab from './AdminEmojiTab.vue'
import AdminReportsTab from './AdminReportsTab.vue'
import AdminAutomodTab from './AdminAutomodTab.vue'
import AdminRaidTab from './AdminRaidTab.vue'
import AdminImportTab from './AdminImportTab.vue'

const props = defineProps({
  initialTab: {
    type: String,
    default: 'users'
  }
})

const emit = defineEmits<{ close: [] }>()
const authStore = useAuthStore()
const { isCompact } = useCompactLayout()
const permissions = usePermissionsStore()
const features = useServerFeaturesStore()
// Every draft inside a tab registers here, so closing asks once for all of them.
const drafts = provideDraftScope()

// Each tab needs its own permission; storage and system are the Owner's.
const ALLOWED: Record<AdminTab, () => boolean> = {
  users: () => permissions.can('manage_members') || permissions.can('manage_roles'),
  roles: () => permissions.can('manage_roles'),
  channels: () => permissions.can('manage_channels'),
  invites: () => permissions.can('create_invites'),
  onboarding: () => permissions.can('administrator'),
  access: () => permissions.can('administrator'),
  emoji: () => permissions.can('manage_emoji'),
  reports: () => features.reports && permissions.can('manage_reports'),
  automod: () => features.automod && permissions.can('administrator'),
  raid: () => permissions.can('administrator'),
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
  access: AdminAccessSettings,
  emoji: AdminEmojiTab,
  reports: AdminReportsTab,
  automod: AdminAutomodTab,
  raid: AdminRaidTab,
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

// Phone: the sheet opens on the list of sections (unless the address names
// another one) and each section has a back arrow to that list.
const showList = ref(isCompact.value && activeTab.value === 'users')
const onList = computed(() => isCompact.value && showList.value)
function select(tab: AdminTab) {
  activeTab.value = tab
  showList.value = false
}
function backToList() {
  showList.value = true
  void nextTick(() => document.querySelector<HTMLElement>('[data-testid=admin-tabs] [aria-pressed=true]')?.focus())
}

// The open-report count beside "Reports": one cheap look when the console opens,
// and again whenever a report was closed.
const openReports = ref(0)
const openReportsMore = ref(false)
async function loadOpenReports() {
  if (!ALLOWED.reports()) return
  try {
    const page = await api('/api/admin/reports?status=open&limit=100', { decode: decodeChatReportsPage })
    openReports.value = page.reports.length
    openReportsMore.value = page.has_more
  } catch {
    openReports.value = 0
  }
}
onMounted(() => { void loadOpenReports() })
const badges = computed<Partial<Record<AdminTab, string>>>(() =>
  openReports.value > 0 ? { reports: openReportsMore.value ? `${openReports.value}+` : String(openReports.value) } : {})

// Focus starts on the dialog itself: no focus frame on a nav item or a button that was not asked for.
const focusDialog = (root: HTMLElement) => root
const signedInAs = computed(() => t('admin.nav.signedIn', { name: authStore.user?.display_name || authStore.user?.username || '' }))

// Opening the channels or status page completes that step of the Owner's
// checklist; closing the console reads the checklist again, as the Owner may
// have changed what its other steps look at.
const firstRun = useFirstRunStore()
watch(activeTab, tab => { if (tab === 'channels' || tab === 'status') void firstRun.visit(tab) }, { immediate: true })
onBeforeUnmount(() => { void firstRun.reload() })

watch(activeTab, (tab) => {
  if (typeof window !== 'undefined' && window.location.pathname !== `/admin/${tab}`) {
    navigate(`/admin/${tab}`, { replace: true })
  }
})
</script>

<template>
  <BaseDialog
    panel-class="max-w-5xl h-[85vh] max-h-[860px] max-md:fixed max-md:inset-0 max-md:h-auto max-md:max-h-none max-md:max-w-none max-md:rounded-none max-md:border-0 max-md:pt-[env(safe-area-inset-top)] max-md:pb-[env(safe-area-inset-bottom)]"
    :initial-focus="focusDialog"
    :guard="drafts.guard"
    @close="emit('close')"
  >
    <template #default="{ titleId, close }">
      <header class="flex shrink-0 items-center gap-2 border-b border-mnema-hairline bg-mnema-raised px-6 py-4 max-md:px-3">
        <button v-if="isCompact && !onList" type="button" data-testid="admin-back" class="btn btn-icon max-md:h-11 max-md:w-11 shrink-0" :aria-label="$t('admin.nav.back')" @click="backToList">
          <ArrowLeft class="h-[18px] w-[18px]" />
        </button>
        <h2 :id="titleId" class="min-w-0 flex-1 truncate text-lg font-semibold text-mnema-text max-md:px-1">{{ $t('menu.adminConsole') }}</h2>
        <button type="button" data-dialog-close v-tooltip="$t('common.close')" class="btn btn-icon max-md:h-11 max-md:w-11 shrink-0" @click="close">
          <X class="h-[18px] w-[18px]" />
        </button>
      </header>

      <div class="flex min-h-0 flex-1">
        <!-- Desktop sidebar -->
        <aside v-if="!isCompact" class="flex w-56 shrink-0 flex-col border-r border-mnema-hairline bg-mnema-canvas/40">
          <div class="min-h-0 flex-1 overflow-y-auto">
            <AdminNav :tabs="TABS" :active="activeTab" :badges="badges" @select="select" />
          </div>
          <p class="truncate border-t border-mnema-hairline px-4 py-3 font-mono text-xs text-mnema-tertiary" data-testid="admin-signed-in">{{ signedInAs }}</p>
        </aside>

        <!-- Phone: the list of sections, or the chosen one -->
        <div v-if="onList" class="min-h-0 flex-1 overflow-y-auto">
          <AdminNav :tabs="TABS" :active="activeTab" :badges="badges" list @select="select" />
          <p class="px-5 pb-6 font-mono text-xs text-mnema-tertiary">{{ signedInAs }}</p>
        </div>
        <!-- The padding is the inner box's, so a sticky action bar sits flush with the bottom edge. -->
        <div v-else class="min-w-0 flex-1 overflow-y-auto">
          <div class="space-y-6 p-6 max-md:p-4">
            <KeepAlive>
              <component :is="TAB_COMPONENTS[activeTab]" :key="activeTab" @changed="loadOpenReports" />
            </KeepAlive>
          </div>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>
