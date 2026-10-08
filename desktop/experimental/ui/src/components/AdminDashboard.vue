<script setup lang="ts">
// Admin dashboard shell: the tab bar and one component per tab. Tabs stay
// alive while the dialog is open, so an unsaved layout survives a tab switch.
import { ref, watch } from 'vue'
import { Users, FolderTree, Link, HardDrive, Server } from '@lucide/vue'
import { navigate } from '../lib/router'
import { useAuthStore } from '../stores/auth'
import BaseDialog from './BaseDialog.vue'
import AdminUsersTab from './AdminUsersTab.vue'
import AdminLayoutTab from './AdminLayoutTab.vue'
import AdminInvitesTab from './AdminInvitesTab.vue'
import AdminMediaTab from './AdminMediaTab.vue'
import AdminSystemTab from './AdminSystemTab.vue'

const props = defineProps({
  initialTab: {
    type: String,
    default: 'users'
  }
})

const emit = defineEmits<{ close: [] }>()
const authStore = useAuthStore()

type AdminTab = 'users' | 'channels' | 'invites' | 'media' | 'system'
const TABS: readonly AdminTab[] = ['users', 'channels', 'invites', 'media', 'system']
function isAdminTab(value: string): value is AdminTab { return TABS.some(tab => tab === value) }
const TAB_COMPONENTS = {
  users: AdminUsersTab,
  channels: AdminLayoutTab,
  invites: AdminInvitesTab,
  media: AdminMediaTab,
  system: AdminSystemTab
}
const activeTab = ref<AdminTab>(isAdminTab(props.initialTab) ? props.initialTab : 'users')

watch(() => props.initialTab, (newTab) => {
  if (newTab && isAdminTab(newTab)) {
    activeTab.value = newTab
  }
})

watch(activeTab, (tab) => {
  if (typeof window !== 'undefined' && window.location.pathname !== `/admin/${tab}`) {
    navigate(`/admin/${tab}`, { replace: true })
  }
})
</script>

<template>
  <BaseDialog
    :title="$t('admin.title')"
    :subtitle="$t('admin.signedInAs', { name: authStore.user?.display_name || authStore.user?.username || '' })"
    panel-class="max-w-5xl max-h-[90vh]"
    @close="emit('close')"
  >
    <!-- Navigation tabs -->
    <div class="flex items-center gap-1 px-6 py-2.5 border-b border-mnema-hairline bg-mnema-canvas/40 text-sm overflow-x-auto flex-shrink-0">
      <button
        v-for="tab in TABS"
        :key="tab"
        :data-testid="`tab-${tab}`"
        type="button"
        :aria-pressed="activeTab === tab ? 'true' : 'false'"
        :class="activeTab === tab ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
        class="px-3.5 py-1.5 rounded-md border transition whitespace-nowrap flex items-center gap-2"
        @click="activeTab = tab"
      >
        <Users v-if="tab === 'users'" class="w-4 h-4" />
        <FolderTree v-else-if="tab === 'channels'" class="w-4 h-4" />
        <Link v-else-if="tab === 'invites'" class="w-4 h-4" />
        <HardDrive v-else-if="tab === 'media'" class="w-4 h-4" />
        <Server v-else class="w-4 h-4" />
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
