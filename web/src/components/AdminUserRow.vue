<script setup lang="ts">
import type { PropType } from 'vue'
import type { AdminUser } from '../types/domain'
import type { VoiceSnapshot } from '../types/events'
import { KeyRound, UserX, UserCheck, PhoneOff, LogOut, Volume2, Shield, LockOpen } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { usePermissionsStore } from '../stores/permissions'
import { useAuthStore } from '../stores/auth'
import { t, locale } from '../i18n'
import UserAvatar from './UserAvatar.vue'
import { roleColorCss } from '../lib/permissions'

// One account of the admin user table: who they are, their state and the
// actions the signed-in member may take (by permission and rank). The
// actions themselves are the table's.
const props = defineProps({
  user: { type: Object as PropType<AdminUser>, required: true }
})
const emit = defineEmits<{ kick: []; 'clear-lockout': []; roles: []; 'toggle-disable': []; 'reset-login': []; 'revoke-sessions': [] }>()

const chatStore = useChatStore()
const voiceStore = useVoiceStore()
const permissions = usePermissionsStore()
const authStore = useAuthStore()

function voiceChannelId() {
  if (!voiceStore.channelUsers) return null
  const rooms: VoiceSnapshot = voiceStore.channelUsers
  for (const [chId, group] of Object.entries(rooms)) {
    if (group && group[props.user.id]) return chId
  }
  return null
}

function inVoice() {
  return voiceChannelId() !== null
}

function voiceChannelName() {
  const id = voiceChannelId()
  if (id === null) return ''
  const ch = chatStore.allChannels?.find(c => c.id === id)
  return ch?.name || id
}

function formatDateTime(dt: string | null | undefined) {
  if (!dt) return t('admin.never')
  try {
    return new Date(dt).toLocaleString(locale.value)
  } catch {
    return String(dt)
  }
}

// What the signed-in member may do with this account, by permission and rank.
function canModerate() {
  const u = props.user
  return u.role !== 'admin' && u.id !== authStore.user?.id && permissions.can('manage_members') && permissions.outranksMember(u.id)
}
function canGiveRoles() {
  const u = props.user
  return u.role !== 'admin' && permissions.can('manage_roles') && permissions.outranksMember(u.id) && u.id !== authStore.user?.id
}
function roles() {
  return (props.user.role_ids ?? []).map(id => permissions.roleById(id)).filter(role => role !== undefined)
}
</script>

<template>
  <tr
    data-testid="user-row"
    class="hover:bg-mnema-elevated/60 transition-colors"
  >
    <!-- Username with avatar -->
    <td class="py-3 px-4">
      <div class="flex items-center gap-2.5 min-w-0">
        <UserAvatar :user="user" size="xs" />
        <span data-testid="user-username" class="font-mono text-xs font-semibold text-mnema-text truncate">@{{ user.username }}</span>
      </div>
    </td>

    <!-- Display name -->
    <td class="py-3 px-4">
      <span data-testid="user-displayname" class="text-mnema-text font-medium truncate block max-w-[160px]">{{ user.display_name || user.username }}</span>
    </td>

    <!-- Role -->
    <td class="py-3 px-4">
      <span
        v-if="user.role === 'admin'"
        data-testid="user-role"
        class="px-2 py-0.5 rounded text-xs font-semibold bg-amber-500/10 text-amber-400 border border-amber-500/20"
      >
        {{ $t('role.admin') }}
      </span>
      <div v-else data-testid="user-role" class="flex flex-wrap items-center gap-1 text-xs text-mnema-muted">
        <span v-if="!roles().length">{{ $t('role.member') }}</span>
        <span
          v-for="role in roles()"
          :key="role.id"
          class="inline-flex items-center gap-1 rounded border border-mnema-border px-1.5 py-0.5 text-mnema-text"
        >
          <span class="h-2 w-2 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>{{ role.name }}
        </span>
      </div>
    </td>

    <!-- Last Seen -->
    <td class="py-3 px-4 text-xs text-mnema-muted whitespace-nowrap">
      <span data-testid="user-lastseen">{{ formatDateTime(user.last_seen_at) }}</span>
    </td>

    <!-- Status -->
    <td class="py-3 px-4 whitespace-nowrap">
      <div class="flex items-center gap-1.5 flex-wrap">
        <span
          v-if="user.disabled"
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

        <span
          v-if="user.login_locked"
          data-testid="user-login-locked"
          class="px-2 py-0.5 rounded text-xs font-medium bg-mnema-warning/10 text-mnema-warning border border-mnema-warning/25"
        >
          {{ $t('admin.loginLocked') }}
        </span>

        <!-- In Voice indicator -->
        <span
          v-if="inVoice()"
          class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-mono bg-mnema-band text-mnema-mint border border-mnema-mint/30"
          :title="$t('admin.inVoice')"
        >
          <Volume2 class="w-3 h-3 flex-shrink-0" />
          <span class="truncate max-w-[80px]">{{ voiceChannelName() }}</span>
        </span>
      </div>
    </td>

    <!-- Actions -->
    <td class="py-3 px-4 text-right whitespace-nowrap">
      <div class="flex items-center justify-end gap-1">
        <!-- Kick from voice -->
        <button
          v-if="inVoice() && permissions.can('disconnect_members')"
          data-testid="action-kick"
          type="button"
          class="p-1.5 rounded text-mnema-danger hover:bg-mnema-danger/15 transition"
          :title="$t('admin.kickFromVoice')"
          :aria-label="$t('admin.kickFromVoice')"
          @click="emit('kick')"
        >
          <PhoneOff class="w-4 h-4" />
        </button>

        <!-- Clear login lockout: Owner only, shown while locked -->
        <button
          v-if="permissions.isOwner && user.login_locked"
          data-testid="action-clear-lockout"
          type="button"
          class="p-1.5 rounded text-mnema-warning hover:bg-mnema-warning/15 transition"
          :title="$t('admin.clearLoginLockout')"
          :aria-label="$t('admin.clearLoginLockout')"
          @click="emit('clear-lockout')"
        >
          <LockOpen class="w-4 h-4" />
        </button>

        <button
          v-if="canGiveRoles()"
          data-testid="action-roles"
          type="button"
          class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
          :title="$t('memberRoles.edit')"
          :aria-label="$t('memberRoles.edit')"
          @click="emit('roles')"
        >
          <Shield class="w-4 h-4" />
        </button>

        <template v-if="canModerate()">
          <!-- Disable / Enable -->
          <button
            data-testid="action-disable"
            type="button"
            :class="[
              'p-1.5 rounded transition',
              (user.disabled)
                ? 'text-mnema-accent hover:bg-mnema-accent/15'
                : 'text-mnema-tertiary hover:text-mnema-danger hover:bg-mnema-hover'
            ]"
            :title="(user.disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
            :aria-label="(user.disabled) ? $t('admin.enableUser') : $t('admin.disableUser')"
            @click="emit('toggle-disable')"
          >
            <UserCheck v-if="user.disabled" class="w-4 h-4" />
            <UserX v-else class="w-4 h-4" />
          </button>

          <!-- Reset Password: only the Owner takes over accounts -->
          <button
            v-if="permissions.isOwner"
            data-testid="action-reset-password"
            type="button"
            class="p-1.5 rounded text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-hover transition"
            :title="$t('admin.resetPassword')"
            :aria-label="$t('admin.resetPassword')"
            @click="emit('reset-login')"
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
            @click="emit('revoke-sessions')"
          >
            <LogOut class="w-4 h-4" />
          </button>
        </template>
      </div>
    </td>
  </tr>
</template>
