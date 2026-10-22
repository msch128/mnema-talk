<script setup lang="ts">
// Admin dashboard: accounts (disable, password reset, sessions, voice kick).
import { ref, computed, onMounted } from 'vue'
import { Users, RefreshCw, Search } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import type { AdminUser } from '../types/domain'
import { isAuthAdminUser } from '../types/rest'
import { arrayDecoder } from '../types/validation'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
import { t } from '../i18n'
import AdminUserRow from './AdminUserRow.vue'
import AdminResetPasswordDialog from './AdminResetPasswordDialog.vue'
import MemberRolesDialog from './MemberRolesDialog.vue'

const toasts = useToastStore()
const permissions = usePermissionsStore()
const rolesModalUser = ref<AdminUser | null>(null)

const users = ref<AdminUser[]>([])
const resetUser = ref<AdminUser | null>(null)
const userSearch = ref('')
function showError(e: unknown) {
  toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
}

// A failed reload keeps the list that is already on screen.
async function refreshUsers() {
  try {
    users.value = await api('/api/admin/users', { decode: arrayDecoder('admin users', isAuthAdminUser) })
    permissions.setMemberRoles(users.value)
  } catch (e) {
    showError(e)
  }
}

const filteredUsers = computed(() => {
  const q = userSearch.value.trim().toLowerCase()
  if (!q) return users.value
  return users.value.filter(u =>
    (u.username && u.username.toLowerCase().includes(q)) ||
    (u.display_name && u.display_name.toLowerCase().includes(q))
  )
})

async function toggleDisableUser(u: AdminUser) {
  const isDisabled = !!u.disabled
  if (!isDisabled) {
    const ok = await confirm({
      title: t('admin.disableUserTitle', { name: u.display_name || u.username }),
      body: t('admin.disableUserBody'),
      confirmLabel: t('admin.disableUserConfirm'),
      danger: true
    })
    if (!ok) return
    try {
      await api(`/api/admin/users/${u.id}/disable`, { method: 'POST' })
      toasts.success(t('admin.userDisabledToast'))
      await refreshUsers()
    } catch (e) {
      showError(e)
    }
  } else {
    try {
      await api(`/api/admin/users/${u.id}/enable`, { method: 'POST' })
      toasts.success(t('admin.userEnabledToast'))
      await refreshUsers()
    } catch (e) {
      showError(e)
    }
  }
}

async function revokeSessions(u: AdminUser) {
  const ok = await confirm({
    title: t('admin.revokeSessionsTitle', { name: u.display_name || u.username }),
    body: t('admin.revokeSessionsBody'),
    confirmLabel: t('admin.revokeSessionsConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/users/${u.id}/sessions/revoke`, { method: 'POST' })
    toasts.success(t('admin.sessionsRevoked'))
  } catch (e) {
    showError(e)
  }
}

async function resetTwoFactor(u: AdminUser) {
  const ok = await confirm({
    title: t('admin.resetTwoFactorTitle', { name: u.display_name || u.username }),
    body: t('admin.resetTwoFactorBody'),
    confirmLabel: t('admin.resetTwoFactorConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/users/${u.id}/two-factor/reset`, { method: 'POST' })
    toasts.success(t('admin.twoFactorReset'))
  } catch (e) {
    showError(e)
  }
}

async function clearLoginLockout(u: AdminUser) {
  try {
    await api(`/api/admin/users/${u.id}/login-lockout/clear`, { method: 'POST' })
    toasts.success(t('admin.loginLockoutCleared'))
    await refreshUsers()
  } catch (e) {
    showError(e)
  }
}

async function kickUserFromVoice(u: AdminUser) {
  const ok = await confirm({
    title: t('admin.kickFromVoiceTitle', { name: u.display_name || u.username }),
    body: t('admin.kickFromVoiceBody'),
    confirmLabel: t('admin.kickConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/users/${u.id}/kick`, { method: 'POST' })
    toasts.success(t('admin.userKicked'))
  } catch (e) {
    showError(e)
  }
}

onMounted(async () => {
  await Promise.all([refreshUsers(), permissions.loadRoles()])
})
</script>

<template>
  <section class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <h3 class="text-sm font-semibold text-mnema-text uppercase font-mono tracking-wider flex items-center gap-2">
          <Users class="w-4 h-4 text-mnema-accent" />
          <span>{{ $t('admin.users') }}</span>
        </h3>
        <span class="text-xs text-mnema-tertiary">({{ users.length }})</span>
      </div>

      <div class="flex items-center gap-2 flex-1 max-w-sm ml-auto">
        <div class="relative flex-1">
          <Search class="w-4 h-4 text-mnema-tertiary absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none" />
          <input
            v-model="userSearch"
            type="search"
            :placeholder="$t('admin.searchUsers')"
            :aria-label="$t('admin.searchUsersLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md pl-8 pr-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
        </div>
        <button
          type="button"
          class="p-2 rounded-md text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-elevated transition flex-shrink-0"
          :title="$t('admin.refresh')"
          :aria-label="$t('admin.refresh')"
          @click="refreshUsers"
        >
          <RefreshCw class="w-4 h-4" />
        </button>
      </div>
    </div>

    <!-- Users Table -->
    <div class="bg-mnema-surface rounded-lg border border-mnema-hairline overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm divide-y divide-mnema-hairline">
          <thead class="bg-mnema-canvas/50 text-xs uppercase font-mono text-mnema-tertiary tracking-wider select-none">
            <tr>
              <th scope="col" class="py-3 px-4">{{ $t('admin.username') }}</th>
              <th scope="col" class="py-3 px-4">{{ $t('admin.displayName') }}</th>
              <th scope="col" class="py-3 px-4">{{ $t('admin.role') }}</th>
              <th scope="col" class="py-3 px-4">{{ $t('admin.lastSeen') }}</th>
              <th scope="col" class="py-3 px-4">{{ $t('admin.status') }}</th>
              <th scope="col" class="py-3 px-4 text-right">{{ $t('admin.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-mnema-hairline">
            <tr v-if="!filteredUsers.length">
              <td colspan="6" class="p-6 text-center text-mnema-tertiary italic">
                {{ $t('admin.noUsersFound') }}
              </td>
            </tr>
            <AdminUserRow
              v-for="u in filteredUsers"
              :key="u.id"
              :user="u"
              @kick="kickUserFromVoice(u)"
              @clear-lockout="clearLoginLockout(u)"
              @roles="rolesModalUser = u"
              @toggle-disable="toggleDisableUser(u)"
              @reset-login="resetUser = u"
              @reset-two-factor="resetTwoFactor(u)"
              @revoke-sessions="revokeSessions(u)"
            />
          </tbody>
        </table>
      </div>
    </div>

    <MemberRolesDialog v-if="rolesModalUser" :user="rolesModalUser" @close="rolesModalUser = null" @saved="refreshUsers" />

    <AdminResetPasswordDialog v-if="resetUser" :user="resetUser" @close="resetUser = null" />
  </section>
</template>
