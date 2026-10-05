<script setup>
// Admin dashboard: accounts (disable, password reset, sessions, voice kick).
import { ref, computed, onMounted } from 'vue'
import { Users, RefreshCw, Search, KeyRound, UserX, UserCheck, PhoneOff, LogOut, Volume2 } from '@lucide/vue'
import { api } from '../lib/api'
import { confirm } from '../lib/confirm'
import { MIN_PASSWORD_LENGTH } from '../lib/passwordPolicy'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'
import BaseDialog from './BaseDialog.vue'
import UserAvatar from './UserAvatar.vue'

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const toasts = useToastStore()

const users = ref([])
const userSearch = ref('')
const passwordModalUser = ref(null)
const newPasswordInput = ref('')
const isResettingPassword = ref(false)

// Counted in characters like the server does, not UTF-16 units.
const passwordTooShort = computed(() => [...newPasswordInput.value].length < MIN_PASSWORD_LENGTH)

function showError(e) {
  toasts.error(e?.message || t('admin.unknownError'))
}

// A failed reload keeps the list that is already on screen.
async function refreshUsers() {
  try {
    users.value = (await api('/api/admin/users')) || []
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

function isUserInVoice(userId) {
  if (!voiceStore.channelUsers) return false
  for (const group of Object.values(voiceStore.channelUsers)) {
    if (group && group[userId]) return true
  }
  return false
}

function userVoiceChannelName(userId) {
  if (!voiceStore.channelUsers) return ''
  for (const [chId, group] of Object.entries(voiceStore.channelUsers)) {
    if (group && group[userId]) {
      const ch = chatStore.allChannels?.find(c => c.id === chId)
      return ch?.name || chId
    }
  }
  return ''
}

function formatDateTime(dt) {
  if (!dt) return t('admin.never')
  try {
    return new Date(dt).toLocaleString(locale.value)
  } catch {
    return String(dt)
  }
}

async function toggleDisableUser(u) {
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

function openPasswordModal(u) {
  passwordModalUser.value = u
  newPasswordInput.value = ''
}

function closePasswordModal() {
  passwordModalUser.value = null
  newPasswordInput.value = ''
}

async function submitPasswordReset() {
  if (!passwordModalUser.value || passwordTooShort.value) return
  isResettingPassword.value = true
  try {
    await api(`/api/admin/users/${passwordModalUser.value.id}/password`, {
      method: 'POST',
      json: { password: newPasswordInput.value }
    })
    toasts.success(t('admin.passwordResetSuccess'))
    closePasswordModal()
  } catch (e) {
    showError(e)
  } finally {
    isResettingPassword.value = false
  }
}

async function revokeSessions(u) {
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

async function kickUserFromVoice(u) {
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

onMounted(refreshUsers)
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
            <tr
              v-for="u in filteredUsers"
              :key="u.id"
              data-testid="user-row"
              class="hover:bg-mnema-elevated/60 transition-colors"
            >
              <!-- Username with avatar -->
              <td class="py-3 px-4">
                <div class="flex items-center gap-2.5 min-w-0">
                  <UserAvatar :user="u" size="xs" />
                  <span data-testid="user-username" class="font-mono text-xs font-semibold text-mnema-text truncate">@{{ u.username }}</span>
                </div>
              </td>

              <!-- Display name -->
              <td class="py-3 px-4">
                <span data-testid="user-displayname" class="text-mnema-text font-medium truncate block max-w-[160px]">{{ u.display_name || u.username }}</span>
              </td>

              <!-- Role -->
              <td class="py-3 px-4">
                <span
                  v-if="u.role === 'admin'"
                  data-testid="user-role"
                  class="px-2 py-0.5 rounded text-xs font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20"
                >
                  {{ $t('role.admin') }}
                </span>
                <span v-else data-testid="user-role" class="text-xs text-mnema-muted font-normal">
                  {{ $t('role.member') }}
                </span>
              </td>

              <!-- Last Seen -->
              <td class="py-3 px-4 text-xs text-mnema-muted whitespace-nowrap">
                <span data-testid="user-lastseen">{{ formatDateTime(u.last_seen_at) }}</span>
              </td>

              <!-- Status -->
              <td class="py-3 px-4 whitespace-nowrap">
                <div class="flex items-center gap-1.5 flex-wrap">
                  <span
                    v-if="u.disabled"
                    data-testid="user-status"
                    class="px-2 py-0.5 rounded text-xs font-medium bg-mnema-danger/10 text-mnema-danger border border-mnema-danger/20"
                  >
                    {{ $t('admin.userDisabled') }}
                  </span>
                  <span
                    v-else
                    data-testid="user-status"
                    class="px-2 py-0.5 rounded text-xs font-medium bg-mnema-accent/10 text-mnema-accent border border-mnema-accent/20"
                  >
                    {{ $t('admin.userActive') }}
                  </span>

                  <!-- In Voice indicator -->
                  <span
                    v-if="isUserInVoice(u.id)"
                    class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-mono bg-mnema-band text-mnema-mint border border-mnema-mint/30"
                    :title="$t('admin.inVoice')"
                  >
                    <Volume2 class="w-3 h-3 flex-shrink-0" />
                    <span class="truncate max-w-[80px]">{{ userVoiceChannelName(u.id) }}</span>
                  </span>
                </div>
              </td>

              <!-- Actions -->
              <td class="py-3 px-4 text-right whitespace-nowrap">
                <div class="flex items-center justify-end gap-1">
                  <!-- Kick from voice -->
                  <button
                    v-if="isUserInVoice(u.id)"
                    data-testid="action-kick"
                    type="button"
                    class="p-1.5 rounded text-mnema-danger hover:bg-mnema-danger/15 transition"
                    :title="$t('admin.kickFromVoice')"
                    :aria-label="$t('admin.kickFromVoice')"
                    @click="kickUserFromVoice(u)"
                  >
                    <PhoneOff class="w-4 h-4" />
                  </button>

                  <template v-if="u.role !== 'admin'">
                    <!-- Disable / Enable -->
                    <button
                      data-testid="action-disable"
                      type="button"
                      :class="[
                        'p-1.5 rounded transition',
                        (u.disabled)
                          ? 'text-mnema-accent hover:bg-mnema-accent/15'
                          : 'text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-hover'
                      ]"
                      :title="(u.disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
                      :aria-label="(u.disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
                      @click="toggleDisableUser(u)"
                    >
                      <UserCheck v-if="u.disabled" class="w-4 h-4" />
                      <UserX v-else class="w-4 h-4" />
                    </button>

                    <!-- Reset Password -->
                    <button
                      data-testid="action-reset-password"
                      type="button"
                      class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
                      :title="$t('admin.resetPassword')"
                      :aria-label="$t('admin.resetPassword')"
                      @click="openPasswordModal(u)"
                    >
                      <KeyRound class="w-4 h-4" />
                    </button>

                    <!-- Revoke Sessions -->
                    <button
                      data-testid="action-revoke-sessions"
                      type="button"
                      class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-warning hover:bg-mnema-hover transition"
                      :title="$t('admin.revokeSessions')"
                      :aria-label="$t('admin.revokeSessions')"
                      @click="revokeSessions(u)"
                    >
                      <LogOut class="w-4 h-4" />
                    </button>
                  </template>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Reset Password Modal -->
    <BaseDialog
      v-if="passwordModalUser"
      :title="$t('admin.resetPasswordTitle', { name: passwordModalUser.display_name || passwordModalUser.username })"
      panel-class="max-w-md"
      @close="closePasswordModal"
    >
      <form @submit.prevent="submitPasswordReset" class="p-6 space-y-4">
        <p class="text-sm text-mnema-muted">{{ $t('admin.resetPasswordPrompt', { count: MIN_PASSWORD_LENGTH }) }}</p>
        <div>
          <label class="block text-xs font-semibold text-mnema-tertiary uppercase tracking-wider mb-1.5 font-mono">
            {{ $t('admin.newPasswordLabel') }}
          </label>
          <input
            v-model="newPasswordInput"
            data-testid="new-password-input"
            type="password"
            :placeholder="$t('admin.newPasswordPlaceholder', { count: MIN_PASSWORD_LENGTH })"
            :aria-label="$t('admin.newPasswordLabel')"
            class="w-full bg-mnema-surface border border-mnema-border-field rounded-md px-3 py-2 text-sm text-mnema-text placeholder-mnema-tertiary outline-none focus:border-mnema-accent transition"
          />
          <p v-if="newPasswordInput && passwordTooShort" class="text-xs text-mnema-danger mt-1">
            {{ $t('admin.passwordTooShort', { count: MIN_PASSWORD_LENGTH }) }}
          </p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button
            type="button"
            class="px-3.5 py-1.5 rounded-md border border-mnema-border text-sm text-mnema-muted hover:text-mnema-text hover:bg-mnema-hover transition"
            @click="closePasswordModal"
          >
            {{ $t('common.cancel') }}
          </button>
          <button
            data-testid="save-password-button"
            type="submit"
            :disabled="passwordTooShort || isResettingPassword"
            class="bg-mnema-accent text-mnema-accent-ink hover:bg-mnema-accent-hover font-semibold px-4 py-1.5 rounded-md text-sm transition disabled:opacity-40"
          >
            {{ $t('admin.savePassword') }}
          </button>
        </div>
      </form>
    </BaseDialog>
  </section>
</template>
